package cmd

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestSystemdDefinitionStartsServeAtLogin(t *testing.T) {
	definition, err := serviceDefinition("systemd", "/opt/Inference Rig/inferencerig")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[Unit]", `ExecStart="/opt/Inference Rig/inferencerig" serve`, "WantedBy=default.target"} {
		if !strings.Contains(definition, want) {
			t.Fatalf("definition missing %q:\n%s", want, definition)
		}
	}
}

func TestLaunchAgentReinstallBootsOutBeforeBootstrap(t *testing.T) {
	var calls []string
	run := func(_ *cobra.Command, name string, args ...string) error {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		if strings.Contains(calls[len(calls)-1], "bootout") {
			return errors.New("not loaded")
		}
		return nil
	}
	if err := activateService(&cobra.Command{}, "launchd", "/tmp/control.plist", run); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, "\n") != "launchctl bootout gui/"+strconv.Itoa(os.Getuid())+" /tmp/control.plist\nlaunchctl bootstrap gui/"+strconv.Itoa(os.Getuid())+" /tmp/control.plist" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestServiceUninstallRemovesFileWhenNativeServiceIsAbsent(t *testing.T) {
	for _, manager := range []string{"systemd", "launchd"} {
		t.Run(manager, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "service")
			if err := os.WriteFile(path, []byte("service"), 0o600); err != nil {
				t.Fatal(err)
			}
			run := func(_ *cobra.Command, _ string, args ...string) error {
				if slices.Contains(args, "daemon-reload") {
					return nil
				}
				return errors.New("not found")
			}
			if err := deactivateService(&cobra.Command{}, manager, path, run); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("service file remains: %v", err)
			}
		})
	}
}

func TestLaunchAgentDefinitionStartsServeAtLogin(t *testing.T) {
	definition, err := serviceDefinition("launchd", "/opt/Inference Rig/inferencerig")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<?xml version=", launchAgentLabel, "/opt/Inference Rig/inferencerig", "<string>serve</string>", "<key>RunAtLoad</key>"} {
		if !strings.Contains(definition, want) {
			t.Fatalf("definition missing %q:\n%s", want, definition)
		}
	}
	if err := xml.Unmarshal([]byte(definition), new(any)); err != nil {
		t.Fatalf("invalid LaunchAgent plist XML: %v", err)
	}
}

func TestServiceCommandGeneratesBothNativeFormats(t *testing.T) {
	for _, manager := range []string{"systemd", "launchd"} {
		root := NewRootCommand()
		var out strings.Builder
		root.SetOut(&out)
		root.SetArgs([]string{"service", "generate", manager})
		if err := root.Execute(); err != nil {
			t.Fatalf("generate %s: %v", manager, err)
		}
		if out.Len() == 0 {
			t.Fatalf("generate %s produced no output", manager)
		}
	}
}

func TestServiceInstallLocationsArePerUser(t *testing.T) {
	systemd, err := serviceInstallPath("systemd", "/home/test")
	if err != nil || systemd != "/home/test/.config/systemd/user/inferencerig.service" {
		t.Fatalf("systemd path = %q, err = %v", systemd, err)
	}
	launchd, err := serviceInstallPath("launchd", "/Users/test")
	if err != nil || launchd != "/Users/test/Library/LaunchAgents/dev.inferencerig.control.plist" {
		t.Fatalf("launchd path = %q, err = %v", launchd, err)
	}
}
