package micantainer

import (
	"context"
	"encoding/json"
	"fmt"
	er "micrun/errors"
	log "micrun/logger"
	"micrun/pkg/libmica"
	"micrun/pkg/netns"
	ped "micrun/pkg/pedestal"
	"micrun/pkg/utils"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

// Container represents a single container instance, encapsulating its configuration,
// state, and relationship with a sandbox.
type Container struct {
	ctx           context.Context
	id            string
	me            libmica.MicaExecutor
	config        *ContainerConfig
	sandbox       *Sandbox
	sandboxId     string
	mounts        []Mount
	rootfs        RootFs
	containerPath string // The path relative to the root bundle: <bundleRoot>/<sandboxID>/<containerID>.
	state         ContainerState
	exitNotifier  chan struct{}
	exitOnce      sync.Once
	infraCmd      *exec.Cmd
	infraExitCh   chan helperCh
}

type ContainerConfig struct {
	ID             string
	Rootfs         RootFs
	Mount          []Mount
	ReadOnlyRootfs bool
	IsInfra        bool
	Pid            int // Pid is typically the shim pid.
	Annotations    map[string]string
	Resources      *specs.LinuxResources

	// ImageAbsPath is the absolute path of the <RTOS> image in the host required by mica
	ImageAbsPath string      `json:"elf_abs_path"`
	PedestalType ped.PedType `json:"pedestal_type"`
	PedestalConf string      `json:"pedestal_conf"`
	OS           string      `json:"os"`

	// VCPUNum is the number of virtual CPUs. Matches the configured CPU capacity when not pinning; otherwise, equals the size of the cpuset.
	VCPUNum uint32 `json:"vcpu_num"`
	// PCPUNum is the number of allocated physical CPUs.
	// TODO: Implement for openAMP and Jailhouse cases.
	PCPUNum int `json:"ncpu"`
	// MaxVcpuNum is the pedestal max virtual CPUs configured for this container.
	MaxVcpuNum uint32 `json:"max_vcpu_num"`

	// MemoryThresholdMB is the pedestal maximum allocable memory in MiB.
	MemoryThresholdMB uint32 `json:"memory_threshold"`

	// 	// LegacyPty specifies whether to use legacy PTY mode (true) or micad's rpmsg PTY (false)
	LegacyPty bool `json:"legacy_pty"`

	// Cmdline is the boot command line for the guest.
	// TODO: consider passing the cmdline as a parameter to the pty, acting as if we "execute" command
	Cmdline string `json:"cmdline"`
}

// Noop writer/reader are used for infra container which never has PTY or IO.
type noopWriteCloser struct{}

func (noopWriteCloser) Write(p []byte) (int, error) {
	return len(p), nil
}

func (noopWriteCloser) Close() error {
	return nil
}

type helperCh struct {
	code int
	err  error
}

// newContainer creates a new container struct instance.
// It assumes that the container config is already parsed.
func newContainer(ctx context.Context, s *Sandbox, cc *ContainerConfig) (*Container, error) {
	if cc == nil {
		return &Container{}, fmt.Errorf("container config is none")
	}

	if cc.ID == "" {
		log.Debugf("Empty container id.")
		return &Container{}, er.EmptyContainerID
	}

	c := &Container{
		id:            cc.ID,
		me:            libmica.MicaExecutor{Id: cc.ID},
		sandbox:       s,
		sandboxId:     s.id,
		config:        cc,
		rootfs:        cc.Rootfs,
		containerPath: filepath.Join(s.id, cc.ID),
		mounts:        cc.Mount,
		state:         ContainerState{State: StateDown},
		ctx:           s.ctx,
	}

	if err := c.RestoreState(); err != nil {
		log.Warnf("Failed to restore container state: %v.", err)
	}

	c.updateExitNotifier(c.checkState())

	return c, nil
}

// CleanupContainer stops and deletes a container and its associated sandbox if it's the last one.
// NOTICE: This function is designed for exclusive cleanup operations.
func CleanupContainer(ctx context.Context, sandboxID string, containerID string, force bool) error {
	log.Debugf("Cleaning up sandbox %s, container %s.", sandboxID, containerID)
	if sandboxID == "" {
		return er.EmptySandboxID
	}

	if containerID == "" {
		return er.EmptyContainerID
	}

	sandbox, err := loadSandbox(ctx, sandboxID)
	if err != nil {
		if err == er.SandboxNotFound {
			if !libmica.ClientNotExist(containerID) && !force {
				return fmt.Errorf("sandbox state missing while client %s still exists", containerID)
			}
			log.Debugf("Sandbox %s already removed from disk, skipping container %s cleanup.", sandboxID, containerID)
			return nil
		}
		return err
	}

	if _, err = sandbox.StopContainer(ctx, containerID, force); err != nil {
		if err != er.ContainerNotFound && !force {
			return err
		}
		log.Debugf("Container %s already stopped or absent in sandbox %s: %v.", containerID, sandboxID, err)
	}

	if _, err = sandbox.DeleteContainer(ctx, containerID); err != nil {
		if err != er.ContainerNotFound && !force {
			return err
		}
		log.Debugf("Container %s already deleted from sandbox %s: %v.", containerID, sandboxID, err)
	}

	if len(sandbox.containers) > 0 {
		return nil
	}

	if err = sandbox.Stop(ctx, force); err != nil && !force {
		return err
	}

	if err = sandbox.Delete(ctx); err != nil {
		return err
	}

	return nil
}

// start begins the execution of the container.
func (c *Container) start(ctx context.Context) error {
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}

	if c.config != nil && c.config.IsInfra {
		if currentState == StateRunning {
			return nil
		}
		if currentState != StateReady && currentState != StateStopped {
			return fmt.Errorf("container is not ready or stopped, cannot start")
		}
		if err := c.state.ValidTransition(currentState, StateRunning); err != nil {
			return err
		}
		return c.setContainerState(ctx, StateRunning)
	}

	if currentState == StateRunning {
		return fmt.Errorf("container %s is already running", c.id)
	}

	if currentState != StateReady && currentState != StateStopped {
		return fmt.Errorf("container is not ready or stopped, cannot start")
	}

	if err := c.state.Transition(currentState, StateRunning); err != nil {
		return err
	}

	if err := startClient(ctx, c.sandbox, c); err != nil {
		log.Warnf("Failed to start container: %v, stopping it", err)
		if err := c.stop(ctx, true); err != nil {
			log.Warn("Failed to stop the container after start failed.")
		}
	}

	return c.setContainerState(ctx, StateRunning)
}

