package backends_test

import (
	"testing"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
)

func TestRegistryStartsEmpty(t *testing.T) {
	r := backends.NewRegistry()
	if names := r.Names(); len(names) != 0 {
		t.Fatalf("new registry not empty: %v", names)
	}
	if _, ok := r.Lookup("llamacpp"); ok {
		t.Fatal("empty registry returned a backend")
	}
}

func TestRegistryRegisterAndLookup(t *testing.T) {
	r := backends.NewRegistry()
	if err := r.Register(backendtest.New("llamacpp")); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(backendtest.New("mlx")); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(backendtest.New("llamacpp")); err == nil {
		t.Fatal("duplicate registration accepted")
	}
	if err := r.Register(backendtest.New("")); err == nil {
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

// sharedBackend claims to serve several profiles at once without offering a way
// to say which one, which leaves its runtime slot with no defined behaviour.
type sharedBackend struct{ *backendtest.Fake }

func (b *sharedBackend) Capabilities() backends.Capabilities {
	capabilities := b.Fake.Capabilities()
	capabilities.SingleActiveProfile = false
	return capabilities
}

// A backend is either exclusive or a router. The registry is where that is
// enforced, because a manager handed the third combination would have to guess
// whether starting a second profile spawns, replaces or activates.
func TestRegistryRejectsAMultiProfileBackendWithoutAnActivator(t *testing.T) {
	r := backends.NewRegistry()
	if err := r.Register(&sharedBackend{Fake: backendtest.New("shared")}); err == nil {
		t.Fatal("backend serving several profiles without a RuntimeActivator accepted")
	}
	if _, ok := r.Lookup("shared"); ok {
		t.Fatal("rejected backend reached the registry")
	}
}
