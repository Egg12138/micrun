package micantainer

import (
	"context"
	"fmt"
	er "micrun/errors"
	log "micrun/logger"
	"micrun/pkg/libmica"
	"micrun/pkg/pedestal"
	"sync"

	"github.com/pkg/errors"
)

// Status is a graph of the sanbox, contains more than state
type SandboxStatus struct {
	ContainersState []ContainerStatus
	Annotations     map[string]string
	ID              string
	State           SandboxState
}

// expand fields of sandboxconfigs as sandbox memebers
type Sandbox struct {
	ctx context.Context
	// use annoymous field to avoid unused fields wanring
	sync.Mutex
	// fs, storage, devices, volumes...
	// monitor
	resManager SandboxResource
	config     *SandboxConfig
	containers map[string]*Container
	id         string
	network    Network
	state      SandboxState

	vcpuAlreadyPinned bool

	annotaLock *sync.RWMutex
	wg         *sync.WaitGroup
}

// impl SandboxTraits for Sandbox
func (s *Sandbox) GetAllContainers() []ContainerTraits {
	list := make([]ContainerTraits, 0, len(s.containers))
	for _, c := range s.containers {
		list = append(list, c)
	}
	return list
}

func (s *Sandbox) SandboxID() string {
	return s.id
}

func (s *Sandbox) Annotation(key string) (string, error) {
	s.annotaLock.RLock()
	defer s.annotaLock.RUnlock()
	value, found := s.config.Annotations[key]
	if !found {
		return "", fmt.Errorf("annotation not found: %s", key)
	}
	return value, nil
}

// TALK: diffult to do it?
func (s *Sandbox) Monitor() {
}

func (s *Sandbox) GetNetNamespace() string {
	return s.network.NetID()
}

func (s *Sandbox) NetnsHolderPID() int {
	if cfg := s.config; cfg != nil {
		return cfg.NetworkConfig.HolderPid
	}
	return 0
}

func (s *Sandbox) Start(ctx context.Context) error {
	cur := s.state.State
	log.Debugf("current sandbox state=%s", cur)

	//  If restored as 'creating', normalize to 'ready' before starting
	if cur == StateCreating {
		if err := s.setSandboxState(StateReady); err != nil {
			return err
		}
		cur = s.state.State
	}

	// If already running, ensure all containers are running
	if cur == StateRunning {
		log.Debugf("sandbox %s already running, checking containers", s.id)
		for _, c := range s.containers {
			if c.checkState() != StateRunning {
				if err := c.start(ctx); err != nil {
					return err
				}
			}
		}
		if err := s.StoreSandbox(ctx); err != nil {
			return err
		}
		return nil
	}

	if err := s.state.Transition(cur, StateRunning); err != nil {
		log.Debugf("transition error: from=%s to=%s", cur, StateRunning)
		return err
	}

	oldState := cur
	if err := s.setSandboxState(StateRunning); err != nil {
		return fmt.Errorf("set Sandbox state error: %v", err)
	}
	log.Debugf("sandbox state: %s -> %s", oldState, s.state.State)

	var startErr error
	defer func() {
		if startErr != nil {
			s.setSandboxState(oldState)
		}
	}()

	for _, c := range s.containers {
		if startErr = c.start(ctx); startErr != nil {
			return startErr
		}
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return err
	}

	return nil

}

// Stop stops all containers inside the sandbox as well as sandbox itself
// For a forced stopping,  ignore container stop failures
func (s *Sandbox) Stop(ctx context.Context, force bool) error {

	if s.state.State == StateStopped {
		return nil
	}

	if err := s.state.Transition(s.state.State, StateStopped); err != nil {
		return err
	}

	for _, c := range s.containers {
		if err := c.stop(ctx, force); err != nil {
			return err
		}
	}

	if err := s.stopClients(ctx); err != nil && !force {
		return err
	}

	log.Debug("stop monitor and console")

	if err := s.setSandboxState(StateStopped); err != nil {
		return err
	}

	if err := s.removeNetwork(); err != nil && !force {
		return err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return err
	}

	return nil
}

