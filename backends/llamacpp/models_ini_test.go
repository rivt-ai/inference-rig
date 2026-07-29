package llamacpp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inferencerig/core/profiles"
)

func demoProfile(name, source string) profiles.Profile {
	return profiles.Profile{
		Version: "1", Name: name, Backend: Name,
		Model:  profiles.ModelSpec{Source: source},
		Listen: profiles.ListenSpec{Host: "127.0.0.1", Port: 8080},
	}
}

func TestRenderModelsINIHeaderGlobalAndSections(t *testing.T) {
	content, err := renderModelsINI(
		map[string]string{"ctx-size": "4096"},
		[]section{{Name: "b", Values: map[string]string{"model": "/b.gguf"}}, {Name: "a", Values: map[string]string{"model": "/a.gguf"}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(content, "; InferenceRig materialized runtime file.") {
		t.Fatalf("missing materialized-file header: %q", content)
	}
	if !strings.Contains(content, "; Manual edits are allowed, but the next InferenceRig materialization overwrites them.") {
		t.Fatalf("missing manual-edit warning: %q", content)
	}
	if !strings.Contains(content, "version = 1\n") {
		t.Fatal("missing version key")
	}
	if !strings.Contains(content, "[*]\nctx-size = 4096\n") {
		t.Fatalf("missing [*] defaults section: %q", content)
	}
	// Named sections are ordered: [a] must appear before [b].
	if strings.Index(content, "[a]") > strings.Index(content, "[b]") {
		t.Fatalf("sections not sorted: %q", content)
	}
}

func TestRenderModelsINIDeterministic(t *testing.T) {
	defaults := map[string]string{"parallel": "2", "ctx-size": "8192"}
	secs := []section{{Name: "demo", Values: map[string]string{"model": "/m.gguf", "LLAMA_ARG_THREADS": "8", "ctx-size": "4096"}}}
	first, err := renderModelsINI(defaults, secs)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := renderModelsINI(defaults, secs)
		if err != nil || again != first {
			t.Fatalf("render not deterministic on pass %d", i)
		}
	}
	// LLAMA_ARG_THREADS canonicalizes to the CLI key `threads`.
	if !strings.Contains(first, "threads = 8") {
		t.Fatalf("environment key not canonicalized: %q", first)
	}
}

func TestRenderRejectsInjectionAndBadKeys(t *testing.T) {
	cases := []section{
		{Name: "demo", Values: map[string]string{"model": "/m.gguf\n[other]\nmodel = /evil.gguf"}},
		{Name: "demo", Values: map[string]string{"Model": "/m.gguf"}},
		{Name: "de[mo", Values: map[string]string{"model": "/m.gguf"}},
		{Name: "demo", Values: map[string]string{"model": "/a.gguf", "LLAMA_ARG_MODEL": "/b.gguf"}},
	}
	for i, s := range cases {
		if _, err := renderModelsINI(nil, []section{s}); !errors.Is(err, ErrInvalidINI) {
			t.Fatalf("case %d: err = %v, want ErrInvalidINI", i, err)
		}
	}
}

func TestParseRoundTrip(t *testing.T) {
	content, err := renderModelsINI(nil, []section{{Name: "demo", Values: map[string]string{"model": "/demo.gguf", "ctx-size": "4096"}}})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseINI([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	sec, ok := parsed["demo"]
	if !ok || sec.Values["model"] != "/demo.gguf" || sec.Values["ctx-size"] != "4096" {
		t.Fatalf("round trip = %#v", parsed)
	}
}

func TestParseStripsInlineCommentsAndRejectsBad(t *testing.T) {
	sec, err := parseINI([]byte("[demo]\nmodel = /demo.gguf ; local\n"))
	if err != nil || sec["demo"].Values["model"] != "/demo.gguf" {
		t.Fatalf("parse = %#v, err = %v", sec, err)
	}
	for _, bad := range []string{"[demo\nmodel = /x.gguf\n", "[demo]\nmodel /x.gguf\n", "[demo]\nModel = /x.gguf\n"} {
		if _, err := parseINI([]byte(bad)); !errors.Is(err, ErrInvalidINI) {
			t.Fatalf("parse(%q) err = %v", bad, err)
		}
	}
}

func TestGenerateAtomicAndInvalidNeverReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.ini")
	b := New(Options{GeneratedININPath: path})

	res, err := b.Generate([]profiles.Profile{demoProfile("good", "/good.gguf")})
	if err != nil {
		t.Fatal(err)
	}
	valid, err := os.ReadFile(path)
	if err != nil || res.Path != path {
		t.Fatalf("write result = %#v, err = %v", res, err)
	}

	// A profile whose engine_args inject a newline must fail render and leave the
	// last valid file byte-identical (no partial/atomic-replace corruption).
	bad := demoProfile("bad", "/bad.gguf")
	bad.EngineArgs = map[string]any{"ctx-size": "4096\n[evil]\nmodel = /evil.gguf"}
	if _, err := b.Generate([]profiles.Profile{bad}); err == nil {
		t.Fatal("Generate accepted an injection profile")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(valid) {
		t.Fatal("invalid profile set replaced the last valid models.ini")
	}
}

func TestMaterializeReturnsGeneratedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.ini")
	b := New(Options{GeneratedININPath: path})
	m, err := b.Materialize(demoProfile("demo", "/demo.gguf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 1 || m.Files[0].Path != path || m.Files[0].Mode != generatedFileMode {
		t.Fatalf("materialization = %#v", m)
	}
	if !strings.Contains(string(m.Files[0].Content), "[demo]") || !strings.Contains(string(m.Files[0].Content), "model = /demo.gguf") {
		t.Fatalf("content = %q", m.Files[0].Content)
	}
	// Materialize must not touch disk.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Materialize wrote to disk")
	}
}
