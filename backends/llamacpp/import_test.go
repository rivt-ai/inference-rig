package llamacpp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inferencerig/core/profiles"
)

const devYAML = `version: 1
name: dev
backend: llamacpp
model:
  source: /models/dev.gguf
listen:
  host: 127.0.0.1
  port: 8080
engine_args:
  ctx-size: 4096
`

// importFixture materializes devYAML to disk the way a start does — file plus
// baseline — then applies edit to the file, and returns the import result.
func importFixture(t *testing.T, edit func(string) string, docs ...profiles.ProfileDocument) (map[string]string, []string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models.ini")
	b := New(Options{GeneratedININPath: path})
	if len(docs) == 0 {
		docs = []profiles.ProfileDocument{devDoc(t, devYAML)}
	}
	effective := make([]profiles.Profile, 0, len(docs))
	for _, doc := range docs {
		effective = append(effective, doc.Effective)
	}
	m, err := b.MaterializeProfiles(effective)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range m.Files {
		if err := os.WriteFile(file.Path, file.Content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if edit != nil {
		current, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(edit(string(current))), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	adopt, conflicts, err := b.ImportGenerated(docs)
	if err != nil {
		t.Fatalf("ImportGenerated: %v", err)
	}
	return adopt, conflicts
}

func devDoc(t *testing.T, profileYAML string) profiles.ProfileDocument {
	t.Helper()
	store := profiles.NewFileStore(t.TempDir(), 0, func(string) (profiles.BackendValidator, error) {
		return New(Options{}), nil
	})
	doc, err := store.Validate(t.Context(), profiles.CreateRequest{Name: "dev", ProfileYAML: profileYAML})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestImportGeneratedAdoptsEditedValue(t *testing.T) {
	adopt, conflicts := importFixture(t, func(s string) string {
		return strings.Replace(s, "ctx-size = 4096", "ctx-size = 16384", 1)
	})
	if len(adopt) != 1 || !strings.Contains(adopt["dev"], "ctx-size: 16384") {
		t.Fatalf("adopt = %#v", adopt)
	}
	// The rest of the profile rides along unchanged: an import must not cost
	// the user fields models.ini knows nothing about.
	for _, want := range []string{"name: dev", "backend: llamacpp", "port: 8080"} {
		if !strings.Contains(adopt["dev"], want) {
			t.Errorf("imported profile lost %q:\n%s", want, adopt["dev"])
		}
	}
	if len(conflicts) != 0 {
		t.Fatalf("conflicts = %#v", conflicts)
	}
}

func TestImportGeneratedAdoptsAddedAndRemovedKeys(t *testing.T) {
	adopt, _ := importFixture(t, func(s string) string {
		s = strings.Replace(s, "ctx-size = 4096\n", "", 1)
		return s + "parallel = 4\n"
	})
	// parallel: 4, not parallel: "4" — an imported count stays a number.
	if strings.Contains(adopt["dev"], "ctx-size") || !strings.Contains(adopt["dev"], "parallel: 4\n") {
		t.Fatalf("profile YAML = %q", adopt["dev"])
	}
}

func TestImportGeneratedAdoptsModelSource(t *testing.T) {
	adopt, _ := importFixture(t, func(s string) string {
		return strings.Replace(s, "/models/dev.gguf", "/models/other.gguf", 1)
	})
	if !strings.Contains(adopt["dev"], "source: /models/other.gguf") {
		t.Fatalf("adopt = %#v", adopt)
	}
}

// The whole reason a baseline is kept: a key the user edited *and* the profile
// moved cannot be merged, so the profile wins and the user is told.
func TestImportGeneratedReportsConflictAndKeepsProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.ini")
	b := New(Options{GeneratedININPath: path})
	base := devDoc(t, devYAML)
	m, err := b.MaterializeProfiles([]profiles.Profile{base.Effective})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range m.Files {
		if err := os.WriteFile(file.Path, file.Content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Hand-edit the file...
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(current), "ctx-size = 4096", "ctx-size = 16384", 1)
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	// ...and move the profile too, to a third value.
	moved := devDoc(t, strings.Replace(devYAML, "ctx-size: 4096", "ctx-size: 2048", 1))

	adopt, conflicts, err := b.ImportGenerated([]profiles.ProfileDocument{moved})
	if err != nil {
		t.Fatal(err)
	}
	if len(adopt) != 0 {
		t.Fatalf("conflicting edit was imported: %#v", adopt)
	}
	if len(conflicts) != 1 || conflicts[0] != "dev.ctx-size" {
		t.Fatalf("conflicts = %#v", conflicts)
	}
}

func TestImportGeneratedIgnoresUnattributableEdits(t *testing.T) {
	// [*] belongs to no profile, and [ghost] to no profile that exists.
	adopt, conflicts := importFixture(t, func(s string) string {
		return s + "\n[*]\nctx-size = 99\n\n[ghost]\nmodel = /ghost.gguf\n"
	})
	if len(adopt) != 0 || len(conflicts) != 0 {
		t.Fatalf("adopt = %#v, conflicts = %#v", adopt, conflicts)
	}
}

func TestImportGeneratedNoEditIsNoOp(t *testing.T) {
	adopt, conflicts := importFixture(t, nil)
	if len(adopt) != 0 || len(conflicts) != 0 {
		t.Fatalf("untouched file produced adopt = %#v, conflicts = %#v", adopt, conflicts)
	}
}

// Without a baseline there is no way to tell an edit from a profile change, so
// nothing is attributed to the user. This covers the first materialization and
// any install predating the baseline.
func TestImportGeneratedWithoutBaselineImportsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.ini")
	b := New(Options{GeneratedININPath: path})
	doc := devDoc(t, devYAML)
	if err := os.WriteFile(path, []byte("\n[dev]\nctx-size = 16384\nmodel = /models/dev.gguf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	adopt, conflicts, err := b.ImportGenerated([]profiles.ProfileDocument{doc})
	if err != nil {
		t.Fatal(err)
	}
	if len(adopt) != 0 || len(conflicts) != 0 {
		t.Fatalf("adopt = %#v, conflicts = %#v", adopt, conflicts)
	}
}

func TestImportGeneratedWithoutFileImportsNothing(t *testing.T) {
	b := New(Options{GeneratedININPath: filepath.Join(t.TempDir(), "models.ini")})
	adopt, conflicts, err := b.ImportGenerated([]profiles.ProfileDocument{devDoc(t, devYAML)})
	if err != nil {
		t.Fatal(err)
	}
	if len(adopt) != 0 || len(conflicts) != 0 {
		t.Fatalf("adopt = %#v, conflicts = %#v", adopt, conflicts)
	}
}

// A stale baseline — the crash-between-writes case — must not read as a set of
// edits reverting the file to what it used to say.
func TestImportGeneratedToleratesStaleBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.ini")
	b := New(Options{GeneratedININPath: path})
	doc := devDoc(t, devYAML)
	m, err := b.MaterializeProfiles([]profiles.Profile{doc.Effective})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, m.Files[0].Content, 0o600); err != nil {
		t.Fatal(err)
	}
	// Baseline from an older profile, as if the file write landed and the
	// baseline write did not.
	old := devDoc(t, strings.Replace(devYAML, "ctx-size: 4096", "ctx-size: 512", 1))
	stale, err := b.MaterializeProfiles([]profiles.Profile{old.Effective})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+baselineSuffix, stale.Files[0].Content, 0o600); err != nil {
		t.Fatal(err)
	}
	adopt, conflicts, err := b.ImportGenerated([]profiles.ProfileDocument{doc})
	if err != nil {
		t.Fatal(err)
	}
	if len(adopt) != 0 || len(conflicts) != 0 {
		t.Fatalf("stale baseline produced adopt = %#v, conflicts = %#v", adopt, conflicts)
	}
}

func TestImportGeneratedRejectsUngrammaticalFile(t *testing.T) {
	_, _, err := func() (map[string]string, []string, error) {
		path := filepath.Join(t.TempDir(), "models.ini")
		b := New(Options{GeneratedININPath: path})
		doc := devDoc(t, devYAML)
		m, err := b.MaterializeProfiles([]profiles.Profile{doc.Effective})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+baselineSuffix, m.Files[0].Content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("[dev]\nctx-size 4096\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return b.ImportGenerated([]profiles.ProfileDocument{doc})
	}()
	if err == nil {
		t.Fatal("ungrammatical file did not fail the import")
	}
}

// The import writes the profile back, so it must write back the file the user
// wrote rather than a re-rendering of the fields models.ini happens to know.
func TestImportGeneratedPreservesProfileComments(t *testing.T) {
	const annotated = `version: 1
name: dev
backend: llamacpp
model:
  source: /models/dev.gguf # the quantized build
listen:
  host: 127.0.0.1
  port: 8080
# tuned against the long-context runs
engine_args:
  ctx-size: 4096 # raise this together with parallel
`
	adopt, conflicts := importFixture(t, func(s string) string {
		return strings.Replace(s, "ctx-size = 4096", "ctx-size = 16384", 1)
	}, devDoc(t, annotated))
	if len(conflicts) != 0 {
		t.Fatalf("conflicts = %#v", conflicts)
	}
	for _, want := range []string{
		"# the quantized build",
		"# tuned against the long-context runs",
		"# raise this together with parallel",
		"ctx-size: 16384",
	} {
		if !strings.Contains(adopt["dev"], want) {
			t.Errorf("imported profile lost %q:\n%s", want, adopt["dev"])
		}
	}
}
