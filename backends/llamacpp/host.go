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
// shared telemetry collector; this backend policy supplies HasGPU/VRAMBytes.
// Ported from llamarig core/signals/gpu_subprocess.go (NVIDIA policy).
func (b *Backend) HostResources(ctx context.Context) (backends.HostResources, []string) {
	total, warnings := b.probeNVIDIAVRAM(ctx)
	host := backends.HostResources{}
	if total > 0 {
		host.HasGPU = true
		host.VRAMBytes = total
	}
	return host, warnings
}

// probeNVIDIAVRAM sums total VRAM across NVIDIA GPUs, in bytes.
func (b *Backend) probeNVIDIAVRAM(ctx context.Context) (int64, []string) {
	out, err := b.opts.gpu.Run(ctx, "nvidia-smi", "--query-gpu=memory.total", "--format=csv,noheader,nounits")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return 0, []string{"nvidia-smi not found; GPU telemetry unavailable"}
		}
		return 0, []string{fmt.Sprintf("nvidia-smi failed: %v", err)}
	}
	total, err := parseNVIDIAVRAM(out)
	if err != nil {
		return 0, []string{err.Error()}
	}
	return total, nil
}

// parseNVIDIAVRAM parses nvidia-smi memory.total (MiB per line) into total bytes.
func parseNVIDIAVRAM(out []byte) (int64, error) {
	var total int64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		mib, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse nvidia-smi VRAM %q: %w", line, err)
		}
		total += mib * 1024 * 1024
	}
	return total, nil
}
