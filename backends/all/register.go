// Package all registers every backend shipped with InferenceRig.
package all

import (
	"runtime"

	"inferencerig/backends"
	"inferencerig/backends/llamacpp"
	"inferencerig/backends/mlx"
)

// Options supplies neutral settings shared by every built-in backend.
type Options struct {
	ModelStorageDir string
	// ExposeModelsWithoutProfile relaxes a backend runtime to also serve models
	// present in model storage that no profile declares.
	ExposeModelsWithoutProfile bool
}

// Register adds every built-in backend that can run on this host. MLX is
// registered only on Apple silicon: elsewhere its installer and its runtime
// both fail by construction, so offering it only produces a backend the user
// can select but never start.
func Register(registry *backends.Registry, options Options) error {
	if err := registry.Register(llamacpp.New(llamacpp.Options{
		ModelStorageDir:            options.ModelStorageDir,
		ExposeModelsWithoutProfile: options.ExposeModelsWithoutProfile,
	})); err != nil {
		return err
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return nil
	}
	return registry.Register(mlx.New(mlx.Options{ModelStorageDir: options.ModelStorageDir}))
}
