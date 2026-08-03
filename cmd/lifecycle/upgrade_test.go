package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"inferencerig/config"
	"inferencerig/internal/buildinfo"
	"inferencerig/platform/process"

	"github.com/spf13/cobra"
)

// recorder captures the order of process operations, which is the whole point
// of the upgrade sequence: stopping must happen before the install replaces the
// binary, or StopDetached's same-inode guard silently declines to stop anything.
type recorder struct {
	steps     []string
	running   map[string]bool
	installer func() error
	startErr  error
}

func (r *recorder) install(context.Context, string, io.Writer) error {
	r.steps = append(r.steps, "install")
	if r.installer != nil {
		return r.installer()
	}
	return nil
}

func (r *recorder) wire(t *testing.T) {
	t.Helper()
	originalStop, originalStart, originalStatus, originalInstall := stopDetached, startDetached, statusDetached, runInstaller
	t.Cleanup(func() {
		stopDetached, startDetached, statusDetached, runInstaller = originalStop, originalStart, originalStatus, originalInstall
	})

	statusDetached = func(name string) (process.DetachedStatus, error) {
		return process.DetachedStatus{Running: r.running[name], PID: 1}, nil
	}
	stopDetached = func(name string) error {
		r.steps = append(r.steps, "stop:"+name)
		return nil
	}
	startDetached = func(name string, _ ...string) error {
		r.steps = append(r.steps, "start:"+name)
		return r.startErr
	}
	runInstaller = r.install
}

func upgradeFor(t *testing.T) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	previous := buildinfo.Version
	buildinfo.Version = "v0.0.1"
	t.Cleanup(func() { buildinfo.Version = previous })

	out := &bytes.Buffer{}
	command := UpgradeCommand()
	command.SetOut(out)
	command.SetErr(out)
	command.SetArgs(nil)
	return command, out
}

func TestUpgradeStopsBeforeInstallingAndStartsAfter(t *testing.T) {
	rec := &recorder{running: map[string]bool{
		config.ServiceProcessName(config.StartupServiceControl): true,
		config.ServiceProcessName(config.StartupServiceWeb):     true,
	}}
	rec.wire(t)
	command, _ := upgradeFor(t)

	if err := runUpgrade(command, ""); err != nil {
		t.Fatalf("runUpgrade: %v", err)
	}

	got := strings.Join(rec.steps, " ")
	install := slices.Index(rec.steps, "install")
	for i, step := range rec.steps {
		if strings.HasPrefix(step, "stop:") && i > install {
			t.Fatalf("stop happened after install, which the inode guard would reject: %s", got)
		}
		if strings.HasPrefix(step, "start:") && i < install {
			t.Fatalf("start happened before install, so it would run the old binary: %s", got)
		}
	}
	if len(rec.steps) != 5 {
		t.Fatalf("want 2 stops, 1 install, 2 starts; got %s", got)
	}
}

// Services that were not running must be left alone rather than started.
func TestUpgradeOnlyRestartsWhatWasRunning(t *testing.T) {
	rec := &recorder{running: map[string]bool{
		config.ServiceProcessName(config.StartupServiceControl): true,
	}}
	rec.wire(t)
	command, _ := upgradeFor(t)

	if err := runUpgrade(command, ""); err != nil {
		t.Fatalf("runUpgrade: %v", err)
	}

	for _, step := range rec.steps {
		if strings.Contains(step, config.StartupServiceWeb) {
			t.Fatalf("touched a service that was not running: %v", rec.steps)
		}
	}
}

// A failed download must not cost the user their daemons.
func TestUpgradeRestartsServicesWhenInstallFails(t *testing.T) {
	wantErr := errors.New("download exploded")
	rec := &recorder{
		running:   map[string]bool{config.ServiceProcessName(config.StartupServiceControl): true},
		installer: func() error { return wantErr },
	}
	rec.wire(t)
	command, _ := upgradeFor(t)

	if err := runUpgrade(command, ""); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if last := rec.steps[len(rec.steps)-1]; !strings.HasPrefix(last, "start:") {
		t.Fatalf("services were left stopped after a failed install: %v", rec.steps)
	}
}

// Nothing is stopped and nothing is installed when the binary is a dev build.
func TestUpgradeRefusesDevBuild(t *testing.T) {
	rec := &recorder{running: map[string]bool{
		config.ServiceProcessName(config.StartupServiceControl): true,
	}}
	rec.wire(t)
	command, _ := upgradeFor(t)
	buildinfo.Version = "dev"

	if err := command.Execute(); err == nil {
		t.Fatal("want an error for a dev build")
	}
	if len(rec.steps) != 0 {
		t.Fatalf("dev build touched processes: %v", rec.steps)
	}
}

func TestUpgradeAcceptsUpdateAlias(t *testing.T) {
	command := UpgradeCommand()
	if !slices.Contains(command.Aliases, "update") {
		t.Fatalf("aliases = %v, want to include update", command.Aliases)
	}
}