func (c *Container) startInfraProcess(ctx context.Context) error {
	if c.infraCmd != nil {
		return nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get cwd: %v", err)
	}

	bundle, err := utils.ValidBundle(c.id, cwd)
	if err != nil {
		return err
	}

	spec, err := loadSpecFromBundle(bundle)
	if err != nil {
		return fmt.Errorf("failed to load sandbox spec: %w", err)
	}

	nsenterPath, err := exec.LookPath("nsenter")
	if err != nil {
		return fmt.Errorf("nsenter not found, unable to join netnamespace: %w", err)
	}

	rootfs := filepath.Join(bundle, "rootfs")
	if c.config.Rootfs.Target != "" {
		rootfs = c.config.Rootfs.Target
	}

	netPath := ""
	if c.sandbox != nil && c.sandbox.config != nil {
		netPath = c.sandbox.config.NetworkConfig.NetworkID
	}

	args, err := genNsenterArgs(spec, rootfs, netPath)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, nsenterPath, args...)

	env := assembleHelperEnv(spec)
	cmd.Env = env

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:    true,
		Pdeathsig: syscall.SIGKILL,
	}

	devNull, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open /dev/null: %w", err)
	}
	defer devNull.Close()

	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start sandbox pause helper: %w", err)
	}

	c.infraCmd = cmd
	c.infraExitCh = make(chan helperCh, 1)

	go c.monitorInfraExit(cmd)

	c.config.Pid = cmd.Process.Pid
	if c.sandbox != nil && c.sandbox.config != nil {
		prev := c.sandbox.config.NetworkConfig.HolderPid
		c.sandbox.config.NetworkConfig.HolderPid = cmd.Process.Pid
		if c.sandbox.config.NetworkConfig.NetworkCreated && prev > 0 && prev != cmd.Process.Pid {
			if err := netns.Cleanup(c.sandbox.id, prev); err != nil && !errors.Is(err, os.ErrProcessDone) {
				log.Warnf("failed to cleanup previous netns holder %d: %v", prev, err)
			}
		}
	}

	return nil
}

