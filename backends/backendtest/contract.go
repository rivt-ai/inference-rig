package backendtest

import (
	"context"
	"testing"

	"inferencerig/backends"
	"inferencerig/core/profiles"
)

// RunContractTests asserts the invariants every conforming backend must hold.
// newBackend must return a fresh backend on each call. It is engine-neutral so
// the real llama.cpp and MLX backends reuse it verbatim.
func RunContractTests(t *testing.T, newBackend func() backends.Backend) {
	t.Helper()
	t.Run("Name", func(t *testing.T) { checkName(t, newBackend()) })
	t.Run("ValidateProfile", func(t *testing.T) { checkValidateProfile(t, newBackend()) })
	t.Run("MaterializeLaunch", func(t *testing.T) { checkMaterializeLaunch(t, newBackend()) })
	t.Run("ResolvePlan", func(t *testing.T) { checkResolvePlan(t, newBackend()) })
	t.Run("Fit", func(t *testing.T) { checkFit(t, newBackend()) })
	t.Run("Install", func(t *testing.T) { checkInstall(t, newBackend()) })
	t.Run("Capabilities", func(t *testing.T) { checkCapabilities(t, newBackend()) })
}

// validProfile builds a structurally valid neutral profile for backend name.
func validProfile(name string) profiles.Profile {
	return profiles.Profile{
		Version: "1",
		Name:    "contract-demo",
		Backend: name,
		Model:   profiles.ModelSpec{Source: "example://model", Reference: "ref"},
		Listen:  profiles.ListenSpec{Host: "127.0.0.1", Port: 8080},
	}
}

func checkName(t *testing.T, b backends.Backend) {
	t.Helper()
	if b.Name() == "" {
		t.Fatal("Name() is empty")
	}
}

func checkValidateProfile(t *testing.T, b backends.Backend) {
	t.Helper()
	if _, err := b.ValidateProfile(validProfile(b.Name())); err != nil {
		t.Fatalf("ValidateProfile rejected a valid profile: %v", err)
	}
	bad := validProfile(b.Name())
	bad.Model.Source = ""
	if _, err := b.ValidateProfile(bad); err == nil {
		t.Fatal("ValidateProfile accepted a structurally invalid profile")
	}
}

func checkMaterializeLaunch(t *testing.T, b backends.Backend) {
	t.Helper()
	p := validProfile(b.Name())
	m, err := b.Materialize(p)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	spec, err := b.LaunchSpec(p, m)
	if err != nil {
		t.Fatalf("LaunchSpec: %v", err)
	}
	if spec.Executable == "" && spec.BuildErr == nil {
		t.Fatal("LaunchSpec produced no executable and no BuildErr")
	}
}

func checkResolvePlan(t *testing.T, b backends.Backend) {
	t.Helper()
	resolved, err := b.Resolve(context.Background(), validProfile(b.Name()))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	plan, err := b.Plan(resolved)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Items) == 0 {
		t.Fatal("Plan produced an empty artifact plan")
	}
	if plan.MultiFile != resolved.MultiFile {
		t.Fatalf("Plan.MultiFile %v disagrees with ResolvedModel.MultiFile %v", plan.MultiFile, resolved.MultiFile)
	}
}

func checkFit(t *testing.T, b backends.Backend) {
	t.Helper()
	est, err := b.Fit(validProfile(b.Name()), backends.HostResources{
		AvailableRAMBytes: 64 << 30,
		VRAMBytes:         24 << 30,
	})
	if err != nil {
		t.Fatalf("Fit: %v", err)
	}
	if !est.Level.Valid() {
		t.Fatalf("Fit returned an invalid level %q", est.Level)
	}
}

func checkInstall(t *testing.T, b backends.Backend) {
	t.Helper()
	first, err := b.Install(context.Background(), backends.InstallOptions{})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if first.Version == "" {
		t.Fatal("Install reported an empty version")
	}
	second, err := b.Install(context.Background(), backends.InstallOptions{})
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if second.Changed {
		t.Fatal("Install is not idempotent: a repeat install reported Changed")
	}
}

func checkCapabilities(t *testing.T, b backends.Backend) {
	t.Helper()
	c := b.Capabilities()
	if !c.SingleFileArtifacts && !c.MultiFileArtifacts {
		t.Fatal("Capabilities advertise neither single- nor multi-file artifacts")
	}
	if !c.DiscreteVRAM && !c.UnifiedMemory {
		t.Fatal("Capabilities advertise neither discrete-VRAM nor unified-memory fit")
	}
	assertPlanMatchesCapabilities(t, b, c)
}

// assertPlanMatchesCapabilities checks the artifact form a backend actually
// plans is one it advertises.
func assertPlanMatchesCapabilities(t *testing.T, b backends.Backend, c backends.Capabilities) {
	t.Helper()
	resolved, err := b.Resolve(context.Background(), validProfile(b.Name()))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	plan, err := b.Plan(resolved)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.MultiFile && !c.MultiFileArtifacts {
		t.Fatal("Plan is multi-file but Capabilities.MultiFileArtifacts is false")
	}
	if !plan.MultiFile && !c.SingleFileArtifacts {
		t.Fatal("Plan is single-file but Capabilities.SingleFileArtifacts is false")
	}
}
