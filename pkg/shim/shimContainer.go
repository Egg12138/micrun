package shim

import (
	"io"
	log "micrun/logger"
	cntr "micrun/pkg/micantainer"
	"sync"
	"time"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types/task"
	"github.com/containerd/containerd/errdefs"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type shimContainer struct {
	s     *shimService
	spec  *specs.Spec
	ttyio *ttyIO
	id    string
	// io
	stdin       string
	stdout      string
	stderr      string
	stdinPipe   io.WriteCloser
	stdinCloser chan struct{}
	exitIOch    chan struct{}
	exitIoOnce  sync.Once
	bundle      string // abs path of the bundle directory
	cType       cntr.ContainerType
	status      task.Status
	exit        uint32
	terminal    bool
	pid         uint32 // shim pid
	exitTime    time.Time
	mounted     bool
	// TODO: we can simulate `exec` by sending commands to mica pty
	execs map[string]*execTask // extensible in future
}

// TODO: redundant fields
type execTask struct {
	id         string
	pid        uint32 // shimPid, remove this field is ok
	exitCode   uint32
	createTime time.Time
	exitTime   time.Time
	status     task.Status
	waitCh     chan struct{}
	waitOnce   sync.Once
	// NOTICE: stdin, stdout, stderr will be assigned with different fifo set
	stdin    string
	stdout   string
	stderr   string
	terminal bool

	ttyio       *ttyIO
	stdinPipe   io.WriteCloser
	stdinCloser chan struct{}
	exitIOch    chan struct{}
}

func newExecProcess(id string) *execTask {
	return &execTask{
		id:          id,
		status:      task.Status_CREATED,
		waitCh:      make(chan struct{}),
		stdinCloser: make(chan struct{}),
		exitIOch:    make(chan struct{}),
	}
}

func (t *execTask) markStarted(pid uint32) {
	t.pid = pid
	t.status = task.Status_RUNNING
}

func (t *execTask) markExited(exitStatus uint32) (changed bool) {
	if t.status != task.Status_STOPPED {
		t.status = task.Status_STOPPED
		t.exitCode = exitStatus
		t.exitTime = time.Now()
		changed = true
	}
	t.waitOnce.Do(func() {
		close(t.waitCh)
	})
	return changed
}

// newContainer creates a new container object for the shim.
func newContainer(s *shimService, r *taskAPI.CreateTaskRequest, cType cntr.ContainerType, ocispec *specs.Spec, mounted bool) (*shimContainer, error) {
	if r == nil {
		return nil, errdefs.ToGRPCf(errdefs.ErrInvalidArgument, "CreateTaskRequest points to nil")
	}

	if ocispec == nil {
		ocispec = &specs.Spec{}
	}

	c := &shimContainer{
		s:           s,
		spec:        ocispec,
		id:          r.ID,
		stdin:       r.Stdin,
		stdout:      r.Stdout,
		stderr:      r.Stderr,
		exitIOch:    make(chan struct{}),
		stdinCloser: make(chan struct{}),
		bundle:      r.Bundle,
		cType:       cType,
		status:      task.Status_CREATED,
		terminal:    r.Terminal,
		mounted:     mounted,
		pid:         shimPid,
		execs:       make(map[string]*execTask),
	}

	return c, nil
}

func (c *shimContainer) ioExit() {
	log.Debugf("close shim container io channel")
	if c == nil {
		return
	}
	c.exitIoOnce.Do(func() {
		close(c.exitIOch)
	})
}
