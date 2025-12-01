package shim

import (
	"context"
	"fmt"
	er "micrun/errors"
	log "micrun/logger"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	cntr "micrun/pkg/micantainer"
	oci "micrun/pkg/oci"
	"micrun/pkg/utils"

	"github.com/containerd/containerd/api/events"
	eventstypes "github.com/containerd/containerd/api/events"
	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/namespaces"
	ptypes "github.com/containerd/containerd/protobuf/types"
	shimv2 "github.com/containerd/containerd/runtime/v2/shim"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const channelSize = 128

var (
	_       taskAPI.TaskService = (*shimService)(nil)
	shimPid                     = uint32(os.Getpid())
)

type shimService struct {
	id          string
	micadPid    int
	shimPid     int
	namespace   string
	config      *oci.RuntimeConfig
	containers  map[string]*shimContainer
	sandbox     cntr.SandboxTraits
	ctx         context.Context
	events      chan any
	ec          chan exitEvent
	ss          func()
	mu          sync.Mutex
	monitor     chan error
	eventSendMu sync.Mutex
}

func New(ctx context.Context, id string, publisher shimv2.Publisher, shutdown func()) (shimv2.Shim, error) {
	ns, found := namespaces.Namespace(ctx)
	if !found {
		return nil, fmt.Errorf("namespace is required")
	}

	micadPid, err := getMicadPid()
	if err != nil {
		log.Warnf("failed to get micad PID, setting to 0: %v", err)
		return nil, err
	}

	s := &shimService{
		id:        id,
		micadPid:  micadPid,
		shimPid:   os.Getpid(),
		namespace: ns,
		ctx:       ctx,
		events:    make(chan any, channelSize),
		ss:        shutdown,
		monitor:   make(chan error),
	}

	log.Debugf("starting service background goroutines exit listener")

	go s.listenAndReportExits()

	// Start events forwarder to publish events to containerd
	forwarder := s.newEventsForwarder(ctx, publisher)
	go forwarder.forward()

	log.Debugf("completed successfully, returning shimService")
	return s, nil
}

func newCommand(ctx context.Context, opts shimv2.StartOpts, cwd string) (*exec.Cmd, error) {

	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get current executable path: %w", err)
	}

	var args []string
	if opts.Debug {
		args = append(args, "-debug")
		args = append(args, "-id", opts.ID)
	}

	// TTRPC_ADDRESS the address of containerd's ttrpc API socket
	// GRPC_ADDRESS the address of containerd's grpc API socket (1.7+)
	// MAX_SHIM_VERSION the maximum shim version supported by the client, always 2 for shim v2 (1.7+)
	// SCHED_CORE enable core scheduling if available (1.6+)
	// NAMESPACE an optional namespace the shim is operating in or inheriting (1.7+)
	// LOG_COLOR controls colored output in the shim process
	cmdCfg := &shimv2.CommandConfig{
		Runtime:      self,
		Address:      opts.Address,
		TTRPCAddress: opts.TTRPCAddress,
		// resolved expanded path
		Path:      cwd,
		SchedCore: os.Getenv(contdShimEnvShedCore) != "",
		Args:      args,
	}

	// -namespace the namespace for the container
	// -address the address of the containerd's main grpc socket
	// -publish-binary the binary path to publish events back to containerd
	// -id the id of the container (containerID)
	// The start command, as well as all binary calls to the shim, has the bundle for the container set as the cwd.
	cmd, err := shimv2.Command(ctx, cmdCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create shim command: %w", err)
	}
	// Pass LOG_COLOR environment variable to child process
	if logColor := os.Getenv("LOG_COLOR"); logColor != "" {
		cmd.Env = append(cmd.Env, "LOG_COLOR="+logColor)
	}

	// Do not redirect child's stdout here. The parent `start` path is
	// responsible for emitting the address cleanly; child's logging(info,warn,error) is
	// routed to containerd via the shim FIFO logger setup.
	return cmd, nil
}

