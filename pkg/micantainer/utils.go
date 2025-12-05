package micantainer

import (
	"context"
	"errors"
	"fmt"
	er "micrun/errors"
	log "micrun/logger"
	"micrun/pkg/libmica"
	"micrun/pkg/pedestal"
	"micrun/pkg/utils"
	"os/exec"
	"strings"
	"time"
)

func startClient(ctx context.Context, sandbox SandboxTraits, c *Container) error {
	if _, err := c.ensureClientPresence(); err != nil {
		return err
	}

	start := time.Now()
	if err := libmica.Start(c.id); err != nil {
		log.Errorf("startClient: Start failed: %v", err)
		return err
	}

	if err := c.setupMemory(); err != nil {
		return err
	}
	log.Infof("startClient: Start OK in %s", time.Since(start))

	return nil
}

// 1. search in bundle/.../<clientOSname>.elf
// 2. if missing, log and search for binary in bundle recursively
// TODO: Only copy values, the evaluation procedure is in the caller function
func createMicaClientConf(container *Container) (libmica.MicaClientConf, error) {
	config := container.config
	pedType := HostPedType
	cpus := container.GetClientCPUs()
	conf := libmica.MicaClientConf{}
	cpuCap := int(config.cpuCapacity())
	// Pre-calculate effective values for clarity.
	// Use VCPUNum prepared in ContainerConfig; it already reflects cpuset policy
	// or defaults to 1 when not specified.
	vcpus := int(config.VCPUNum)
	if vcpus <= 0 {
		vcpus = 1
	}
	// memoryMB (initial) should prefer the configured limit, falling back to the minimum (reservation) when unset.
	memMB := int(config.containerMaxMemMB())
	if err := ensureFirmwarePath(config.ImageAbsPath); err != nil {
		return libmica.MicaClientConf{}, fmt.Errorf("firmware validation failed: %w", err)
	}

	// Memory limit is already expressed in MiB
	conf.InitWithOpts(libmica.MicaClientConfCreateOptions{
		CPU:             cpus,
		CPUCapacity:     cpuCap,
		CPUWeight:       int(pedestal.ShareToWeight(config.cpuShares())),
		VCPUs:           vcpus,
		MaxVCPUs:        int(config.MaxVcpuNum),
		MemoryMB:        memMB,
		MemoryThreshold: int(config.MemoryThresholdMB),
		Name:            container.id,
		Path:            config.ImageAbsPath,
		Ped:             pedType.String(),
		PedCfg:          config.PedestalConf,
	})
	return conf, nil
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

// formatResourceForLog formats EssentialResource for readable logging
func formatResourceForLog(res *pedestal.EssentialResource) string {
	if res == nil {
		return "<nil>"
	}

	var parts []string

	if res.CpuCpacity != nil {
		parts = append(parts, fmt.Sprintf("CpuCapacity=%d", *res.CpuCpacity))
	}

	if res.CPUWeight != nil {
		parts = append(parts, fmt.Sprintf("CPUWeight=%d", *res.CPUWeight))
	}

	if res.ClientCpuSet != "" {
		parts = append(parts, fmt.Sprintf("ClientCpuSet=%s", res.ClientCpuSet))
	}

	if res.Vcpu != nil {
		parts = append(parts, fmt.Sprintf("Vcpu=%d", *res.Vcpu))
	}

	if res.MemoryMaxMB != nil {
		parts = append(parts, fmt.Sprintf("MemoryLimitMB=%d", *res.MemoryMaxMB))
	}

	if len(parts) == 0 {
		return "<empty>"
	}

	return "{" + strings.Join(parts, ", ") + "}"
}

func ensureFirmwarePath(firmwarePath string) error {

	absPath, err := utils.EnsureRegularFilePath(firmwarePath)
	if err != nil {
		return err
	}

	log.Debugf("firmware path validated: %s", absPath)
	return nil
}

func copyUint32(v uint32) *uint32 {
	val := v
	return &val
}

// loadSandbox restores a sandbox from disk by its ID.
func loadSandbox(ctx context.Context, id string) (sandbox *Sandbox, err error) {
	if id == "" {
		return nil, er.EmptySandboxID
	}

	ss, err := restoreSandbox(ctx, id)
	if err != nil {
		log.Debugf("Failed to restore sandbox from disk: %v.", err)
		return nil, err
	}
	c := ss.Config

	sandbox, err = createSandbox(ctx, &c)
	if err != nil {
		log.Errorf("Failed to create sandbox: %v.", err)
		return nil, err
	}

	if err := sandbox.loadContainersToSandbox(ctx); err != nil {
		return nil, err
	}
	return sandbox, nil
}

func extractENo(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 255
}
