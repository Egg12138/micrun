package micantainer

import (
	"context"
	"micrun/pkg/libmica"
	ped "micrun/pkg/pedestal"
	"sync"

	"github.com/opencontainers/runtime-spec/specs-go"
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

	// 	// LegacyPty specifies whether to use legacy PTY mode (true) or micad's rpmsg PTY (false)
	LegacyPty bool `json:"legacy_pty"`

	// Cmdline is the boot command line for the guest.
}