func (s *shimService) StartShim(ctx context.Context, opts shimv2.StartOpts) (_ string, retErr error) {
	// origLevel := log.Log.GetLevel()
	// log.Log.SetLevel(logrus.WarnLevel)
	// defer log.Log.SetLevel(origLevel)
	bundle, err := os.Getwd()
	if err != nil {
		return "", err
	}

	bundle, err = validBundle(opts.ID, bundle)
	if err != nil {
		return "", err
	}

	sockaddr, err := getContainerSocketAddr(ctx, bundle, opts)
	if err != nil {
		return "", err
	}

	// if podContainer/singleContainer: do not need a new shim binary, only write socket and then finished starting
	if sockaddr != "" {
		// write <socketaddr> into <bundle>/address socket
		if err := shimv2.WriteAddress("address", sockaddr); err != nil {
			return "", fmt.Errorf("failed to write socket address for pod container: %w", err)
		}
		return sockaddr, nil
	}

	log.Debugf("args: %s", os.Args)
	cmd, err := newCommand(ctx, opts, bundle)
	if err != nil {
		return "", err
	}

	// single container / sandbox
	sockAddr, err := shimv2.SocketAddress(ctx, opts.Address, opts.ID)
	if err != nil {
		return "", err
	}

	socket, err := shimv2.NewSocket(sockAddr)

	if err != nil {
		// containerd:
		// the only time where this would happen is if there is a bug and the socket
		// was not cleaned up in the cleanup method of the shim or we are using the
		// grouping functionality where the new process should be run with the same
		// shim as an existing container
		if !shimv2.SocketEaddrinuse(err) {
			return "", fmt.Errorf("create new shim socket: %w", err)
		}
		if shimv2.CanConnect(sockAddr) {
			if err := shimv2.WriteAddress("address", sockAddr); err != nil {
				return "", fmt.Errorf("write existing socket for shim: %w", err)
			}
			return sockAddr, nil
		}
		log.Debugf("removing stale socket and creating new one")
		if err := shimv2.RemoveSocket(sockAddr); err != nil {
			return "", fmt.Errorf("remove pre-existing socket: %w", err)
		}
		if socket, err = shimv2.NewSocket(sockAddr); err != nil {
			return "", fmt.Errorf("try create new shim socket 2x: %w", err)
		}
	}

	defer func() {
		if retErr != nil {
			socket.Close()
			if err := shimv2.RemoveSocket(sockAddr); err != nil {
				log.Debugf("failed to remove socket %s: %v", sockAddr, err)
			}
		}
	}()

	// make sure that reexec shim-v2 binary use the value if need
	if err := shimv2.WriteAddress("address", sockAddr); err != nil {
		return "", err
	}

	sock, err := socket.File()
	if err != nil {
		return "", err
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, sock)

	runtime.LockOSThread()
	if os.Getenv("SCHED_CORE") != "" {
		tipSchedCore()
	}

	if err := cmd.Start(); err != nil {
		_ = sock.Close()
		return "", fmt.Errorf("failed to start shim task service: %w", err)
	}

	runtime.UnlockOSThread()

	// BUG: sometimes micrun failed to ensure the container socket is dropped after deleted
	// result in socket leak even if containerd managed to remove the socket
	defer func() {
		if retErr != nil {
			if err := cmd.Process.Kill(); err != nil {
				time.Sleep(2 * time.Second)
				log.Debugf("failed to kill shim process: %v, try again: %v", err, cmd.Process.Kill().Error())
			}
		}
	}()

	// Wait in background to avoid zombie if parent outlives child briefly.
	go cmd.Wait()

	if err = shimv2.WritePidFile("shim.pid", cmd.Process.Pid); err != nil {
		return "", fmt.Errorf("failed to write shim PID file: %w", err)
	}

	if err = shimv2.WriteAddress("address", sockAddr); err != nil {
		return "", err
	}

	// best effort
	err = setupStateDir()
	if err != nil {
		log.Warnf("failed to setup micrun state directory: %v", err)
	}

	return sockAddr, nil

}

// steps:
// delete forcely, cleanup containers in memory
// unmount recursively
// clean pidfile
// send event
// return Response
func (s *shimService) Cleanup(ctx context.Context) (*taskAPI.DeleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Tips from kata-container:
	// Since the binary cleanup will return the DeleteResponse from stdout to
	// containerd, thus we must make sure there is no any outputs in stdout except
	// the returned response, thus here redirect the log to stderr in case there's
	// any log output to stdout.
	logrus.SetOutput(os.Stderr)
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	if s.id == "" {
		return nil, fmt.Errorf("container ID is required")
	}

	ociSpec, err := oci.LoadSpec(cwd)
	if err != nil {
		return nil, fmt.Errorf("failed to load valid runtime config: %w", err)
	}

	ctype, err := oci.GetContainerType(&ociSpec)
	if err != nil {
		return nil, err
	}
	switch ctype {
	case cntr.PodSandbox, cntr.SingleContainer:
		err = cleanupContainer(ctx, s.id, s.id, cwd)
		if err != nil {
			return nil, err
		}
	case cntr.PodContainer:
		sandboxID, err := oci.GetSandboxID(&ociSpec)
		if err != nil {
			return nil, err
		}
		err = cleanupContainer(ctx, sandboxID, s.id, cwd)
		if err != nil {
			return nil, err
		}
	default:
		log.Debugf("unknown container type to be cleaned up: %s", ctype)
	}

	return &taskAPI.DeleteResponse{
		ExitedAt:   timestamppb.New(time.Now()),
		ExitStatus: 128 + uint32(unix.SIGKILL),
	}, nil
}

