package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/huh/v2"

	"inferencerig/core/configstore"
	"inferencerig/internal/prompt"
	"inferencerig/platform/terminal"
)

// ErrCancelled reports that the operator declined to change anything. Callers
// render it as a normal exit: choosing not to act is not a failure.
var ErrCancelled = errors.New("no changes made")

// ErrNotInteractive reports that --fix needs a terminal it does not have.
var ErrNotInteractive = errors.New("--fix needs an interactive terminal")

// runForm is a package var so tests can drive the selection without a terminal.
var runForm = func(ctx context.Context, form *huh.Form) error {
	if err := form.RunWithContext(ctx); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return ErrCancelled
		}
		return err
	}
	return nil
}

// FixResult describes what a repair did.
type FixResult struct {
	RemedyID   string
	Changed    bool
	BackupPath string
}

// Fixable returns the remedies the report offers, in the order they were
// reported. Empty means nothing here can be repaired automatically.
func (r Report) Fixable() []Remedy {
	var out []Remedy
	for _, check := range r.Checks {
		if check.Status != StatusFail {
			continue
		}
		for _, remedy := range check.Remedies {
			if remedy.ConfigEdit != "" {
				out = append(out, remedy)
			}
		}
	}
	return out
}

// Apply performs one named remedy against the config file at path.
//
// Every remedy is a security decision, so nothing here infers one: the caller
// has to name it, whether that name came from a prompt or a flag.
func Apply(ctx context.Context, configPath, remedyID string) (FixResult, error) {
	store := configstore.NewFileStore(configPath, 0)
	repair, ok := map[string]func(context.Context) (configstore.WriteResult, error){
		RemedyBindLoopback: store.RepairBindLoopback,
		RemedyRequireAuth:  store.RepairRequireAuth,
		RemedyAllowExposed: store.RepairAllowExposed,
	}[remedyID]
	if !ok {
		return FixResult{}, fmt.Errorf("unknown remedy %q (want %s)", remedyID, strings.Join(RemedyIDs(), ", "))
	}
	result, err := repair(ctx)
	if err != nil {
		return FixResult{}, err
	}
	return FixResult{
		RemedyID:   remedyID,
		Changed:    result.BackupPath != "",
		BackupPath: result.BackupPath,
	}, nil
}

// RemedyIDs lists every remedy Apply accepts, for error messages and --help.
func RemedyIDs() []string {
	return []string{RemedyBindLoopback, RemedyRequireAuth, RemedyAllowExposed}
}

// SelectRemedy asks which remedy to apply.
//
// There is no default and no recommendation: the three differ in how much of
// the machine they expose, and only the operator knows what this deployment is
// for. Declining is an explicit option rather than something to infer from a
// cancelled prompt.
func SelectRemedy(ctx context.Context, input io.Reader, output io.Writer, remedies []Remedy) (string, error) {
	if len(remedies) == 0 {
		return "", ErrCancelled
	}
	if !terminal.IsInteractive(input, output) {
		return "", ErrNotInteractive
	}
	options := make([]huh.Option[string], 0, len(remedies)+1)
	for _, remedy := range remedies {
		options = append(options, huh.NewOption(remedy.Title, remedy.ID))
	}
	options = append(options, huh.NewOption("make no changes", ""))

	choice := ""
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("How should this be fixed?").Options(options...).Value(&choice),
	)).WithTheme(prompt.Theme()).WithInput(input).WithOutput(output)
	if err := runForm(ctx, form); err != nil {
		return "", err
	}
	if choice == "" {
		return "", ErrCancelled
	}
	return choice, nil
}

// WriteRemedyOptions prints the remedies as literal config edits. It is what a
// non-interactive --fix leaves behind: the operator cannot be asked, so they
// get everything needed to decide and act without doctor's help.
func WriteRemedyOptions(w io.Writer, remedies []Remedy) error {
	var b strings.Builder
	b.WriteString("\n--fix needs an interactive terminal to choose between:\n")
	for _, remedy := range remedies {
		fmt.Fprintf(&b, "\n  %s\n", remedy.Title)
		for _, line := range nonEmptyLines(remedy.ConfigEdit) {
			fmt.Fprintf(&b, "      %s\n", line)
		}
		fmt.Fprintf(&b, "      $ inferencerig doctor --fix-with=%s\n", remedy.ID)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
