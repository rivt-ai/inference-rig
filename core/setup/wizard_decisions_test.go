package setup

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"charm.land/huh/v2"

	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
	"inferencerig/platform/filedoc"
)

// The setup wizard decides, once, how exposed a machine will be. Its prompts
// run behind a terminal form, so the decisions themselves are tested directly:
// a wrong answer here is silent, permanent, and reachable from the network.

func TestRemoteBindWarning(t *testing.T) {
	t.Setenv("SETUP_TEST_TOKEN", "")
	tests := []struct {
		name    string
		answers Answers
		want    string
	}{
		{
			name:    "loopback bind needs no confirmation",
			answers: Answers{ListenAddr: "127.0.0.1:7000", DisableAuth: true},
		},
		{
			name:    "remote bind without auth names the exposure",
			answers: Answers{ListenAddr: "0.0.0.0:7000", DisableAuth: true},
			want:    "anyone who can reach 0.0.0.0:7000",
		},
		{
			name:    "remote bind with an empty token env still confirms",
			answers: Answers{ListenAddr: "0.0.0.0:7000", AuthTokenEnv: "SETUP_TEST_TOKEN"},
			want:    "without a populated token environment",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := remoteBindWarning(test.answers)
			if test.want == "" && got != "" {
				t.Fatalf("warning = %q, want none", got)
			}
			if test.want != "" && !strings.Contains(got, test.want) {
				t.Fatalf("warning = %q, want it to mention %q", got, test.want)
			}
		})
	}
}

// A populated token makes a remote bind acceptable without a prompt; without
// this case the function above would pass while always warning.
func TestRemoteBindWarningSilentWithPopulatedToken(t *testing.T) {
	t.Setenv("SETUP_TEST_TOKEN", "secret")
	if got := remoteBindWarning(Answers{ListenAddr: "0.0.0.0:7000", AuthTokenEnv: "SETUP_TEST_TOKEN"}); got != "" {
		t.Fatalf("warning = %q, want none", got)
	}
}

func TestAuthSummaryNamesTheExposedCase(t *testing.T) {
	tests := []struct {
		name    string
		answers Answers
		want    string
	}{
		{"token", Answers{AuthTokenEnv: "TOKEN_ENV"}, "bearer token from $TOKEN_ENV"},
		{"loopback", Answers{DisableAuth: true, ListenAddr: "127.0.0.1:7000"}, "disabled (loopback only)"},
		{"exposed", Answers{DisableAuth: true, ListenAddr: ":7000"}, "DISABLED on a non-loopback bind (exposed)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := authSummary(test.answers); got != test.want {
				t.Fatalf("authSummary = %q, want %q", got, test.want)
			}
		})
	}
}

func TestReviewSummaryReportsEveryDecision(t *testing.T) {
	summary := reviewSummary(
		Paths{Home: "/home/rig", Config: "/home/rig/config.yaml"},
		Answers{ModelStorageDir: "/models", ListenAddr: "127.0.0.1:7000", AuthTokenEnv: "TOKEN_ENV", Backend: "llamacpp"},
		"web",
	)
	for _, want := range []string{"/home/rig", "/home/rig/config.yaml", "/models", "127.0.0.1:7000", "TOKEN_ENV", "llamacpp", "web"} {
		if !strings.Contains(summary, want) {
			t.Errorf("review summary omitted %q:\n%s", want, summary)
		}
	}
}

func TestStartupServices(t *testing.T) {
	tests := map[string][]string{
		"control": {"control"},
		"web":     {"web"},
		"both":    {"control", "web"},
		"":        {"control", "web"},
	}
	for selected, want := range tests {
		got := startupServices(selected)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("startupServices(%q) = %v, want %v", selected, got, want)
		}
	}
}

func TestValidateListen(t *testing.T) {
	if err := validateListen(" 127.0.0.1:7000 "); err != nil {
		t.Errorf("valid address rejected: %v", err)
	}
	if err := validateListen("127.0.0.1"); err == nil {
		t.Error("address without a port was accepted")
	}
}

func TestValidateStorageDir(t *testing.T) {
	if err := validateStorageDir("/models"); err != nil {
		t.Errorf("absolute path rejected: %v", err)
	}
	if err := validateStorageDir("~/models"); err != nil {
		t.Errorf("home-relative path rejected: %v", err)
	}
	if err := validateStorageDir(""); err == nil {
		t.Error("empty storage dir was accepted")
	}
	if err := validateStorageDir("models"); err == nil {
		t.Error("relative storage dir was accepted")
	}
}

