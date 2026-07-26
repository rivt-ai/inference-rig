package llamacpp

import (
	"context"
	"os/exec"
)

// detect picks the host's llama.cpp accelerator, preferring CUDA, then ROCm,
// then Vulkan, falling back to CPU (Metal on macOS). Ported from llamarig
// core/llamainstall/backend.go.
func (i *installer) detect(ctx context.Context) Accel {
	if i.goos == "darwin" {
		return AccelMetal
	}
	switch {
	case commandWorks(ctx, "nvidia-smi", "-L"):
		return AccelCUDA
	case commandWorks(ctx, "rocminfo"):
		return AccelROCm
	case commandWorks(ctx, "vulkaninfo", "--summary"):
		return AccelVulkan
	default:
		return AccelCPU
	}
}

// commandWorks reports whether command is on PATH and exits successfully.
func commandWorks(ctx context.Context, command string, args ...string) bool {
	if _, err := exec.LookPath(command); err != nil {
		return false
	}
	return exec.CommandContext(ctx, command, args...).Run() == nil
}
