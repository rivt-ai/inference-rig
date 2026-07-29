// Package modelcatalog is the neutral model-catalog mechanism shared by every
// backend: local artifact scanning, path containment, and memory-fit math. It
// owns no engine format knowledge — which files constitute a model, and whether
// a model is a single file or a multi-file snapshot, is decided by a backend
// FormatPolicy. Concrete backends supply the policy;
// the shared control plane depends only on this package.
package modelcatalog

import "context"

// FormatPolicy is the backend-specific catalog policy the shared mechanism
// calls into. It keeps engine format specifics (file extensions, single- vs
// multi-file layout) out of the neutral core.
type FormatPolicy interface {
	// IsModelFile reports whether filename is one of this format's model files
	// (for example, by extension). Directory entries that are not model files
	// are skipped by the scanner.
	IsModelFile(filename string) bool
	// MultiFile reports whether a resolved model of this format is a directory
	// snapshot of many files rather than a single self-contained file.
	MultiFile() bool
}

// CatalogPolicy owns the backend-specific interpretation of remote repository
// files and local artifact layout.
type CatalogPolicy interface {
	// SearchFilter is the remote catalog's tag for this format ("gguf", "mlx").
	// Without it a search returns the most-downloaded models of any format and
	// Variants discards nearly all of them, leaving an empty catalog that looks
	// like a network failure. An empty string searches unfiltered.
	SearchFilter() string
	Variants(Source, []RemoteFile) ([]Variant, error)
	ListLocal(context.Context) ([]LocalModel, error)
	DeleteLocal(string) error
}
