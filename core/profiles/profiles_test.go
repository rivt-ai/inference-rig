package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// fakeBackend is a stand-in BackendValidator that proves the store drives the
// full CRUD lifecycle without importing any real engine package. It interprets
// engine_args: a truthy "bad" key is rejected, and it stamps "validated" onto
// the effective profile so tests can confirm backend validation was reached.
type fakeBackend struct{}

func (fakeBackend) ValidateProfile(p Profile) (Profile, error) {
	if bad, _ := p.EngineArgs["bad"].(bool); bad {
		return Profile{}, errors.New("engine_args.bad is not allowed")
	}
	out := p
	out.EngineArgs = map[string]any{"validated": true}
	for k, v := range p.EngineArgs {
		out.EngineArgs[k] = v
	}
	return out, nil
}

func testLookup(v BackendValidator) BackendLookup {
	return func(backend string) (BackendValidator, error) {
		if backend == "fake" {
			return v, nil
		}
		return nil, fmt.Errorf("unknown backend %q", backend)
	}
}

func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	return NewFileStore(t.TempDir(), 0, testLookup(fakeBackend{}))
}

func validYAML(name string) string {
	return fmt.Sprintf(`version: 1
name: %s
backend: fake
model:
  source: hf/repo
  reference: q4_k_m
listen:
  host: 127.0.0.1
  port: 8080
engine_args:
  temperature: 0.7
`, name)
}

func TestCreateReadBackAndBackendValidation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	created, err := store.Create(ctx, CreateRequest{Name: "alpha", ProfileYAML: validYAML("alpha")})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Effective.Backend != "fake" {
		t.Fatalf("backend = %q, want fake", created.Effective.Backend)
	}
	if created.Effective.Listen.Port != 8080 {
		t.Fatalf("port = %d, want 8080", created.Effective.Listen.Port)
	}
	// Backend validation was reached: fakeBackend stamps "validated".
	if got, _ := created.Effective.EngineArgs["validated"].(bool); !got {
		t.Fatalf("effective engine_args missing backend stamp: %v", created.Effective.EngineArgs)
	}
	// Parsed keeps the raw engine_args (no stamp).
	if _, stamped := created.Parsed.EngineArgs["validated"]; stamped {
		t.Fatalf("parsed engine_args must not carry backend stamp: %v", created.Parsed.EngineArgs)
	}

	got, err := store.Get(ctx, "alpha")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ProfileYAMLPath != filepath.Join(store.root, "alpha", "profile.yaml") {
		t.Fatalf("unexpected profile path %q", got.ProfileYAMLPath)
	}
	if _, err := os.Stat(got.ProfileYAMLPath); err != nil {
		t.Fatalf("profile.yaml not persisted: %v", err)
	}
}

func TestDuplicateCreateRejected(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if _, err := store.Create(ctx, CreateRequest{Name: "dup", ProfileYAML: validYAML("dup")}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := store.Create(ctx, CreateRequest{Name: "dup", ProfileYAML: validYAML("dup")})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate create err = %v, want ErrExists", err)
	}
}

func TestUnknownBackendRejected(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	yaml := `version: 1
name: nob
backend: mystery
model:
  source: hf/repo
listen:
  port: 8080
`
	_, err := store.Create(ctx, CreateRequest{Name: "nob", ProfileYAML: yaml})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown backend err = %v, want ErrInvalid", err)
	}
}

func TestBackendRejectsBadEngineArgs(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	yaml := `version: 1
name: badargs
backend: fake
model:
  source: hf/repo
listen:
  port: 8080
engine_args:
  bad: true
`
	_, err := store.Create(ctx, CreateRequest{Name: "badargs", ProfileYAML: yaml})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad engine_args err = %v, want ErrInvalid", err)
	}
}

func TestNameDirMismatchRejected(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	// profile body names "other" but the request targets dir "real".
	_, err := store.Validate(ctx, CreateRequest{Name: "real", ProfileYAML: validYAML("other")})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("name/dir mismatch err = %v, want ErrInvalid", err)
	}
}

