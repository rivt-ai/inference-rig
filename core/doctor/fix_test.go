package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inferencerig/config"
)

func TestFixableListsTheThreeRemedies(t *testing.T) {
	writeConfig(t, brokenConfig)
	report := runDoctor(t, Options{ValidateConfig: realValidator})

	var ids []string
	for _, remedy := range report.Fixable() {
		ids = append(ids, remedy.ID)
	}
	if strings.Join(ids, ",") != strings.Join(RemedyIDs(), ",") {
		t.Errorf("Fixable = %v, want %v", ids, RemedyIDs())
	}
}

// A healthy install offers nothing to fix, so --fix has nothing to prompt for.
func TestFixableIsEmptyForHealthyInstall(t *testing.T) {
	writeConfig(t, healthyConfig)
	report := runDoctor(t, Options{ValidateConfig: realValidator})

	if got := report.Fixable(); len(got) != 0 {
		t.Errorf("Fixable = %v, want none", got)
	}
}

func TestApplyEachRemedyMakesConfigLoad(t *testing.T) {
	for _, id := range RemedyIDs() {
		t.Run(id, func(t *testing.T) {
			home := writeConfig(t, brokenConfig)
			path := filepath.Join(home, "config.yaml")

			result, err := Apply(context.Background(), path, id)
			if err != nil {
				t.Fatalf("Apply(%s): %v", id, err)
			}
			if !result.Changed || result.BackupPath == "" {
				t.Errorf("result = %+v, want a change and a backup", result)
			}
			if _, err := config.LoadFile(path); err != nil {
				t.Errorf("config still does not load after %s: %v", id, err)
			}
		})
	}
}

func TestApplyRejectsUnknownRemedy(t *testing.T) {
	home := writeConfig(t, brokenConfig)

	_, err := Apply(context.Background(), filepath.Join(home, "config.yaml"), "make-it-work")

	if err == nil {
		t.Fatal("Apply accepted an unknown remedy")
	}
	// The message has to name the valid ids, or a typo is a dead end.
	for _, id := range RemedyIDs() {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("error %q does not list %q", err, id)
		}
	}
}

// Applying a remedy that is already in place must not write another backup.
func TestApplyIsIdempotent(t *testing.T) {
	home := writeConfig(t, brokenConfig)
	path := filepath.Join(home, "config.yaml")
	if _, err := Apply(context.Background(), path, RemedyBindLoopback); err != nil {
		t.Fatal(err)
	}

	result, err := Apply(context.Background(), path, RemedyBindLoopback)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Errorf("result = %+v, want no change on a second apply", result)
	}
}

// Choosing a security posture with nobody watching is not something a
// diagnostic gets to do, so a non-terminal --fix must refuse.
func TestSelectRemedyRefusesWithoutATerminal(t *testing.T) {
	writeConfig(t, brokenConfig)
	report := runDoctor(t, Options{ValidateConfig: realValidator})

	_, err := SelectRemedy(context.Background(), strings.NewReader(""), &strings.Builder{}, report.Fixable())

	if !errors.Is(err, ErrNotInteractive) {
		t.Fatalf("err = %v, want ErrNotInteractive", err)
	}
}

// Declining is a first-class option, not an error state.
func TestSelectRemedyTreatsNoChangesAsCancelled(t *testing.T) {
	if _, err := SelectRemedy(context.Background(), os.Stdin, os.Stdout, nil); !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled when there is nothing to choose", err)
	}
}

// A non-interactive refusal is only useful if it leaves behind enough to act on.
func TestWriteRemedyOptionsCarriesLiteralEdits(t *testing.T) {
	writeConfig(t, brokenConfig)
	report := runDoctor(t, Options{ValidateConfig: realValidator})

	var out strings.Builder
	if err := WriteRemedyOptions(&out, report.Fixable()); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		"listen_addr: 127.0.0.1:0",
		"disable_auth: false",
		"allow_exposed_without_auth: true",
		"--fix-with=bind-loopback",
		"--fix-with=require-auth",
		"--fix-with=allow-exposed",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}
