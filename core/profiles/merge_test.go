package profiles

import (
	"strings"
	"testing"
)

const annotated = `# Development rig.
version: 1
name: dev
backend: llamacpp
model:
  source: /models/dev.gguf # the quantized build, not the f16 one
  reference: ""
listen:
  host: 127.0.0.1
  port: 8080
# Tuned against the 32k-context runs.
engine_args:
  ctx-size: 4096 # raise this together with parallel
  flash-attn: true
`

func parseAnnotated(t *testing.T) Profile {
	t.Helper()
	store := NewFileStore(t.TempDir(), 0, func(string) (BackendValidator, error) { return passthrough{}, nil })
	doc, err := store.Validate(t.Context(), CreateRequest{Name: "dev", ProfileYAML: annotated})
	if err != nil {
		t.Fatal(err)
	}
	return doc.Parsed
}

type passthrough struct{}

func (passthrough) ValidateProfile(p Profile) (Profile, error) { return p, nil }

// The whole point of merging rather than re-marshalling: a change to one engine
// argument must cost the user that one line and nothing else.
func TestMergeYAMLKeepsCommentsAndLayout(t *testing.T) {
	updated := parseAnnotated(t)
	updated.EngineArgs["ctx-size"] = 16384

	merged, err := MergeYAML(annotated, updated)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Development rig.",
		"# the quantized build, not the f16 one",
		"# Tuned against the 32k-context runs.",
		"# raise this together with parallel",
		"ctx-size: 16384",
		"  host: 127.0.0.1",
	} {
		if !strings.Contains(merged, want) {
			t.Errorf("merged profile lost %q:\n%s", want, merged)
		}
	}
	if strings.Contains(merged, "ctx-size: 4096") {
		t.Errorf("edit not applied:\n%s", merged)
	}
	// Key order is the file's, not the struct's.
	if !strings.HasPrefix(merged, "# Development rig.\nversion: 1\nname: dev\n") {
		t.Errorf("key order changed:\n%s", merged)
	}
}

// An untouched profile must come back byte-identical, so a no-op import cannot
// show up as a diff in the operator's version control.
func TestMergeYAMLWithoutChangesIsIdentity(t *testing.T) {
	merged, err := MergeYAML(annotated, parseAnnotated(t))
	if err != nil {
		t.Fatal(err)
	}
	if merged != annotated {
		t.Errorf("merge rewrote an unchanged profile:\n%s", merged)
	}
}

func TestMergeYAMLAddsAndRemovesEngineArgs(t *testing.T) {
	updated := parseAnnotated(t)
	delete(updated.EngineArgs, "flash-attn")
	updated.EngineArgs["parallel"] = 4

	merged, err := MergeYAML(annotated, updated)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(merged, "flash-attn") {
		t.Errorf("removed engine arg survived:\n%s", merged)
	}
	// A number stays a number, and the new key lands inside engine_args.
	if !strings.Contains(merged, "\n  parallel: 4\n") {
		t.Errorf("added engine arg missing or misplaced:\n%s", merged)
	}
	if !strings.Contains(merged, "# Tuned against the 32k-context runs.") {
		t.Errorf("merged profile lost a comment:\n%s", merged)
	}
}

// yaml.v3 indents by four spaces unless told otherwise, which would reformat
// every nested block of a two-space file that an edit only meant to touch once.
func TestMergeYAMLKeepsDocumentIndent(t *testing.T) {
	const wide = "version: 1\nname: dev\nbackend: llamacpp\nmodel:\n    source: /models/dev.gguf\n" +
		"    reference: \"\"\nlisten:\n    host: 127.0.0.1\n    port: 8080\nengine_args: {}\n"
	store := NewFileStore(t.TempDir(), 0, func(string) (BackendValidator, error) { return passthrough{}, nil })
	doc, err := store.Validate(t.Context(), CreateRequest{Name: "dev", ProfileYAML: wide})
	if err != nil {
		t.Fatal(err)
	}
	updated := doc.Parsed
	updated.Model.Source = "/models/other.gguf"

	merged, err := MergeYAML(wide, updated)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(merged, "\n    source: /models/other.gguf\n") {
		t.Errorf("four-space document was reindented:\n%s", merged)
	}
}

func TestMergeYAMLRejectsUnparseableProfile(t *testing.T) {
	if _, err := MergeYAML("\tnot: yaml\n", Profile{}); err == nil {
		t.Fatal("unparseable profile did not fail the merge")
	}
}
