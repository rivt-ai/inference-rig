package llamacpp

import (
	"context"
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

// HostResources probes the host's discrete GPU/VRAM (via nvidia-smi) and
// returns the discrete-VRAM axis of a neutral HostResources. RAM is left to the
// shared telemetry collector; this backend policy supplies the discrete fields
// (HasGPU/VRAMBytes/VRAMUsedBytes/AcceleratorName).
// Ported from llamarig core/signals/gpu_subprocess.go (NVIDIA policy).
func (b *Backend) HostResources(ctx context.Context) (backends.HostResources, []string) {
	vram, warnings := b.probeNVIDIAVRAM(ctx)
	if vram.TotalBytes <= 0 {
		return backends.HostResources{}, warnings
	}
	return backends.HostResources{
		HasGPU:          true,
		VRAMBytes:       vram.TotalBytes,
		VRAMUsedBytes:   vram.UsedBytes,
		AcceleratorName: vram.Name,
	}, warnings
}

// nvidiaVRAM is the aggregate of one nvidia-smi telemetry query.
type nvidiaVRAM struct {
	TotalBytes int64
	UsedBytes  int64
	Name       string
}

// probeNVIDIAVRAM sums VRAM across NVIDIA GPUs, in bytes.
func (b *Backend) probeNVIDIAVRAM(ctx context.Context) (nvidiaVRAM, []string) {
	out, err := b.opts.gpu.Run(ctx, "nvidia-smi", "--query-gpu=memory.total,memory.used,name", "--format=csv,noheader,nounits")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nvidiaVRAM{}, []string{"nvidia-smi not found; GPU telemetry unavailable"}
		}
		return nvidiaVRAM{}, []string{fmt.Sprintf("nvidia-smi failed: %v", err)}
	}
	vram, err := parseNVIDIAVRAM(out)
	if err != nil {
		return nvidiaVRAM{}, []string{err.Error()}
	}
	return vram, nil
}

// parseNVIDIAVRAM parses nvidia-smi "memory.total, memory.used, name" rows (MiB
// per GPU) into aggregate bytes. Multiple GPUs sum into one pooled figure named
// after the first adapter, since a fit decision cares about total capacity.
// ponytail: pooling is a simplification — report per-device rows if a host with
// mismatched cards ever needs them.
func parseNVIDIAVRAM(out []byte) (nvidiaVRAM, error) {
	vram := nvidiaVRAM{}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, ",", 3)
		if len(fields) < 3 {
			return nvidiaVRAM{}, fmt.Errorf("parse nvidia-smi VRAM %q: want 3 fields", line)
		}
		total, err := parseMiB(fields[0])
		if err != nil {
			return nvidiaVRAM{}, err
		}
		used, err := parseMiB(fields[1])
		if err != nil {
			return nvidiaVRAM{}, err
		}
		vram.TotalBytes += total
		vram.UsedBytes += used
		count++
		if vram.Name == "" {
			vram.Name = strings.TrimSpace(fields[2])
		}
	}
	if count > 1 {
		vram.Name = fmt.Sprintf("%d× %s", count, vram.Name)
	}
	return vram, nil
}

func parseMiB(field string) (int64, error) {
	mib, err := strconv.ParseInt(strings.TrimSpace(field), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse nvidia-smi VRAM %q: %w", field, err)
	}
	return mib * 1024 * 1024, nil
}
