package mlx

import (
	"context"
	"runtime"

	"inferencerig/backends"
)

// HostResources reports the unified-memory axis on Apple silicon, where the GPU
// draws from system RAM instead of dedicated VRAM. The shared telemetry
// collector fills the byte fields from its own memory stats, so this probe only
// has to establish that a unified device is present.
//
// ponytail: darwin/arm64 is the whole probe. MLX only runs on Apple silicon, so
// querying the system profiler would refine the device name and nothing else.
func (b *Backend) HostResources(context.Context) (backends.HostResources, []string) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return backends.HostResources{}, nil
	}
	return backends.HostResources{UnifiedMemory: true, AcceleratorName: "Apple Metal"}, nil
}
