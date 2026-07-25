// Package buildinfo carries version metadata stamped into the binary at build
// time via -ldflags. Defaults apply for `go run` and plain `go build`.
package buildinfo

var (
	// Version is the release version, e.g. "v0.1.0-alpha.1".
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = "none"
	// CommitTime is the commit timestamp in RFC 3339.
	CommitTime = "unknown"
)