func (c *Container) monitorInfraExit(cmd *exec.Cmd) {
	err := cmd.Wait()
	exitCode := extractExitCode(err)
	if c.infraExitCh != nil {
		c.infraExitCh <- helperCh{code: exitCode, err: nil}
		close(c.infraExitCh)
	}
	c.infraCmd = nil
	c.infraExitCh = nil
	if c.config != nil {
		c.config.Pid = 0
	}
	if c.sandbox != nil && c.sandbox.config != nil {
		c.sandbox.config.NetworkConfig.HolderPid = 0
	}
}

func genNsenterArgs(spec specs.Spec, rootfs, fallbackNetPath string) ([]string, error) {
	if spec.Process == nil || len(spec.Process.Args) == 0 {
		return nil, fmt.Errorf("invalid sandbox process definition")
	}

	args := make([]string, 0)
	nsSeen := make(map[specs.LinuxNamespaceType]struct{})
	if spec.Linux != nil {
		for _, ns := range spec.Linux.Namespaces {
			if ns.Path == "" {
				continue
			}
			switch ns.Type {
			case specs.NetworkNamespace:
				args = append(args, "--net="+ns.Path)
				nsSeen[specs.NetworkNamespace] = struct{}{}
			case specs.IPCNamespace:
				args = append(args, "--ipc="+ns.Path)
			case specs.UTSNamespace:
				args = append(args, "--uts="+ns.Path)
			case specs.PIDNamespace:
				args = append(args, "--pid="+ns.Path)
			case specs.UserNamespace:
				args = append(args, "--user="+ns.Path)
			case specs.MountNamespace:
				args = append(args, "--mount="+ns.Path)
			}
		}
	}

	if fallbackNetPath != "" {
		if _, ok := nsSeen[specs.NetworkNamespace]; !ok {
			args = append(args, "--net="+fallbackNetPath)
		}
	}

	if rootfs != "" {
		args = append(args, "--root="+rootfs)
	}
	if spec.Process.Cwd != "" {
		args = append(args, "--wd="+spec.Process.Cwd)
	}

	args = append(args, "--")
	args = append(args, spec.Process.Args...)
	return args, nil
}

