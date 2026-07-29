package signals

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	runtimepkg "inferencerig/core/runtime"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

type GopsutilCollector struct {
	Runtime RuntimeStatusProvider
	Clock   Clock
	Disks   []DiskTarget
	// Accelerators probes GPU-class devices. Optional: nil reports none. It is a
	// plain func so the composition root can source it from backend policy
	// without core/signals depending on the backend registry.
	Accelerators func(context.Context) ([]AcceleratorStats, []string)
}

func NewGopsutilCollector(runtime RuntimeStatusProvider, disks []DiskTarget) *GopsutilCollector {
	return &GopsutilCollector{Runtime: runtime, Clock: systemClock{}, Disks: disks}
}

func (c *GopsutilCollector) Snapshot(ctx context.Context) (Snapshot, error) {
	clock := c.Clock
	if clock == nil {
		clock = systemClock{}
	}
	snapshot := Snapshot{CapturedAt: clock.Now().Format(time.RFC3339)}
	warnings := []string{}
	machine, err := c.Machine(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Memory = machine.Memory
	warnings = append(warnings, machine.Warnings...)

	if info, err := host.InfoWithContext(ctx); err == nil {
		snapshot.Host = HostStats{Hostname: info.Hostname, OS: info.OS, Platform: info.Platform}
	} else {
		warnings = append(warnings, fmt.Sprintf("host stats unavailable: %v", err))
		snapshot.Host = HostStats{OS: runtime.GOOS}
	}

	disks, diskWarnings := c.diskStats(ctx)
	snapshot.Disks = disks
	warnings = append(warnings, diskWarnings...)

	if counts, err := cpu.CountsWithContext(ctx, true); err == nil {
		snapshot.CPU.LogicalCores = counts
	} else {
		warnings = append(warnings, fmt.Sprintf("CPU core count unavailable: %v", err))
	}
	if percents, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(percents) > 0 {
		snapshot.CPU.UsedPercent = percents[0]
	} else if err != nil {
		warnings = append(warnings, fmt.Sprintf("CPU usage unavailable: %v", err))
	}

	accelerators, acceleratorWarnings := c.acceleratorStats(ctx, snapshot.Memory)
	snapshot.Accelerators = accelerators
	warnings = append(warnings, acceleratorWarnings...)

	processes, processWarnings := c.runtimeProcesses(ctx)
	snapshot.Runtime = processes
	warnings = append(warnings, processWarnings...)
	snapshot.Warnings = warnings
	return snapshot, nil
}

// acceleratorStats runs the injected probe and resolves unified-memory devices
// against system RAM, which is the memory such a device actually draws from.
func (c *GopsutilCollector) acceleratorStats(ctx context.Context, memory MemoryStats) ([]AcceleratorStats, []string) {
	if c.Accelerators == nil {
		return nil, nil
	}
	stats, warnings := c.Accelerators(ctx)
	for i := range stats {
		if stats[i].UnifiedMemory {
			stats[i].TotalBytes = memory.TotalBytes
			stats[i].UsedBytes = memory.UsedBytes
		}
	}
	return stats, warnings
}

func (c *GopsutilCollector) Machine(ctx context.Context) (MachineSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return MachineSnapshot{}, err
	}
	machine := MachineSnapshot{}
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		machine.Memory = MemoryStats{TotalBytes: vm.Total, AvailableBytes: vm.Available, UsedBytes: vm.Used, UsedPercent: vm.UsedPercent}
	} else {
		machine.Warnings = append(machine.Warnings, fmt.Sprintf("memory stats unavailable: %v", err))
	}
	return machine, nil
}

func (c *GopsutilCollector) diskStats(ctx context.Context) ([]DiskStats, []string) {
	targets := c.Disks
	if len(targets) == 0 {
		targets = []DiskTarget{{Label: "root", Path: "/"}}
	}
	stats := make([]DiskStats, 0, len(targets))
	warnings := []string{}
	for _, target := range targets {
		if target.Path == "" {
			continue
		}
		usage, err := disk.UsageWithContext(ctx, target.Path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s disk stats unavailable: %v", target.Label, err))
			continue
		}
		stats = append(stats, DiskStats{Label: target.Label, Path: usage.Path, TotalBytes: usage.Total, FreeBytes: usage.Free, UsedBytes: usage.Used, UsedPercent: usage.UsedPercent})
	}
	return stats, warnings
}

func (c *GopsutilCollector) runtimeProcesses(ctx context.Context) ([]RuntimeProcessStats, []string) {
	// The provider is optional; a collector without one simply reports no
	// per-process rows rather than warning on every snapshot.
	if c.Runtime == nil {
		return nil, nil
	}
	status, err := c.Runtime.Status(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("runtime process status unavailable: %v", err)}
	}
	stats := make([]RuntimeProcessStats, 0, len(status.Processes))
	warnings := []string{}
	for _, procStatus := range status.Processes {
		if procStatus.PID <= 0 || procStatus.State == runtimepkg.Stopped {
			continue
		}
		stat, err := processStats(ctx, procStatus)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		stats = append(stats, stat)
	}
	return stats, warnings
}

func processStats(ctx context.Context, procStatus runtimepkg.ProcessStatus) (RuntimeProcessStats, error) {
	proc, err := process.NewProcessWithContext(ctx, int32(procStatus.PID))
	if err != nil {
		return RuntimeProcessStats{}, fmt.Errorf("process %d stats unavailable: %w", procStatus.PID, err)
	}
	memInfo, err := proc.MemoryInfoWithContext(ctx)
	if err != nil {
		return RuntimeProcessStats{}, fmt.Errorf("process %d memory stats unavailable: %w", procStatus.PID, err)
	}
	cpuPercent, err := proc.CPUPercentWithContext(ctx)
	if err != nil {
		return RuntimeProcessStats{}, fmt.Errorf("process %d CPU stats unavailable: %w", procStatus.PID, err)
	}
	cmdline, _ := proc.CmdlineWithContext(ctx)
	return RuntimeProcessStats{Name: procStatus.Name, PID: procStatus.PID, RSSBytes: memInfo.RSS, CPUPercent: cpuPercent, Command: strings.TrimSpace(cmdline)}, nil
}
