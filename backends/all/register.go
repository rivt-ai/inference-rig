// Package all registers every backend shipped with InferenceRig.
package all

import (
	"inferencerig/backends"
	"inferencerig/backends/llamacpp"
	"inferencerig/backends/mlx"
)

// Options supplies neutral settings shared by every built-in backend.
type Options struct {
	ModelStorageDir string
}

// Register adds every built-in backend to registry.
func Register(registry *backends.Registry, options Options) error {
	if err := registry.Register(llamacpp.New(llamacpp.Options{ModelStorageDir: options.ModelStorageDir})); err != nil {
		return err
	}
	return registry.Register(mlx.New(mlx.Options{ModelStorageDir: options.ModelStorageDir}))
}
