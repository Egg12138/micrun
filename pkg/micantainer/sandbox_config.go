package micantainer

import (
	ped "micrun/pkg/pedestal"
)

type SandboxConfig struct {
	ID                 string
	Hostname           string
	NetworkConfig      NetworkConfig
	PedConfig          ped.PedestalConfig
	ContainerConfigs   map[string]*ContainerConfig
	Annotations        map[string]string
	SharedMemorySize   uint64
	SandboxResources   SandboxResourceSizing
	EnableVCPUsPining  bool
	StaticResourceMgmt bool
	HugePageSupport    bool
	InfraOnly          bool
}
