package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func stubInventory(inventory Inventory, err error) func(context.Context, bool) (Inventory, error) {
	return func(context.Context, bool) (Inventory, error) { return inventory, err }
}

// An engine whose bytes no longer match what was installed is a real fault:
// a truncated download or a tampered binary.
func TestEngineCheckFailsOnDigestMismatch(t *testing.T) {
	writeConfig(t, healthyConfig)

	report := runDoctor(t, Options{
		ValidateConfig: realValidator,
		Inventory: stubInventory(Inventory{Engines: []EngineState{
			{Backend: "llamacpp", Installed: true, Managed: true, Version: "b4321", Mismatched: true},
		}}, nil),
	})

	check := find(t, report, "engine.install")
	if check.Status != StatusFail {
		t.Fatalf("engine.install = %q, want fail", check.Status)
	}
	if !strings.Contains(check.Detail, "b4321") {
		t.Errorf("detail %q does not name the version", check.Detail)
	}
}

// An engine installed before digests were recorded has nothing to compare
// against. That is not corruption and must not be reported as a fault.
func TestEngineCheckAcceptsAnUnverifiableInstall(t *testing.T) {
	writeConfig(t, healthyConfig)

	report := runDoctor(t, Options{
		ValidateConfig: realValidator,
		Inventory: stubInventory(Inventory{Engines: []EngineState{
			{Backend: "llamacpp", Installed: true, Managed: true, Version: "b4321",
				SkipReason: "no sha256 digest was recorded for this install"},
		}}, nil),
	})

	if got := find(t, report, "engine.install").Status; got != StatusOK {
		t.Errorf("engine.install = %q, want ok when there is nothing to verify against", got)
	}
}

// CPU-only inference is a supported configuration, not a problem.
func TestAcceleratorCheckAcceptsNoAccelerators(t *testing.T) {
	writeConfig(t, healthyConfig)

	report := runDoctor(t, Options{ValidateConfig: realValidator, Inventory: stubInventory(Inventory{}, nil)})

	if got := find(t, report, "accelerators").Status; got != StatusOK {
		t.Errorf("accelerators = %q, want ok on a CPU-only host", got)
	}
}

func TestModelCheckWarnsWhenDiskIsNearlyFull(t *testing.T) {
	writeConfig(t, healthyConfig)

	report := runDoctor(t, Options{
		ValidateConfig: realValidator,
		Inventory: stubInventory(Inventory{Models: ModelState{
			Root: "/models", Count: 3, TotalBytes: 40 << 30,
			DiskBytes: 1000 << 30, FreeBytes: 20 << 30,
		}}, nil),
	})

	if got := find(t, report, "models").Status; got != StatusWarn {
		t.Errorf("models = %q, want warn when the disk is over 90%% full", got)
	}
}

func TestModelCheckFailsOnCorruptModels(t *testing.T) {
	writeConfig(t, healthyConfig)

	report := runDoctor(t, Options{
		ValidateConfig: realValidator,
		VerifyModels:   true,
		Inventory: stubInventory(Inventory{Models: ModelState{
			Root: "/models", Count: 3, VerifyRequested: true,
			Verified: 1, Unverifiable: 1, Corrupt: 1,
			DiskBytes: 1000 << 30, FreeBytes: 900 << 30,
		}}, nil),
	})

	check := find(t, report, "models")
	if check.Status != StatusFail {
		t.Fatalf("models = %q, want fail", check.Status)
	}
	// Unverifiable must be reported separately from corrupt, or models that
	// predate digest recording look like damage.
	if !strings.Contains(check.Detail, "no recorded digest") {
		t.Errorf("detail %q does not separate unverifiable from corrupt", check.Detail)
	}
}

// The registry needs a loadable config, so it is unavailable exactly when
// config.valid already failed. Reporting that again would double-count one
// fault as several.
func TestInventoryChecksSkipWhenConfigIsBroken(t *testing.T) {
	writeConfig(t, brokenConfig)

	report := runDoctor(t, Options{
		ValidateConfig: realValidator,
		Inventory: stubInventory(Inventory{}, errors.New("registry unavailable: "+
			"this whole error must not be repeated on every dependent check")),
	})

	for _, id := range []string{"engine.install", "accelerators", "models"} {
		check := find(t, report, id)
		if check.Status != StatusSkipped {
			t.Errorf("%s = %q, want skip", id, check.Status)
		}
		if strings.Contains(check.Detail, "must not be repeated") {
			t.Errorf("%s repeats the config failure instead of naming it: %q", id, check.Detail)
		}
	}
	if report.Counts[StatusFail] != 1 {
		t.Errorf("fail count = %d, want the one config fault counted once", report.Counts[StatusFail])
	}
}

// Three checks read the inventory, and probing accelerators shells out to
// nvidia-smi, so it must be built once per run.
func TestInventoryIsResolvedOnce(t *testing.T) {
	writeConfig(t, healthyConfig)
	calls := 0

	runDoctor(t, Options{
		ValidateConfig: realValidator,
		Inventory: func(context.Context, bool) (Inventory, error) {
			calls++
			return Inventory{}, nil
		},
	})

	if calls != 1 {
		t.Errorf("inventory built %d times, want once per run", calls)
	}
}