func assembleHelperEnv(spec specs.Spec) []string {
	env := append([]string{}, os.Environ()...)

	if spec.Process != nil && len(spec.Process.Env) > 0 {
		env = mergeEnv(env, spec.Process.Env)
	}

	if !envHasKey(env, "PATH") {
		env = append(env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}
	return env
}

func envHasKey(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

func mergeEnv(base, override []string) []string {
	result := append([]string{}, base...)
	index := make(map[string]int, len(result))
	for i, kv := range result {
		if pos := strings.Index(kv, "="); pos >= 0 {
			index[kv[:pos]] = i
		}
	}

	for _, kv := range override {
		if pos := strings.Index(kv, "="); pos >= 0 {
			key := kv[:pos]
			if idx, ok := index[key]; ok {
				result[idx] = kv
			} else {
				index[key] = len(result)
				result = append(result, kv)
			}
		}
	}
	return result
}

func loadSpecFromBundle(bundle string) (specs.Spec, error) {
	configPath := filepath.Join(bundle, "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return specs.Spec{}, fmt.Errorf("failed to read %s: %w", configPath, err)
	}
	var spec specs.Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return specs.Spec{}, fmt.Errorf("failed to unmarshal %s: %w", configPath, err)
	}
	return spec, nil
}

// create prepares the container to be started.
func (c *Container) create(ctx context.Context) error {
	if c.config != nil && c.config.IsInfra {
		return c.setContainerState(ctx, StateReady)
	}

	rtosTask, err := initContainerTaskInSandbox(c.sandbox, c.config)
	if err != nil {
		return err
	}

	if _, err := c.ensureClientPresence(); err != nil {
		return err
	}

	if err := c.setContainerState(ctx, StateReady); err != nil {
		return err
	}
	return nil
}

// doStop performs the actual stop operation on the client.
func (c *Container) doStop(force bool) error {
	if c.config != nil && c.config.IsInfra {
		if c.infraCmd == nil || c.infraCmd.Process == nil {
			return nil
		}
		if err := c.infraCmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		return nil
	}
	currentState := c.checkState()
	if currentState == StateStopped {
		log.Debugf("Container %s is already stopped.", c.id)
		return nil
	}

	if err := c.state.ValidTransition(currentState, StateStopped); err != nil && !force {
		return err
	}

	if err := libmica.Stop(c.ID()); err != nil {
		return err
	}
	return nil
}

// stop stops the container.
// for semantic continuation, register client at micad even if client is not here
func (c *Container) stop(ctx context.Context, force bool) error {
	if _, err := c.ensureClientPresence(); err != nil {
		return err
	}

	var err error
	if err = c.doStop(force); err != nil {
		log.Debugf("failed to stop container %s: %v", c.id, err)
		return err
	}
	log.Debugf("container %s stopped", c.id)

	if err = c.setContainerState(ctx, StateStopped); err != nil {
		return err
	}

	return nil
}

// kill forcibly stops the container.
// Due to the 1:1:1 relationship of Container:ClientOS:Task in mica, kill() is essentially stop().
func (c *Container) kill() error {

	if c.sandbox == nil {
		return fmt.Errorf("container sandbox is nil")
	}
	if c.sandbox.state.State != StateReady && c.sandbox.state.State != StateRunning {
		return fmt.Errorf("sandbox is not running or ready, can not signal container")
	}
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}
	log.Debugf("Container state is %s.", currentState)

	if libmica.ClientNotExist(c.id) {
		return c.setContainerState(c.ctx, StateStopped)
	} else if err := c.doStop(true); err != nil {
		log.Debugf("failed to stop container %s: %v", c.id, err)
		return err
	}
	log.Debugf("container %s stopped", c.id)

	if err := c.setContainerState(c.ctx, StateStopped); err != nil {
		return err
	}
	return nil
}

// delete removes the container.
// This differs from mica, where `rm` forces a client stop. For a container engine, that is bad practice.
func (c *Container) delete(ctx context.Context) error {
	if c.sandbox == nil {
		return fmt.Errorf("container sandbox reference is nil")
	}
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}
	if currentState != StateReady &&
		currentState != StatePaused &&
		currentState != StateStopped {
		return fmt.Errorf("sandbox is not ready, paused, or stopped, cannot delete container")
	}

	if c.config == nil || !c.config.IsInfra {
		if err := libmica.Remove(c.id); err != nil {
			log.Debugf("Failed to remove container %s.", err)
			return err
		}
	}
	if err := c.sandbox.removeContainer(c.id); err != nil {
		return err
	}
	if err := c.sandbox.StoreSandbox(ctx); err != nil {
		return fmt.Errorf("failed to store sandbox")
	}
	if err := utils.RemoveContainerCacheDir(c.id); err != nil {
		log.Warnf("failed to remove cache directory for container %s: %v", c.id, err)
	}
	return nil
}

// pause pauses the container's execution.
func (c *Container) pause(ctx context.Context) error {
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}
	if currentState != StateRunning {
		return fmt.Errorf("container is not running, cannot pause container")
	}
	if c.config != nil && c.config.IsInfra {
		return c.setContainerState(ctx, StatePaused)
	}
	if err := libmica.Pause(c.id); err != nil {
		return er.MicadOpFailed
	}
	return c.setContainerState(ctx, StatePaused)
}

// resume resumes a paused container.
func (c *Container) resume(ctx context.Context) error {
	if c.sandbox == nil {
		return fmt.Errorf("container sandbox reference is nil")
	}
	currentState, err := c.ensureClientPresence()
	if err != nil {
		return err
	}
	if currentState != StatePaused && c.sandbox.state.State != StateStopped {
		return fmt.Errorf("container is not paused, cannot resume container")
	}
	if c.config != nil && c.config.IsInfra {
		return c.setContainerState(ctx, StateRunning)
	}
	log.Debugf("resuming container %s (restarting RTOS)", c.id)
	if err := libmica.Start(c.id); err != nil {
		return er.MicadOpFailed
	}
	return c.setContainerState(ctx, StateRunning)
}

