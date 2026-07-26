// Package modelcatalog is the neutral model-catalog mechanism shared by every
// backend: local artifact scanning, path containment, and memory-fit math. It
// owns no engine format knowledge — which files constitute a model, and whether
// a model is a single file or a multi-file snapshot, is decided by a backend
// FormatPolicy. Concrete backends supply the policy;
// the shared control plane depends only on this package.
package modelcatalog

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
