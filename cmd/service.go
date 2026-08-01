package cmd

import (
	"bytes"
	_ "embed"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"inferencerig/config"
	"inferencerig/platform/filedoc"

	"github.com/spf13/cobra"
)

const launchAgentLabel = "dev.inferencerig.control"

type serviceRunner func(*cobra.Command, string, ...string) error

var (
	//go:embed service.systemd
	systemdDefinition string
	//go:embed service.plist
	launchAgentDefinition string
)

func serviceCommand() *cobra.Command {
	group := &cobra.Command{Use: "service", Short: "Manage the per-user control daemon service"}
	group.AddCommand(&cobra.Command{
		Use: "generate <systemd|launchd>", Short: "Print a native user-service definition", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			executable, err := os.Executable()
			if err != nil {
				return err
			}
			definition, err := serviceDefinition(args[0], executable)
			if err == nil {
				_, err = fmt.Fprint(command.OutOrStdout(), definition)
			}
			return err
		},
	})
	group.AddCommand(&cobra.Command{
		Use: "install", Short: "Install and start the native user service", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error { return installService(command) },
	})
	group.AddCommand(&cobra.Command{
		Use: "uninstall", Short: "Stop and remove the native user service", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error { return uninstallService(command) },
	})
	return group
}

func nativeServiceManager() (string, error) {
	manager := map[string]string{"linux": "systemd", "darwin": "launchd"}[runtime.GOOS]
	if manager == "" {
		return manager, fmt.Errorf("user services are not supported on %s", runtime.GOOS)
	}
	return manager, nil
}

func serviceDefinition(manager, executable string) (string, error) {
	switch manager {
	case "systemd":
		return fmt.Sprintf(systemdDefinition, strconv.Quote(executable)), nil
	case "launchd":
		var path bytes.Buffer
		_ = xml.EscapeText(&path, []byte(executable)) // bytes.Buffer writes cannot fail.
		return fmt.Sprintf(launchAgentDefinition, path.String()), nil
	default:
		return "", fmt.Errorf("unknown service manager %q (want systemd or launchd)", manager)
	}
}

func serviceInstallPath(manager, home string) (string, error) {
	switch manager {
	case "systemd":
		return filepath.Join(home, ".config", "systemd", "user", config.ProjectName+".service"), nil
	case "launchd":
		return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist"), nil
	default:
		return "", fmt.Errorf("unknown service manager %q", manager)
	}
}

func serviceTarget() (string, string, error) {
	manager, err := nativeServiceManager()
	if err != nil {
		return "", "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	path, err := serviceInstallPath(manager, home)
	return manager, path, err
}

func installService(command *cobra.Command) error {
	manager, path, err := serviceTarget()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	definition, _ := serviceDefinition(manager, executable) // manager came from nativeServiceManager.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if _, err := filedoc.WriteFile(path, definition, filedoc.WriteOptions{Perm: 0o644}); err != nil {
		return err
	}
	return activateService(command, manager, path, runNative)
}

func activateService(command *cobra.Command, manager, path string, run serviceRunner) error {
	if manager == "systemd" {
		if err := run(command, "systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		return run(command, "systemctl", "--user", "enable", "--now", config.ProjectName+".service")
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	_ = run(command, "launchctl", "bootout", domain, path)
	return run(command, "launchctl", "bootstrap", domain, path)
}

func uninstallService(command *cobra.Command) error {
	manager, path, err := serviceTarget()
	if err != nil {
		return err
	}
	return deactivateService(command, manager, path, runNative)
}

func deactivateService(command *cobra.Command, manager, path string, run serviceRunner) error {
	if manager == "systemd" {
		_ = run(command, "systemctl", "--user", "disable", "--now", config.ProjectName+".service")
	} else {
		_ = run(command, "launchctl", "bootout", fmt.Sprintf("gui/%d", os.Getuid()), path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if manager == "systemd" {
		return run(command, "systemctl", "--user", "daemon-reload")
	}
	return nil
}

func runNative(command *cobra.Command, name string, args ...string) error {
	process := exec.CommandContext(command.Context(), name, args...)
	process.Stdout, process.Stderr = command.OutOrStdout(), command.ErrOrStderr()
	return process.Run()
}
