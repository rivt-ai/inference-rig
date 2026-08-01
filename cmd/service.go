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
	"strings"

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
		commands := [][]string{{"--user", "daemon-reload"}, {"--user", "enable", config.ProjectName + ".service"}, {"--user", "restart", config.ProjectName + ".service"}}
		for _, args := range commands {
			if err := run(command, "systemctl", args...); err != nil {
				return err
			}
		}
		return nil
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	if err := run(command, "launchctl", "bootout", domain, path); err != nil && !nativeServiceAbsent(err) {
		return err
	}
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
	var err error
	if manager == "systemd" {
		err = run(command, "systemctl", "--user", "disable", "--now", config.ProjectName+".service")
	} else {
		err = run(command, "launchctl", "bootout", fmt.Sprintf("gui/%d", os.Getuid()), path)
	}
	if err != nil && !nativeServiceAbsent(err) {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if manager == "systemd" {
		return run(command, "systemctl", "--user", "daemon-reload")
	}
	return nil
}

func nativeServiceAbsent(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "could not find service") || strings.Contains(message, "does not exist") ||
		strings.Contains(message, "not loaded") || strings.Contains(message, "no such process")
}

func runNative(command *cobra.Command, name string, args ...string) error {
	process := exec.CommandContext(command.Context(), name, args...)
	output, err := process.CombinedOutput()
	writer := command.OutOrStdout()
	if err != nil {
		writer = command.ErrOrStderr()
	}
	_, _ = writer.Write(output)
	if detail := strings.TrimSpace(string(output)); err != nil && detail != "" {
		return fmt.Errorf("%s: %w", detail, err)
	}
	return err
}
