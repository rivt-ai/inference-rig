package control

import (
	"testing"

	"inferencerig/core/profiles"
)

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
