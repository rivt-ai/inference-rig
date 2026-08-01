// Package doctor diagnoses an InferenceRig installation without needing one to
// be running.
//
// Two rules shape everything here. A check that cannot be answered without a
// live daemon reports Skipped rather than Fail, because the daemon being down
// is the normal case for a diagnostic and must not read as a wall of problems.
// And doctor never starts, stops or repairs anything: it is called when things
// are already broken, so it must not destroy the evidence it is reporting on —
// which is why it reads PID files with pidfile.Read rather than Running, and
// dials the control socket rather than clearing it.
package doctor

import (
	"context"
	"sync"
	"time"

	"inferencerig/config"
)

// Options carries what doctor cannot build for itself.
//
// The funcs are injected rather than imported so this package does not depend
// on bootstrap or the RPC transport, which would invert the adapters -> core
// layering. cmd wires them, the same way it already wires the CLI's validator.
type Options struct {
	// ValidateConfig answers "would startup accept this config", by running
	// exactly what startup runs. A nil value skips the check rather than
	// reporting a healthy config it never actually examined.
	ValidateConfig func(context.Context) error
	// DialControl reaches a running daemon. Nil means daemon checks skip.
	DialControl func(socket string) (HealthChecker, error)
	// Inventory answers what only the backend registry can: engine installs,
	// accelerators and model storage. Nil skips those checks.
	Inventory func(context.Context, bool) (Inventory, error)
	// VerifyModels re-hashes model files against their recorded digests. Off by
	// default: it reads every byte of storage that can run to hundreds of GB.
	VerifyModels bool
	// Now defaults to time.Now.
	Now func() time.Time
}

// HealthChecker is the one control-plane call doctor makes. Narrowing it here
// keeps the generated RPC client out of this package's interface.
type HealthChecker interface {
	Health(context.Context) error
}

// Runner executes the check list.
type Runner struct {
	opts   Options
	checks []check
}

// env is resolved once and shared by every check, so a run is one consistent
// view rather than each check racing to re-read the same files.
type env struct {
	paths   config.Paths
	cfg     config.Config
	loadErr error
	opts    Options

	// The inventory is built once and shared: three checks read it, and
	// probing accelerators shells out to nvidia-smi.
	inventoryOnce sync.Once
	inventoryVal  Inventory
	inventoryErr  error
}

func (e *env) inventory(ctx context.Context) (Inventory, error) {
	e.inventoryOnce.Do(func() {
		e.inventoryVal, e.inventoryErr = e.opts.Inventory(ctx, e.opts.VerifyModels)
	})
	return e.inventoryVal, e.inventoryErr
}

type check func(context.Context, *env) Check

// NewRunner returns a Runner over the standard check list.
func NewRunner(opts Options) *Runner {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Runner{opts: opts, checks: []check{
		checkConfigValid,
		checkAuthPosture,
		checkPermissions,
		checkPIDFile,
		checkPort,
		checkSocket,
		checkDaemonReachable,
		checkRecentLog,
		checkRecentFailures,
		checkEngines,
		checkAccelerators,
		checkModels,
	}}
}

// Run executes every check. It returns an error only when the installation
// cannot be located at all; every other problem is a Check in the Report,
// because a diagnostic that aborts on the first fault diagnoses nothing.
func (r *Runner) Run(ctx context.Context) (Report, error) {
	paths, err := config.ResolvePaths()
	if err != nil {
		return Report{}, err
	}
	current := &env{paths: paths, opts: r.opts}
	current.cfg, current.loadErr = config.LoadOrDefault()

	report := Report{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   r.opts.Now(),
		Home:          paths.Home,
		ConfigPath:    paths.Config,
		Counts:        map[Status]int{},
	}
	for _, run := range r.checks {
		result := run(ctx, current)
		report.Checks = append(report.Checks, result)
		report.Counts[result.Status]++
	}
	return report, nil
}
