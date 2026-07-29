package migrate

import (
	"context"
	"testing"

	"inferencerig/core/profiles"
)

type testImporter struct{ candidates []Candidate }

func (i testImporter) Preview(context.Context) ([]Candidate, error) {
	return append([]Candidate(nil), i.candidates...), nil
}

type testValidator struct{}

func (testValidator) ValidateProfile(profile profiles.Profile) (profiles.Profile, error) {
	return profile, nil
}

func TestServicePreviewsAndAppliesCreateOnlyRepeatably(t *testing.T) {
	store := profiles.NewFileStore(t.TempDir(), 0, func(string) (profiles.BackendValidator, error) {
		return testValidator{}, nil
	})
	service := NewService(store)
	importer := testImporter{candidates: []Candidate{{
		Name: "demo", SourcePath: "/read-only/source",
		ProfileYAML: "version: 1\nname: demo\nbackend: test\nmodel:\n  source: model\nlisten:\n  port: 8080\n",
	}}}
	first, err := service.Preview(context.Background(), importer)
	if err != nil || len(first.Items) != 1 {
		t.Fatalf("plan = %#v, err = %v", first, err)
	}
	second, err := service.Preview(context.Background(), importer)
	if err != nil || second.Items[0].ProfileYAML != first.Items[0].ProfileYAML {
		t.Fatalf("repeat plan = %#v, err = %v", second, err)
	}
	applied, err := service.Apply(context.Background(), first)
	if err != nil || len(applied.Created) != 1 {
		t.Fatalf("apply = %#v, err = %v", applied, err)
	}
	repeated, err := service.Apply(context.Background(), first)
	if err != nil || len(repeated.Skipped) != 1 {
		t.Fatalf("repeat apply = %#v, err = %v", repeated, err)
	}
}
