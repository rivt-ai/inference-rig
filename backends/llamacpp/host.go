package llamacpp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"inferencerig/backends"
)

// commandRunner runs an external telemetry command; tests inject a stub.
type commandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// HostResources probes the host's discrete GPU/VRAM and returns the
// discrete-VRAM axis of a neutral HostResources. RAM is left to the shared
// telemetry collector; this backend policy supplies the discrete fields
// (HasGPU/VRAMBytes/VRAMUsedBytes/AcceleratorName).
// NVIDIA is ported from llamarig core/signals/gpu_subprocess.go. AMD is tried
// only when NVIDIA finds nothing, so an NVIDIA host never pays for (or warns
// about) a missing rocm-smi.
func (b *Backend) HostResources(ctx context.Context) (backends.HostResources, []string) {
	vram, warnings := b.probeNVIDIAVRAM(ctx)
	if vram.TotalBytes <= 0 {
		var amdWarnings []string
		vram, amdWarnings = b.probeAMDVRAM(ctx)
		warnings = append(warnings, amdWarnings...)
	}
	if vram.TotalBytes <= 0 {
		// No accelerator found: name the state explicitly rather than leaving
		// AcceleratorName empty, so a client can say "CPU only" instead of
		// showing nothing.
		return backends.HostResources{AcceleratorName: "CPU"}, warnings
	}
	return backends.HostResources{
		HasGPU:          true,
		VRAMBytes:       vram.TotalBytes,
		VRAMUsedBytes:   vram.UsedBytes,
		AcceleratorName: vram.Name,
	}, warnings
}

// gpuVRAM is the aggregate of one telemetry query, NVIDIA or AMD.
type gpuVRAM struct {
	TotalBytes  int64
	UsedBytes   int64
	Name        string
	DeviceCount int
}

// probeNVIDIAVRAM sums VRAM across NVIDIA GPUs, in bytes.
func (b *Backend) probeNVIDIAVRAM(ctx context.Context) (gpuVRAM, []string) {
	out, err := b.opts.gpu.Run(ctx, "nvidia-smi", "--query-gpu=memory.total,memory.used,name", "--format=csv,noheader,nounits")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return gpuVRAM{}, []string{"nvidia-smi not found; GPU telemetry unavailable"}
		}
		return gpuVRAM{}, []string{fmt.Sprintf("nvidia-smi failed: %v", err)}
	}
	vram, err := parseNVIDIAVRAM(out)
	if err != nil {
		return gpuVRAM{}, []string{err.Error()}
	}
	return vram, nil
}

// parseNVIDIAVRAM parses nvidia-smi "memory.total, memory.used, name" rows (MiB
// per GPU) into aggregate bytes. Multiple GPUs sum into one pooled figure named
// after the first adapter, since a fit decision cares about total capacity.
// ponytail: pooling is a simplification — report per-device rows if a host with
// mismatched cards ever needs them.
func parseNVIDIAVRAM(out []byte) (gpuVRAM, error) {
	vram := gpuVRAM{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, ",", 3)
		if len(fields) < 3 {
			return gpuVRAM{}, fmt.Errorf("parse nvidia-smi VRAM %q: want 3 fields", line)
		}
		total, err := parseMiB(fields[0])
		if err != nil {
			return gpuVRAM{}, err
		}
		used, err := parseMiB(fields[1])
		if err != nil {
			return gpuVRAM{}, err
		}
		vram.TotalBytes += total
		vram.UsedBytes += used
		vram.DeviceCount++
		if vram.Name == "" {
			vram.Name = strings.TrimSpace(fields[2])
		}
	}
	vram.Name = poolName(vram.Name, vram.DeviceCount)
	return vram, nil
}

func parseMiB(field string) (int64, error) {
	mib, err := strconv.ParseInt(strings.TrimSpace(field), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse nvidia-smi VRAM %q: %w", field, err)
	}
	return mib * 1024 * 1024, nil
}

// probeAMDVRAM sums VRAM across AMD GPUs using rocm-smi's JSON output, in
// bytes.
//
// ponytail: the exact rocm-smi flags and JSON key names below have not been
// verified against a real ROCm install in this session — only against public
// rocm-smi documentation. If a real machine's output does not parse, that is
// the first thing to check; the shape here ("card0"/"card1" top-level keys,
// "VRAM Total Memory (B)" / "VRAM Total Used Memory (B)" fields) is the
// documented default, not something this codebase has exercised.
func (b *Backend) probeAMDVRAM(ctx context.Context) (gpuVRAM, []string) {
	out, err := b.opts.gpu.Run(ctx, "rocm-smi", "--showmeminfo", "vram", "--showproductname", "--json")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return gpuVRAM{}, []string{"rocm-smi not found; GPU telemetry unavailable"}
		}
		return gpuVRAM{}, []string{fmt.Sprintf("rocm-smi failed: %v", err)}
	}
	vram, err := parseAMDVRAM(out)
	if err != nil {
		return gpuVRAM{}, []string{err.Error()}
	}
	return vram, nil
}

// parseAMDVRAM parses rocm-smi's --json output into aggregate bytes. Devices
// are reported as top-level "card0", "card1", ... keys; unrecognized keys are
// ignored so the driver's occasional non-device metadata entries do not error
// the whole probe.
func parseAMDVRAM(out []byte) (gpuVRAM, error) {
	var raw map[string]map[string]string
	if err := json.Unmarshal(out, &raw); err != nil {
		return gpuVRAM{}, fmt.Errorf("parse rocm-smi VRAM: %w", err)
	}
	vram := gpuVRAM{}
	for key, fields := range raw {
		if !strings.HasPrefix(key, "card") {
			continue
		}
		total, ok := fields["VRAM Total Memory (B)"]
		if !ok {
			continue
		}
		totalBytes, err := strconv.ParseInt(strings.TrimSpace(total), 10, 64)
		if err != nil {
			return gpuVRAM{}, fmt.Errorf("parse rocm-smi VRAM %q for %s: %w", total, key, err)
		}
		var usedBytes int64
		if used, ok := fields["VRAM Total Used Memory (B)"]; ok {
			usedBytes, _ = strconv.ParseInt(strings.TrimSpace(used), 10, 64)
		}
		vram.TotalBytes += totalBytes
		vram.UsedBytes += usedBytes
		vram.DeviceCount++
		if vram.Name == "" {
			if name, ok := fields["Card series"]; ok {
				vram.Name = strings.TrimSpace(name)
			} else {
				vram.Name = "AMD GPU"
			}
		}
	}
	vram.Name = poolName(vram.Name, vram.DeviceCount)
	return vram, nil
}

// poolName prefixes a device name with its count when more than one device
// pooled into the figure, so a summed multi-GPU verdict discloses the split
// rather than reading as a single device.
func poolName(name string, count int) string {
	if count > 1 {
		return fmt.Sprintf("%d× %s", count, name)
	}
	return name
}