// ***************** taskAPI task entries ********************

var emptyResponse = &ptypes.Empty{}

func (s *shimService) State(ctx context.Context, r *taskAPI.StateRequest) (*taskAPI.StateResponse, error) {

	return nil, nil
}

// does not send request to micad, create container in memory
func (s *shimService) Create(ctx context.Context, r *taskAPI.CreateTaskRequest) (*taskAPI.CreateTaskResponse, error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	log.Debugf("creating task %s (bundle: %s, terminal: %v)", r.ID, r.Bundle, r.Terminal)
	if err := utils.ValidContainerID(r.ID); err != nil {
		return nil, er.InvalidCID
	}

	// create container sync

	container, err := create(ctx, s, r)
	if err != nil {
		return nil, err
	}
	// lock when updating shared state
	s.mu.Lock()
	container.status = task.Status_CREATED
	s.containers[r.ID] = container
	s.mu.Unlock()

	pid := container.pid
	if pid <= 0 {
		pid = shimPid
	}
	s.send(&events.TaskCreate{
		ContainerID: r.ID,
		Bundle:      r.Bundle,
		Rootfs:      r.Rootfs,
		IO: &eventstypes.TaskIO{
			Stdin:    r.Stdin,
			Stdout:   r.Stdout,
			Stderr:   r.Stderr,
			Terminal: r.Terminal,
		},
		Checkpoint: r.Checkpoint,
		Pid:        pid,
	})

	return &taskAPI.CreateTaskResponse{
		Pid: pid,
	}, nil
	return nil, nil
}
func (s *shimService) Start(context.Context, *taskAPI.StartRequest) (*taskAPI.StartResponse, error) {

	return nil, nil
}
func (s *shimService) Delete(context.Context, *taskAPI.DeleteRequest) (*taskAPI.DeleteResponse, error) {

	return nil, nil
}
func (s *shimService) Pids(context.Context, *taskAPI.PidsRequest) (*taskAPI.PidsResponse, error) {

	return nil, nil
}
func (s *shimService) Pause(context.Context, *taskAPI.PauseRequest) (*ptypes.Empty, error) {
	return emptyResponse, nil
}
func (s *shimService) Resume(context.Context, *taskAPI.ResumeRequest) (*ptypes.Empty, error) {

	return emptyResponse, nil
}
func (s *shimService) Checkpoint(context.Context, *taskAPI.CheckpointTaskRequest) (*ptypes.Empty, error) {

	return emptyResponse, nil
}
func (s *shimService) Kill(context.Context, *taskAPI.KillRequest) (*ptypes.Empty, error) {

	return emptyResponse, nil
}
func (s *shimService) Exec(context.Context, *taskAPI.ExecProcessRequest) (*ptypes.Empty, error) {

	return emptyResponse, nil
}
func (s *shimService) ResizePty(context.Context, *taskAPI.ResizePtyRequest) (*ptypes.Empty, error) {

	return emptyResponse, nil
}
func (s *shimService) CloseIO(context.Context, *taskAPI.CloseIORequest) (*ptypes.Empty, error) {

	return emptyResponse, nil
}
func (s *shimService) Update(context.Context, *taskAPI.UpdateTaskRequest) (*ptypes.Empty, error) {

	return emptyResponse, nil
}
func (s *shimService) Wait(context.Context, *taskAPI.WaitRequest) (*taskAPI.WaitResponse, error) {

	return nil, nil
}
func (s *shimService) Stats(context.Context, *taskAPI.StatsRequest) (*taskAPI.StatsResponse, error) {

	return nil, nil
}
func (s *shimService) Connect(context.Context, *taskAPI.ConnectRequest) (*taskAPI.ConnectResponse, error) {

	return nil, nil
}
func (s *shimService) Shutdown(context.Context, *taskAPI.ShutdownRequest) (*ptypes.Empty, error) {

	return emptyResponse, nil
}
