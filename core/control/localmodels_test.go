package control

import (
	"reflect"
	"testing"

	"inferencerig/backends"
	"inferencerig/backends/backendtest"
	"inferencerig/core/profiles"
)

// A profile whose source is a URL runs the file downloaded from it, so that
// file must not read as unused (and deletable without cascade).
func TestProfilesUsingModelResolvesURLSource(t *testing.T) {
	t.Chdir(t.TempDir()) // the fake backend materializes into the working directory
	registry := backends.NewRegistry()
	if err := registry.Register(backendtest.New("test")); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Dependencies{
		Registry: registry,
		Profiles: profiles.NewFileStore(t.TempDir(), 0, registry.BackendLookup()),
	})
	ctx := t.Context()
	if _, err := manager.PutProfile(ctx, "url", profileYAML("url", "https://example.test/m"), true); err != nil {
		t.Fatal(err)
	}
	using, err := manager.ProfilesUsingModel(ctx, "/models/model.bin")
	if err != nil || !reflect.DeepEqual(using, []string{"url"}) {
		t.Fatalf("using = %v, err = %v", using, err)
	}
	if _, err := manager.DeleteLocalModelCascade(ctx, "test", "/models/model.bin", false); Kind(err) != ErrorConflict {
		t.Fatalf("delete of in-use URL-sourced model: err = %v", err)
	}
}

func TestProfileReferencesModelViaEngineArgs(t *testing.T) {
	const draft = "/home/u/.inferencerig/models/mtp-Ornith-1.5-9B-head-Q8_0.gguf"
	profile := profiles.Profile{
		Name:  "ornith-9b-1-5-mtp",
		Model: profiles.ModelSpec{Source: "/home/u/.inferencerig/models/Ornith-1.5-9B-Q8_0.gguf"},
		EngineArgs: map[string]any{
			"spec-type":   "draft-mtp",
			"model-draft": draft,
			"ctx-size":    8192,
		},
	}
	if !profileReferencesModel(profile, draft) {
		t.Fatalf("profile with model-draft engine arg should reference %s", draft)
	}
	if !profileReferencesModel(profile, profile.Model.Source) {
		t.Fatalf("profile should still reference its model source")
	}
	if profileReferencesModel(profile, "/home/u/.inferencerig/models/other.gguf") {
		t.Fatalf("profile should not reference an unrelated model")
	}
}
