package shim

import (
	"context"
	"fmt"
	defs "micrun/definitions"
	log "micrun/logger"
	cntr "micrun/pkg/micantainer"
	oci "micrun/pkg/oci"
	"micrun/pkg/utils"
	"path/filepath"

	taskAPI "github.com/containerd/containerd/api/runtime/task/v2"
	"github.com/containerd/containerd/api/types"
	"github.com/containerd/containerd/mount"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func create(ctx context.Context, s *shimService, r *taskAPI.CreateTaskRequest) (*shimContainer, error) {
	if err := setupStateDir(); err != nil {
		log.Debugf("failed to setup micrun state directory: %v", err)
	}

	rootfs := cntr.RootFs{}
	// the first of r.Rootfs is the bundle rootfs
	if len(r.Rootfs) == 1 {
		mnt := r.Rootfs[0]
		rootfs.Source = mnt.Source
		rootfs.Type = mnt.Type
		rootfs.Options = mnt.Options
	}

	detach := r.Terminal
	ociSpec, bundlePath, err := loadSpec(r.ID, r.Bundle)

	if err != nil {
		return nil, err
	}

	containerType, err := oci.GetContainerType(ociSpec)
	if err != nil {
		return nil, err
	}

	disableOutput := detach && ociSpec.Process.Terminal
	rootfsPath := filepath.Join(r.Bundle, "rootfs")
	runtimeConfig, err := loadRuntimeConfig(s, r, ociSpec.Annotations)

	if err := handleContainerTypeCreation(ctx, s, containerType, r, ociSpec, runtimeConfig, bundlePath, rootfsPath, disableOutput, &rootfs); err != nil {
		return nil, err
	}

	container, err := newContainer(s, r, containerType, ociSpec, rootfs.Mounted)
	if err != nil {
		return nil, err
	}

	if containerType == cntr.PodSandbox && s.sandbox != nil {
		if pid := s.sandbox.NetnsHolderPID(); pid > 0 {
			container.pid = uint32(pid)
		}
	}

	return container, nil
}

func handleContainerTypeCreation(ctx context.Context, s *shimService, containerType cntr.ContainerType,
	r *taskAPI.CreateTaskRequest, ociSpec *specs.Spec, runtimeConfig *oci.RuntimeConfig,
	bundlePath, rootfsPath string, disableOutput bool, rootfs *cntr.RootFs) error {
	switch containerType {
	case cntr.PodSandbox, cntr.SingleContainer:
		return createSandboxContainer(ctx, s, containerType, r, ociSpec, runtimeConfig, bundlePath, rootfsPath, disableOutput, rootfs)
	case cntr.PodContainer:
		return createPodContainer(ctx, s, r, ociSpec, bundlePath, rootfsPath, disableOutput, rootfs)
	default:
		return fmt.Errorf("unsupported container type: %v", containerType)
	}
}

func createSandboxContainer(ctx context.Context, s *shimService, containerType cntr.ContainerType,
	r *taskAPI.CreateTaskRequest, ociSpec *specs.Spec, runtimeConfig *oci.RuntimeConfig,
	bundlePath, rootfsPath string, disableOutput bool, rootfs *cntr.RootFs) (err error) {
	if s.sandbox != nil {
		return fmt.Errorf("cannot create an existing sandbox: %s", s.sandbox.SandboxID())
	}

	s.config = runtimeConfig
	if containerType == cntr.PodSandbox {
		s.config.SandboxCPUs, s.config.SandboxMemMB = oci.CalculateSandboxSizing(ociSpec)
	} else {
		s.config.SandboxCPUs, s.config.SandboxMemMB = oci.CalculateContainerSizing(ociSpec)
	}

	if containerType != cntr.PodSandbox {
		log.Debug("rootfs mounted for single container, showing rootfs contents:")
		utils.TravelDir(r.Rootfs[0].GetSource())
	}

	if errC := mountRootfs(rootfsPath, r.Rootfs); errC != nil {
		return errC
	}
	rootfs.Mounted = true

	defer func() {
		if err != nil && rootfs.Mounted {
			if errUmnt := mount.UnmountAll(rootfsPath, 0); errUmnt != nil {
				log.Warnf("failed to clean up rootfs mount: %v", errUmnt)
			}
		}
	}()

	if containerType != cntr.PodSandbox {
		log.Debug("rootfs mounted for single container, showing rootfs contents:")
		utils.TravelDir(rootfsPath)
	}

	var sandbox cntr.SandboxTraits
	sandbox, err = createSandbox(ctx, ociSpec, runtimeConfig, *rootfs, r.ID, bundlePath, disableOutput)
	if err != nil {
		return err
	}

	s.sandbox = sandbox
	return nil
}

func createPodContainer(ctx context.Context, s *shimService, r *taskAPI.CreateTaskRequest,
	ociSpec *specs.Spec, bundlePath, rootfsPath string,
	disableOutput bool, rootfs *cntr.RootFs) (err error) {
	if s.sandbox == nil {
		return fmt.Errorf("cannot start the pod container, since the sandbox is not created")
	}

	if errC := mountRootfs(rootfsPath, r.Rootfs); errC != nil {
		return errC
	}
	rootfs.Mounted = true

	defer func() {
		if err != nil && rootfs.Mounted {
			if errUmnt := mount.UnmountAll(rootfsPath, 0); errUmnt != nil {
				log.Warnf("Failed to cleanup rootfs mount: %v.", errUmnt)
			}
		}
	}()

	log.Debug("rootfs mounted for pod container, showing rootfs contents: ")
	utils.TravelDir(rootfsPath)

	return createPodContainerInSandbox(ctx, s.sandbox, *ociSpec, *rootfs, r.ID, bundlePath, s.config, disableOutput)
}

// mountRootfs mounts the container's root filesystem.
// TODO: **Important**: need mounting samples
func mountRootfs(rootfsPath string, rootfs []*types.Mount) error {
	// NOTICE: Only one rootfs is supported.
	if len(rootfs) != 1 {
		log.Warnf("Only support one rootfs in bundle.")
	}

	if err := utils.MountDirs(rootfs, rootfsPath); err != nil {
		return err
	}
	return nil
}

// createPodContainerInSandbox creates a container within an existing sandbox.
func createPodContainerInSandbox(ctx context.Context, sandbox cntr.SandboxTraits,
	ocispec specs.Spec, rootfs cntr.RootFs,
	containerID, bundlePath string, runtimeConfig *oci.RuntimeConfig, disableOutput bool) error {

	var defaultFirmware string
	if sandbox != nil {
		if fw, err := sandbox.Annotation(defs.FirmwarePath); err == nil {
			defaultFirmware = fw
		}
	}

	containerConfig, err := oci.ContainerConfig(containerID, bundlePath, ocispec, cntr.PodContainer, disableOutput, defaultFirmware, runtimeConfig)
	if err != nil {
		return fmt.Errorf("failed to create container config: %w", err)
	}

	containerConfig.Rootfs = rootfs

	// Validate firmware path before creating container in sandbox
	if err := validateFirmwareForContainer(containerConfig); err != nil {
		return fmt.Errorf("firmware validation failed for container %s: %w", containerID, err)
	}

	_, err = sandbox.CreateContainer(ctx, *containerConfig)
	if err != nil {
		return fmt.Errorf("failed to create container in sandbox: %w", err)
	}

	return nil
}

// createSandbox initializes and creates a new sandbox instance.
func createSandbox(ctx context.Context, ocispec *specs.Spec,
	runtimeConfig *oci.RuntimeConfig, rootfs cntr.RootFs,
	containerId, bundle string, disableOutput bool) (_ cntr.SandboxTraits, err error) {

	sandboxConfig, err := oci.SandboxConfig(ocispec, *runtimeConfig, bundle, containerId, disableOutput)
	if err != nil {
		return nil, err
	}

	if !rootfs.Mounted && len(sandboxConfig.ContainerConfigs) == 1 {
		if rootfs.Source != "" {
			realPath, err := utils.ResolvePath(rootfs.Source)
			if err != nil {
				return nil, err
			}
			rootfs.Source = realPath
		}
		sandboxConfig.ContainerConfigs[containerId].Rootfs = rootfs
	}

	if err := setupNetNS(sandboxConfig.ID, &sandboxConfig.NetworkConfig); err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			if ex := cleanupNetNS(sandboxConfig.ID, &sandboxConfig.NetworkConfig); ex != nil {
				log.Debugf("Failed to cleanup network namespace for sandbox %s: %v", sandboxConfig.ID, ex)
			}
		}
	}()

	if ocispec.Annotations == nil {
		ocispec.Annotations = make(map[string]string)
	}

	// NOTICE: nerdctl is considered as one of the first-class citizens of openEuler Embedded container engines
	// openEuler Embedded now supports containerd + nerdctl and docker-ce is not integrated in yocto, while user can install it via oebridge
	ocispec.Annotations["nerdctl/network-namespace"] = sandboxConfig.NetworkConfig.NetworkID
	sandboxConfig.Annotations["nerdctl/network-namespace"] = ocispec.Annotations["nerdctl/network-namespace"]
	sandbox, err := cntr.CreateSandbox(ctx, &sandboxConfig)
	if err != nil {
		return nil, err
	}

	log.Debugf("Sandbox <%s> created.", sandbox.SandboxID())
	containers := sandbox.GetAllContainers()
	for _, c := range containers {
		log.Debugf("Detect inside sandbox <%s>: container %s.", c.ID(), sandbox.SandboxID())
	}

	if len(containers) != 1 {
		return nil, fmt.Errorf("container list from sandbox is wrong, expecting only one container, got %d", len(containers))
	}
	return sandbox, nil
}
