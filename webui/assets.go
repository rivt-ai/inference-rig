// Package webui embeds the browser client served by the public HTTP adapter.
package webui

import "embed"

// Files contains the capability-aware browser interface.
//
//go:embed dist/*
var Files embed.FS
