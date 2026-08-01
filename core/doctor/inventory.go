package doctor

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/dustin/go-humanize"
)

// Inventory is everything doctor needs that only the backend registry can
// answer. It is a plain data shape filled in by an injected func, so this
// package never imports bootstrap or any backend.
type Inventory struct {
	Engines      []EngineState
	Accelerators []AcceleratorState
	Models       ModelState
	Warnings     []string
}

// EngineState is one backend's installation as doctor reports it.
type EngineState struct {
	Backend    string
	Installed  bool
	Managed    bool
	Version    string
	Path       string
	Verified   bool
	Mismatched bool
	// SkipReason explains why integrity could not be checked, which is not the
	// same as failing it.
	SkipReason string
}

type AcceleratorState struct {
	Name          string
	UnifiedMemory bool
	TotalBytes    uint64
	UsedBytes     uint64
}

// ModelState is the model storage picture: what is on disk and how much room
// is left for more.
type ModelState struct {
	Root       string
	Count      int
	TotalBytes int64
	FreeBytes  uint64
	DiskBytes  uint64
	// Verified counts models whose recorded digest was re-checked; Unverifiable
	// counts those with no digest on record to check against.
	Verified, Unverifiable, Corrupt int
	VerifyRequested                 bool
}

func checkEngines(ctx context.Context, e *env) Check {
	const id, title = "engine.install", "engine installation"
	inventory, check, available := requireInventory(ctx, e, id, title)
	if !available {
		return check
	}
	if len(inventory.Engines) == 0 {
		return warn(id, title, "no backend reports a usable engine").
			withRemedies(Remedy{ID: "install-backend", Title: "install an engine",
				Command: "inferencerig backend install <name>"})
	}
	var lines []string
	worst := StatusOK
	installed := 0
	for _, engine := range inventory.Engines {
		line, status := describeEngine(engine)
		lines = append(lines, line)
		if engine.Installed {
			installed++
		}
		if status == StatusFail || (status == StatusWarn && worst == StatusOK) {
			worst = status
		}
	}
	sort.Strings(lines)
	summary := fmt.Sprintf("%d of %d backends have a usable engine", installed, len(inventory.Engines))
	return Check{ID: id, Title: title, Status: worst, Summary: summary}.
		withDetail(strings.Join(lines, "\n"))
}

func describeEngine(engine EngineState) (string, Status) {
	label := engine.Backend
	if engine.Version != "" {
		label += " " + engine.Version
	}
	switch {
	case !engine.Installed:
		return label + ": not installed", StatusOK
	case engine.Mismatched:
		return label + ": digest does not match the install record", StatusFail
	case engine.Verified:
		return label + ": digest verified", StatusOK
	default:
		reason := engine.SkipReason
		if reason == "" {
			reason = "integrity not verifiable"
		}
		return label + ": " + reason, StatusOK
	}
}

func checkAccelerators(ctx context.Context, e *env) Check {
	const id, title = "accelerators", "accelerators"
	inventory, check, available := requireInventory(ctx, e, id, title)
	if !available {
		return check
	}
	if len(inventory.Accelerators) == 0 {
		// Not a fault: CPU-only inference is a supported configuration.
		return ok(id, title, "none detected; inference will run on CPU")
	}
	var lines []string
	for _, accelerator := range inventory.Accelerators {
		if accelerator.UnifiedMemory {
			lines = append(lines, accelerator.Name+" (unified memory)")
			continue
		}
		lines = append(lines, fmt.Sprintf("%s, %s of %s in use", accelerator.Name,
			humanize.Bytes(accelerator.UsedBytes), humanize.Bytes(accelerator.TotalBytes)))
	}
	return ok(id, title, fmt.Sprintf("%d detected", len(inventory.Accelerators))).
		withDetail(strings.Join(lines, "\n"))
}

// requireInventory resolves the inventory once per check, turning a missing or
// failing provider into a skip. The registry needs a loadable config to build,
// so this is unavailable exactly when config.valid already failed — reporting
// it again as a second failure would double-count one fault.
func requireInventory(ctx context.Context, e *env, id, title string) (Inventory, Check, bool) {
	if e.opts.Inventory == nil {
		return Inventory{}, skip(id, title, "no backend registry wired"), false
	}
	if e.loadErr != nil {
		// Naming config.valid rather than repeating its error: one fault
		// echoed by every dependent check reads as several.
		return Inventory{}, skip(id, title, "unavailable until the configuration loads (see config.valid)"), false
	}
	inventory, err := e.inventory(ctx)
	if err != nil {
		return Inventory{}, skip(id, title, "the backend registry is unavailable").withDetail(err.Error()), false
	}
	return inventory, Check{}, true
}

func checkModels(ctx context.Context, e *env) Check {
	const id, title = "models", "model storage"
	inventory, check, available := requireInventory(ctx, e, id, title)
	if !available {
		return check
	}
	models := inventory.Models
	summary := fmt.Sprintf("%d model files, %s", models.Count, humanize.Bytes(uint64(max(models.TotalBytes, 0))))
	detail := models.Root
	if models.DiskBytes > 0 {
		detail += fmt.Sprintf("\n%s free of %s", humanize.Bytes(models.FreeBytes), humanize.Bytes(models.DiskBytes))
	}
	if models.VerifyRequested {
		detail += fmt.Sprintf("\n%d verified, %d with no recorded digest, %d corrupt",
			models.Verified, models.Unverifiable, models.Corrupt)
	}
	switch {
	case models.Corrupt > 0:
		return fail(id, title, fmt.Sprintf("%d model files do not match their recorded digest", models.Corrupt)).
			withDetail(detail)
	case models.DiskBytes > 0 && models.FreeBytes < models.DiskBytes/10:
		return warn(id, title, summary+", and the disk is over 90% full").withDetail(detail)
	default:
		return ok(id, title, summary).withDetail(detail)
	}
}
