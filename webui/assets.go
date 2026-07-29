// Package webui embeds the browser client served by the public HTTP adapter.
package webui

import "embed"

// Files contains the capability-aware browser interface. The dist tree is a
// build artifact and is not committed, so only dist/.gitkeep is present in a
// fresh checkout; all: is required to embed it, since the default embed
// pattern skips dot-prefixed files and would leave the directory empty.
//
//go:embed all:dist
var Files embed.FS
