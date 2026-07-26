// Package all registers every backend shipped with InferenceRig.
package all

import (
	"inferencerig/backends"
	"inferencerig/backends/llamacpp"
	"inferencerig/backends/mlx"
)

// Register adds every built-in backend to registry.
func Register(registry *backends.Registry) error {
	if err := llamacpp.Register(registry); err != nil {
		return err
	}
	return mlx.Register(registry)
}