// Stop rtos clients && sandbox
func (s *Sandbox) Delete(ctx context.Context) error {

	if s.state.State != StateReady &&
		s.state.State != StatePaused &&
		s.state.State != StateStopped {
		return fmt.Errorf("sandbox is not ready, paused, or stopped, cannot delete")
	}

	for _, c := range s.containers {
		if err := c.delete(ctx); err != nil {
			log.Errorf("failed to delete container %s", c.id)
		}
	}

	if s.monitor != nil {
		s.monitor.stop()
	}
	return s.cleanSandboxStorage()

}

// CreateContainer creates a new container in the sandbox
// This should be called only when the sandbox is already created.
// It will add new container config to sandbox.config.Containers
func (s *Sandbox) CreateContainer(ctx context.Context, config ContainerConfig) (ContainerTraits, error) {

	id := config.ID
	if _, ok := s.containers[id]; ok {
		log.Errorf("container %s already exists", id)
		return nil, er.AlreadyExists
	}
	s.config.ContainerConfigs[id] = &config
	if s.config.InfraOnly && !config.IsInfra {
		s.config.InfraOnly = false
	}
	newc := s.config.ContainerConfigs[id]

	var err error
	defer func() {
		if err != nil {
			if len(s.config.ContainerConfigs) > 0 {
				delete(s.config.ContainerConfigs, id)
			}
		}
	}()

	c, err := newContainer(ctx, s, newc)
	if err != nil {
		return nil, err
	}

	// Validate the container after creation but before starting
	if !c.validMicaContainer() {
		return nil, fmt.Errorf("invalid mica container: %v", c)
	}

	if err = c.create(ctx); err != nil {
		return nil, err
	}

	if err = s.addContainer(c); err != nil {
		return nil, err

	}
	defer func() {
		if err == nil {
			return
		}

		log.Errorf("failed to create container %s: %v", id, err)

		if errStop := c.stop(ctx, true); errStop != nil {
			log.Errorf("failed to stop container %s after creation failure: %v", id, errStop)
		}
		log.Debug("remove stopped container from sandbox")
		s.removeContainer(c.id)
	}()

	if err = s.checkVCPUsPinning(ctx); err != nil {
		return nil, err
	}

	// update sandbox status
	if err = s.StoreSandbox(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) removeContainer(containerID string) error {
	log.Debugf("remove container %s", containerID)
	if s == nil {
		return fmt.Errorf("sandbox is nil")
	}

	if containerID == "" {
		return er.EmptyContainerID
	}

	if _, ok := s.containers[containerID]; !ok {
		return errors.Wrapf(er.ContainerNotFound, "Could not remove the container %q from the sandbox %q containers list",
			containerID, s.id)
	}

	delete(s.containers, containerID)
	return nil
}

func (s *Sandbox) DeleteContainer(ctx context.Context, id string) (ContainerTraits, error) {
	log.Debugf("delete container %s from sandbox", id)
	if s == nil {
		return nil, er.SandboxNotFound
	}
	if id == "" {
		return nil, er.EmptyContainerID
	}

	c, ok := s.containers[id]
	if !ok {
		return nil, er.ContainerNotFound
	}

	if err := c.delete(ctx); err != nil {
		return nil, err
	}

	// Guard nil config; delete from container configs if present
	if s.config != nil {
		delete(s.config.ContainerConfigs, id)
	}

	// Clean resManager per-container mirrors if present
	if s.resManager.ContainerCpuSets != nil {
		delete(s.resManager.ContainerCpuSets, id)
	}
	if s.resManager.ContainerVcpus != nil {
		delete(s.resManager.ContainerVcpus, id)
	}

	// Explicitly refresh aggregated resources; debounce/logging inside updateResources
	if err := s.updateResources(ctx); err != nil {
		log.Debugf("ignore updateResources error after delete %s: %v", id, err)
	}

	if err := s.checkVCPUsPinning(ctx); err != nil {
		return nil, err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) StartContainer(ctx context.Context, id string) (ContainerTraits, error) {
	c, ok := s.containers[id]
	if !ok {
		return nil, er.ContainerNotFound
	}

	// start client os, os start the task from entry inside the OS image
	if err := c.start(ctx); err != nil {
		return nil, err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return nil, err
	}

	if err := s.checkVCPUsPinning(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) StopContainer(ctx context.Context, id string, force bool) (ContainerTraits, error) {
	c, ok := s.containers[id]
	if !ok {
		return nil, er.ContainerNotFound
	}
	if err := c.stop(ctx, force); err != nil {
		return nil, err
	}

	if err := s.StoreSandbox(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// Stop the container forcely and pop it.
func (s *Sandbox) KillContainer(ctx context.Context, id string) (ContainerTraits, error) {
	c, ok := s.containers[id]
	if !ok {
		return nil, er.ContainerNotFound
	}

	if libmica.ClientNotExist(c.id) {
		return c, nil
	}

	if err := c.kill(); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Sandbox) StatusContainer(id string) (ContainerStatus, error) {
	cs := ContainerStatus{}
	if id == "" {
		log.Debugf("status container: empty id")
		return cs, er.EmptyContainerID
	}

	if c, ok := s.containers[id]; ok {
		if _, err := c.ensureClientPresence(); err != nil {
			return cs, err
		}

		if c.checkState() == StateDown {
			return cs, er.ContainerNotFound
		}

		rootfs := c.config.Rootfs.Source
		if c.config.Rootfs.Mounted {
			rootfs = c.config.Rootfs.Target
		}

		// TODO: no need to store starttime in taskinfo, collapsing is unneeded
		cs.Spec = nil
		cs.State = c.state
		cs.ID = c.id
		cs.Rootfs = rootfs
		cs.Pid = c.GetPid()
		cs.Annotations = c.config.Annotations
		return cs, nil
	}
	log.Debugf("container %s not found in sandbox %s", id, s.id)
	return cs, nil
}

// Update resource for changed resource
func updateContainerResource(c *Container, updated *pedestal.EssentialResource) error {
	if c == nil {
		return fmt.Errorf("missing container reference when updating resources")
	}
	exec := &c.me
	old := exec.ReadResource()

	log.Debugf("Resource update for container %s: old=%s, new=%s",
		c.id, formatResourceForLog(old), formatResourceForLog(updated))

	// Nil-safety checks for all pointer fields
	if updated.CpuCpacity != nil {
		if exec.NeedUpdateCpuCap(*updated.CpuCpacity) {
			err := exec.UpdateCPUCapacity(*updated.CpuCpacity)
			if err != nil {
				return fmt.Errorf("failed to update cpu capacity of %s: %v", c.id, err)
			}
			if *updated.CpuCpacity == 0 {
				log.Infof("container %s's cpu capacity is unlimited", c.id)
			}
		}
	}

	if updated.MemoryMaxMB != nil {
		if exec.NeedUpdateMemLimit(*updated.MemoryMaxMB) {
			err := exec.EnsureMemoryLimit(*updated.MemoryMaxMB)
			if err != nil {
				return fmt.Errorf("failed to update max memory of %s: %v", c.id, err)
			}
		}
	}

	if exec.NeedUpdateCpuSet(old.ClientCpuSet, updated.ClientCpuSet) {
		err := exec.UpdatePCPUConstrains(updated.ClientCpuSet)
		if err != nil {
			return fmt.Errorf("failed to update cpuset of vcpu: %v", err)
		}
	}

	if updated.CPUWeight != nil {
		if exec.NeedUpdateCpuShare(*updated.CPUWeight) {
			err := exec.UpdateCPUWeight(*updated.CPUWeight)
			if err != nil {
				return fmt.Errorf("failed to set a different cpu weight for %s: %v", c.id, err)
			}
		}
	}

	if old.Vcpu != nil && updated.Vcpu != nil {
		if exec.NeedUpdateVCpus(*updated.Vcpu) {
			old, newer, err := exec.UpdateVCPUNum(*updated.Vcpu)
			if err != nil {
				log.Warnf("failed to update vcpu number: %v", err)
			}
			if old != newer {
				log.Infof("update vcpu number from %d to %d", old, newer)
			}
		}
	}

	return nil
}
