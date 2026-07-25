package backends

import "testing"

// fakeBackend is a stand-in used to exercise the registry without any real
// engine; the contract tests in Phase 5 grow from here.
type fakeBackend struct{ name string }

func (f fakeBackend) Name() string { return f.name }

func TestRegistryStartsEmpty(t *testing.T) {
	r := NewRegistry()
	if names := r.Names(); len(names) != 0 {
		t.Fatalf("new registry not empty: %v", names)
	}
	if _, ok := r.Lookup("llamacpp"); ok {
		t.Fatal("empty registry returned a backend")
	}
}

func TestRegistryRegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(fakeBackend{"llamacpp"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(fakeBackend{"mlx"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(fakeBackend{"llamacpp"}); err == nil {
		t.Fatal("duplicate registration accepted")
	}
	if err := r.Register(fakeBackend{""}); err == nil {
		t.Fatal("empty backend name accepted")
	}
	b, ok := r.Lookup("mlx")
	if !ok || b.Name() != "mlx" {
		t.Fatalf("lookup mlx = %v, %v", b, ok)
	}
	got := r.Names()
	if len(got) != 2 || got[0] != "llamacpp" || got[1] != "mlx" {
		t.Fatalf("names = %v, want [llamacpp mlx]", got)
	}
}
