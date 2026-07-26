package setup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/huh/v2"

	"inferencerig/core/profiles"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"

	"gopkg.in/yaml.v3"
)

type testClient struct {
	controlv1connect.ControlServiceClient
	put *controlv1.PutProfileRequest
}

func (c *testClient) ListBackends(context.Context, *controlv1.ListBackendsRequest) (*controlv1.ListBackendsResponse, error) {
	return &controlv1.ListBackendsResponse{Backends: []*controlv1.BackendInfo{{
		Name: "test", Capabilities: &controlv1.BackendCapabilities{MultiFileArtifacts: true},
	}}}, nil
}

func (c *testClient) PutProfile(_ context.Context, request *controlv1.PutProfileRequest) (*controlv1.PutProfileResponse, error) {
	c.put = request
	return &controlv1.PutProfileResponse{Profile: &controlv1.Profile{Name: request.GetName(), Backend: "test"}}, nil
}

func (c *testClient) ListProfiles(context.Context, *controlv1.ListProfilesRequest) (*controlv1.ListProfilesResponse, error) {
	return &controlv1.ListProfilesResponse{}, nil
}

func (c *testClient) GetBackendInstallStatus(context.Context, *controlv1.GetBackendInstallStatusRequest) (*controlv1.GetBackendInstallStatusResponse, error) {
	return &controlv1.GetBackendInstallStatusResponse{Installed: true}, nil
}

func TestWizardDiscoversCapabilitiesAndCreatesThroughRPC(t *testing.T) {
	client := &testClient{}
	wizard := NewWizard(client)
	backends, err := wizard.Backends(context.Background())
	if err != nil || !backends[0].GetCapabilities().GetMultiFileArtifacts() {
		t.Fatalf("backends = %#v, err = %v", backends, err)
	}
	profile, err := wizard.Create(context.Background(), Answers{
		ProfileName: "demo", Backend: "test", ModelSource: "repo", ModelReference: "model",
		Host: "127.0.0.1", Port: 8080,
	})
	if err != nil || profile.GetName() != "demo" || !strings.Contains(client.put.GetProfileYaml(), "backend: test") {
		t.Fatalf("profile = %#v, request = %#v, err = %v", profile, client.put, err)
	}
	var parsed profiles.Profile
	if err := yaml.Unmarshal([]byte(client.put.GetProfileYaml()), &parsed); err != nil || parsed.Model.Source != "repo" {
		t.Fatalf("profile YAML = %q, parsed = %#v, err = %v", client.put.GetProfileYaml(), parsed, err)
	}
}

func TestEnsureSkipsExistingConfigAndRejectsNoninteractiveFirstRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("INFERENCERIG_HOME", home)
	client := &testClient{}
	var output bytes.Buffer
	_, err := NewWizard(client).Ensure(context.Background(), strings.NewReader(""), &output)
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("error = %v", err)
	}
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte("listen_addr: 127.0.0.1:7000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := NewWizard(client).Ensure(context.Background(), strings.NewReader(""), &output)
	if err != nil || !result.Skipped {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}

func TestRenderConfigIncludesSelectedStorageAndAutostart(t *testing.T) {
	paths := Paths{DefaultCatalogCache: "/cache"}
	content, err := renderConfig(paths, Answers{
		ListenAddr: "127.0.0.1:9000", AuthTokenEnv: "TOKEN",
		ModelStorageDir: "/models", ProfileName: "demo",
		StartupServices: []string{"control"}, Autostart: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"model_storage_dir: /models", "- demo", "auth_token_env: TOKEN"} {
		if !strings.Contains(content, want) {
			t.Fatalf("config %q does not contain %q", content, want)
		}
	}
}

func TestWriteCreatesConfigAndForcedRerunKeepsBackup(t *testing.T) {
	home := t.TempDir()
	paths := Paths{
		Home: home, Config: filepath.Join(home, "config.yaml"),
		ProfilesDir:         filepath.Join(home, "profiles"),
		DefaultModelStorage: filepath.Join(home, "models"),
		DefaultCatalogCache: filepath.Join(home, "cache"),
	}
	answers := defaultAnswers(paths, "test")
	answers.ModelSource = "owner/model"
	wizard := NewWizard(&testClient{})
	if _, result, err := wizard.write(context.Background(), paths, answers, false); err != nil {
		t.Fatal(err)
	} else if result.BackupPath != "" {
		t.Fatalf("first write backup = %q", result.BackupPath)
	}
	info, err := os.Stat(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o", info.Mode().Perm())
	}
	answers.ListenAddr = "127.0.0.1:9001"
	_, result, err := wizard.write(context.Background(), paths, answers, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.BackupPath == "" {
		t.Fatal("forced rerun did not create a config backup")
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil || !strings.Contains(string(backup), "127.0.0.1:7000") {
		t.Fatalf("backup = %q, err = %v", backup, err)
	}
}

func TestConfigOverrideSuppressesSetup(t *testing.T) {
	t.Setenv("INFERENCERIG_CONFIG", filepath.Join(t.TempDir(), "custom.yaml"))
	result, err := NewWizard(&testClient{}).Rerun(
		context.Background(), strings.NewReader(""), &bytes.Buffer{},
	)
	if err != nil || !result.Skipped {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}

func TestCancelledFormDoesNotWriteProfile(t *testing.T) {
	previous := runForm
	runForm = func(context.Context, *huh.Form) error { return ErrCancelled }
	t.Cleanup(func() { runForm = previous })
	client := &testClient{}
	_, err := NewWizard(client).collect(
		context.Background(),
		Paths{Home: "/home", Config: "/home/config.yaml", ProfilesDir: "/home/profiles", DefaultModelStorage: "/models"},
		strings.NewReader(""), &bytes.Buffer{}, false,
	)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("error = %v", err)
	}
	if client.put != nil {
		t.Fatalf("profile was written: %#v", client.put)
	}
}
