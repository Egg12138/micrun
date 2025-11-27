package micantainer

import (
	"context"
	"sync"
)

// expand fields of sandboxconfigs as sandbox memebers
type Sandbox struct {
	ctx context.Context
	// use annoymous field to avoid unused fields wanring
	sync.Mutex
	// fs, storage, devices, volumes...
	// monitor
	resManager SandboxAgent
	config     *SandboxConfig
	containers map[string]*Container
	id         string
	network    Network
	state      SandboxState

	vcpuAlreadyPinned bool

	annotaLock *sync.RWMutex
	wg         *sync.WaitGroup
}