func (c *Container) update(ctx context.Context, resources specs.LinuxResources) error {
	if c.config != nil && c.config.IsInfra {
		return nil
	}
	if c.sandbox == nil {
		return fmt.Errorf("container sandbox reference is nil")
	}
	if c.sandbox.state.State != StateRunning {
		return fmt.Errorf("sandbox is not running, cannot update container")
	}
	if c.notOperational() {
		return fmt.Errorf("container not ready or running, cannot update")
	}
	if err := c.validateUpdate(); err != nil {
		return err
	}

	pedRes, hasUpdates := c.extractChanges(resources)
	if !hasUpdates {
		return nil
	}

	return c.applyChanges(ctx, pedRes, resources)
}

func (c *Container) validateUpdate() error {

	if c.config == nil {
		return fmt.Errorf("container config is nil")
	}
	if c.config.Resources == nil {
		c.config.Resources = &specs.LinuxResources{}
	}
	if c.config.Resources.CPU == nil {
		c.config.Resources.CPU = &specs.LinuxCPU{}
	}
	if c.config.Resources.Memory == nil {
		c.config.Resources.Memory = &specs.LinuxMemory{}
	}
	return nil
}

func (c *Container) extractChanges(resources specs.LinuxResources) (*ped.EssentialResource, bool) {
	pedRes := ped.InitResource()
	pedRes.MemoryMaxMB = nil
	pedRes.CPUWeight = nil
	hasUpdates := false

	if cpu := resources.CPU; cpu != nil {
		if cpu.Period != nil && *cpu.Period != 0 {
			hasUpdates = true
		}
		if cpu.Quota != nil && *cpu.Quota != 0 {
			hasUpdates = true
		}
		if cpu.Cpus != "" {
			pedRes.ClientCpuSet = cpu.Cpus
			hasUpdates = true
		}
		if cpu.Shares != nil {
			weight := ped.ShareToWeight(*cpu.Shares)
			weightCopy := weight
			pedRes.CPUWeight = &weightCopy
			hasUpdates = true
		}
	}

	if mem := resources.Memory; mem != nil && mem.Limit != nil {
		limitMiB := uint32(*mem.Limit >> 20)
		pedRes.MemoryMinMB = limitMiB
		pedRes.MemoryMaxMB = copyUint32(limitMiB)
		hasUpdates = true
	}

	return pedRes, hasUpdates
}

func (c *Container) applyChanges(ctx context.Context, pedRes *ped.EssentialResource, resources specs.LinuxResources) error {
	if err := updateContainerResource(c, pedRes); err != nil {
		return err
	}

	res := c.config.Resources

	if cpu := resources.CPU; cpu != nil {
		if cpu.Period != nil && *cpu.Period != 0 {
			res.CPU.Period = cpu.Period
		}
		if cpu.Quota != nil && *cpu.Quota != 0 {
			res.CPU.Quota = cpu.Quota
		}
		if cpu.Cpus != "" {
			res.CPU.Cpus = cpu.Cpus
		}
		if cpu.Shares != nil {
			sharesCopy := *cpu.Shares
			res.CPU.Shares = &sharesCopy
		}
	}

	if mem := resources.Memory; mem != nil && mem.Limit != nil {
		res.Memory.Limit = mem.Limit
	}

	if err := c.sandbox.updateResources(ctx); err != nil {
		log.Debugf("Update best-effort: ignore sandbox.updateResources error for %s: %v", c.id, err)
	}

	return nil
}

// Traits:
func (c *Container) ID() string {
	return c.id
}

func (c *Container) GetAnnotations() map[string]string {
	return c.config.Annotations
}

func (c *Container) GetPid() int {
	return c.config.Pid
}

func (c *Container) GetMemoryLimit() uint64 {
	return uint64(c.config.memoryLimitMB())
}

func (c *Container) Sandbox() SandboxTraits {
	return c.sandbox
}

func (c *Container) Status() StateString {
	return c.checkState()
}

func (c *Container) State() *ContainerState {
	c.checkState()
	return &c.state
}