func TestBackendByName(t *testing.T) {
	backends := []*controlv1.BackendInfo{{Name: "llamacpp"}, {Name: "mlx"}}
	if got := backendByName(backends, "mlx"); got.GetName() != "mlx" {
		t.Errorf("backendByName = %v", got)
	}
	if got := backendByName(backends, "absent"); got != nil {
		t.Errorf("backendByName(absent) = %v, want nil", got)
	}
}

// stubForm replaces the terminal form for the duration of a test so the
// confirmation logic can be exercised without a TTY.
//
// It can only express "the form returned without the user accepting", because
// huh binds the confirm value by pointer inside the form the stub replaces.
// That is the case worth pinning: an accepted confirmation just continues, but
// a declined one must stop setup, and a form that errors must not be read as
// consent.
func stubForm(t *testing.T, err error) {
	t.Helper()
	original := runForm
	t.Cleanup(func() { runForm = original })
	runForm = func(context.Context, *huh.Form) error { return err }
}

func TestRequireConfirmCancelsWhenDeclined(t *testing.T) {
	stubForm(t, nil)
	err := requireConfirm(context.Background(), strings.NewReader(""), &bytes.Buffer{}, "proceed?")
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("declined confirmation returned %v, want ErrCancelled", err)
	}
}

func TestRequireConfirmPropagatesFormError(t *testing.T) {
	sentinel := errors.New("terminal closed")
	stubForm(t, sentinel)
	err := requireConfirm(context.Background(), strings.NewReader(""), &bytes.Buffer{}, "proceed?")
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

type installClient struct {
	controlv1connect.ControlServiceClient
	installed bool
	statusErr error
	installs  int
}

func (c *installClient) GetBackendInstallStatus(context.Context, *controlv1.GetBackendInstallStatusRequest) (*controlv1.GetBackendInstallStatusResponse, error) {
	if c.statusErr != nil {
		return nil, c.statusErr
	}
	return &controlv1.GetBackendInstallStatusResponse{Installed: c.installed}, nil
}

func (c *installClient) InstallBackend(context.Context, *controlv1.InstallBackendRequest) (*controlv1.InstallBackendResponse, error) {
	c.installs++
	return &controlv1.InstallBackendResponse{}, nil
}

func TestEnsureBackendSkipsWhenInstalled(t *testing.T) {
	client := &installClient{installed: true}
	backend := &controlv1.BackendInfo{Name: "llamacpp", Capabilities: &controlv1.BackendCapabilities{ManagedInstall: true}}
	if err := NewWizard(client).ensureBackend(context.Background(), strings.NewReader(""), &bytes.Buffer{}, backend); err != nil {
		t.Fatalf("ensureBackend = %v", err)
	}
	if client.installs != 0 {
		t.Errorf("installed an already-installed backend %d times", client.installs)
	}
}

func TestEnsureBackendPropagatesStatusError(t *testing.T) {
	sentinel := errors.New("daemon down")
	client := &installClient{statusErr: sentinel}
	backend := &controlv1.BackendInfo{Name: "llamacpp", Capabilities: &controlv1.BackendCapabilities{ManagedInstall: true}}
	err := NewWizard(client).ensureBackend(context.Background(), strings.NewReader(""), &bytes.Buffer{}, backend)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

// A backend that cannot be installed by InferenceRig must not silently pass;
// setup has to ask, and a decline has to cancel.
func TestEnsureBackendConfirmsUnmanagedBackend(t *testing.T) {
	stubForm(t, nil)
	client := &installClient{}
	backend := &controlv1.BackendInfo{Name: "external", Capabilities: &controlv1.BackendCapabilities{}}
	err := NewWizard(client).ensureBackend(context.Background(), strings.NewReader(""), &bytes.Buffer{}, backend)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("error = %v, want ErrCancelled", err)
	}
	if client.installs != 0 {
		t.Errorf("attempted to install an unmanaged backend")
	}
}

func TestPrintSummaryReportsPathsAndBackup(t *testing.T) {
	var output bytes.Buffer
	printSummary(&output, Paths{Config: "/home/rig/config.yaml"}, filedoc.WriteResult{BackupPath: "/home/rig/config.yaml.bak"})
	text := output.String()
	if !strings.Contains(text, "/home/rig/config.yaml") {
		t.Errorf("summary omitted the config path:\n%s", text)
	}
	if !strings.Contains(text, "/home/rig/config.yaml.bak") {
		t.Errorf("summary omitted the backup path, which is the only record of the replaced file:\n%s", text)
	}
}

func TestPrintSummaryOmitsBackupLineWhenNoneWasTaken(t *testing.T) {
	var output bytes.Buffer
	printSummary(&output, Paths{Config: "/home/rig/config.yaml"}, filedoc.WriteResult{})
	if strings.Contains(output.String(), "backup") {
		t.Errorf("summary mentioned a backup that was never taken:\n%s", output.String())
	}
}