func TestPathEscapeRejected(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	for _, name := range []string{"../evil", "..", "a/b", "."} {
		if _, err := store.Get(ctx, name); !errors.Is(err, ErrInvalid) {
			t.Fatalf("get(%q) err = %v, want ErrInvalid", name, err)
		}
	}
}

func TestSymlinkRejected(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if _, err := store.Create(ctx, CreateRequest{Name: "sym", ProfileYAML: validYAML("sym")}); err != nil {
		t.Fatalf("create: %v", err)
	}
	path := filepath.Join(store.root, "sym", "profile.yaml")
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	target := filepath.Join(store.root, "elsewhere.yaml")
	if err := os.WriteFile(target, []byte(validYAML("sym")), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := store.Get(ctx, "sym"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlinked profile err = %v, want ErrInvalid", err)
	}
}

func TestInvalidPortRejected(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	yaml := `version: 1
name: p
backend: fake
model:
  source: hf/repo
listen:
  port: 0
`
	if _, err := store.Validate(ctx, CreateRequest{Name: "p", ProfileYAML: yaml}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid port err = %v, want ErrInvalid", err)
	}
}

func TestMissingRequiredFieldRejected(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	cases := map[string]string{
		"missing model.source": "version: 1\nname: m\nbackend: fake\nlisten:\n  port: 8080\n",
		"missing version":      "name: m\nbackend: fake\nmodel:\n  source: hf/repo\nlisten:\n  port: 8080\n",
		"missing backend":      "version: 1\nname: m\nmodel:\n  source: hf/repo\nlisten:\n  port: 8080\n",
		"empty":                "   \n",
		"unknown top-level key": "version: 1\nname: m\nbackend: fake\nmodel:\n  source: hf/repo\n" +
			"listen:\n  port: 8080\nbogus: true\n",
	}
	for label, yaml := range cases {
		if _, err := store.Validate(ctx, CreateRequest{Name: "m", ProfileYAML: yaml}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: err = %v, want ErrInvalid", label, err)
		}
	}
}

func TestReplaceRevalidates(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if _, err := store.Create(ctx, CreateRequest{Name: "rep", ProfileYAML: validYAML("rep")}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// A valid replacement persists.
	updated := `version: 2
name: rep
backend: fake
model:
  source: hf/other
listen:
  port: 9090
`
	if _, err := store.Replace(ctx, "rep", updated); err != nil {
		t.Fatalf("replace valid: %v", err)
	}
	got, err := store.Get(ctx, "rep")
	if err != nil {
		t.Fatalf("get after replace: %v", err)
	}
	if got.Effective.Listen.Port != 9090 {
		t.Fatalf("port after replace = %d, want 9090", got.Effective.Listen.Port)
	}
	// An invalid replacement is rejected and does not overwrite the good file.
	if _, err := store.Replace(ctx, "rep", "version: 3\nname: rep\nbackend: fake\nlisten:\n  port: 0\n"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("replace invalid err = %v, want ErrInvalid", err)
	}
	still, err := store.Get(ctx, "rep")
	if err != nil {
		t.Fatalf("get after failed replace: %v", err)
	}
	if still.Effective.Listen.Port != 9090 {
		t.Fatalf("failed replace mutated file: port = %d", still.Effective.Listen.Port)
	}
}

func TestListSortedAndDeleteRemovesDir(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	for _, name := range []string{"gamma", "alpha", "beta"} {
		if _, err := store.Create(ctx, CreateRequest{Name: name, ProfileYAML: validYAML(name)}); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := []string{list[0].Name, list[1].Name, list[2].Name}
	want := []string{"alpha", "beta", "gamma"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("list order = %v, want %v", got, want)
		}
	}

	res, err := store.Delete(ctx, "beta")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(res.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("profile dir still present after delete: %v", err)
	}
	if _, err := store.Get(ctx, "beta"); err == nil {
		t.Fatalf("get after delete should fail")
	}
	list, err = store.List(ctx)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len after delete = %d, want 2", len(list))
	}
}
