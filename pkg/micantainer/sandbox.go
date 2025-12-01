package micantainer

import (
	"context"
	"sync"
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
