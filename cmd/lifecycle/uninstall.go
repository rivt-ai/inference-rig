package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"inferencerig/config"
	"inferencerig/internal/prompt"
)

type uninstallChoices struct {
	settings bool
	command  bool
}

func UninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use: "uninstall", Short: "Uninstall InferenceRig interactively", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error { return runUninstall(command) },
	}
}

func runUninstall(command *cobra.Command) error {
	home, err := config.Home()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	choices, confirmed, err := askUninstall(command, home, executable)
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			_, _ = fmt.Fprintln(command.OutOrStdout(), "uninstall cancelled")
			return nil
		}
		return err
	}
	if !confirmed {
		_, _ = fmt.Fprintln(command.OutOrStdout(), "uninstall cancelled")
		return nil
	}

	stopServices(command.OutOrStdout(), runningServices())
	if err := uninstallNativeService(command); err != nil {
		return err
	}
	if err := removeUninstallTargets(home, executable, choices); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(command.OutOrStdout(), "Goodbye, and thanks for all the fish.")
	return nil
}

func askUninstall(command *cobra.Command, home, executable string) (uninstallChoices, bool, error) {
	choices := uninstallChoices{}
	confirmed := false
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().Title("Delete "+home+" settings and data?").Value(&choices.settings),
			huh.NewConfirm().Title("Delete the "+executable+" command?").Value(&choices.command),
		),
		huh.NewGroup(
			huh.NewNote().Title("Confirm uninstall").Description("InferenceRig services will be stopped and removed."),
			huh.NewConfirm().Title("Uninstall InferenceRig?").Value(&confirmed),
		),
	).WithTheme(prompt.Theme()).WithInput(command.InOrStdin()).WithOutput(command.OutOrStdout())
	return choices, confirmed, form.RunWithContext(command.Context())
}

func uninstallNativeService(command *cobra.Command) error {
	manager, path, err := serviceTarget()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return deactivateService(command, manager, path, runNative)
}

func removeUninstallTargets(home, executable string, choices uninstallChoices) error {
	if choices.settings {
		if !filepath.IsAbs(home) || filepath.Dir(filepath.Clean(home)) == filepath.Clean(home) {
			return fmt.Errorf("refusing to delete unsafe settings path %q", home)
		}
		if err := os.RemoveAll(home); err != nil {
			return fmt.Errorf("delete settings %q: %w", home, err)
		}
	}
	if choices.command {
		if err := os.Remove(executable); err != nil {
			return fmt.Errorf("delete command %q: %w", executable, err)
		}
	}
	return nil
}
