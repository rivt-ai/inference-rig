package llamacpp

import (
	"path"
	"regexp"
	"strings"
)

// quantPattern extracts a GGUF quantization tag (e.g. Q4_K_M, F16) from a
// filename. Ported from llamarig core/modelcatalog/quant.go — GGUF format
// policy that belongs in the backend.
var quantPattern = regexp.MustCompile(`(?i)(?:^|[-_/])((?:UD-)?(?:Q[0-9]_K(?:_[A-Z]+)?|IQ[0-9]_[A-Z0-9]+|Q[0-9]_[0-9]|Q[0-9]|BF16|F16|F32))(?:\.gguf|[-_/])`)

// InferQuant returns the uppercase quantization tag encoded in a GGUF filename,
// or "" when none is present.
func InferQuant(filename string) string {
	name := path.Base(filename)
	matches := quantPattern.FindStringSubmatch(name)
	if len(matches) < 2 {
		return ""
	}
	return strings.ToUpper(matches[1])
}
