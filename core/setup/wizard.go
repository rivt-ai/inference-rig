// Package setup provides the first-run configuration and canonical profile
// wizard.
package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"

	"inferencerig/config"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
	"inferencerig/internal/prompt"
)

var ErrCancelled = errors.New("setup cancelled")

// Answers are the normalized values collected by the setup form.
//
// Setup writes the config file only. Profiles are created from the TUI or web
// UI, which can browse the catalog and download a model in the same step —
// setup could only ask the user to type a model source from memory, and a
// profile is not needed for anything to start.
type Answers struct {
	ListenAddr, AuthTokenEnv, ModelStorageDir string
	// Backend is the backend offered for installation during setup. It is not
	// written anywhere: installing is a machine-level action, and the profile
	// that eventually uses the backend picks its own.
	Backend         string
	StartupServices []string
	// DisableAuth serves the gateway without a bearer token. Pairing it with a
	// network-reachable bind is allowed but requires an explicit confirmation
	// here and warns on every config load.
	DisableAuth bool
}

// Wizard discovers capabilities and persists profiles through canonical RPC.
type Wizard struct {
	client controlv1connect.ControlServiceClient
}

// NewWizard creates a setup wizard.
func NewWizard(client controlv1connect.ControlServiceClient) *Wizard {
	if client == nil {
		panic("setup: control client is required")
	}
	return &Wizard{client: client}
}

// Backends returns the available capability descriptors.
func (w *Wizard) Backends(ctx context.Context) ([]*controlv1.BackendInfo, error) {
	response, err := w.client.ListBackends(ctx, &controlv1.ListBackendsRequest{})
	if err != nil {
		return nil, err
	}
	return response.GetBackends(), nil
}

func (w *Wizard) collect(ctx context.Context, paths Paths, input io.Reader, output io.Writer) (Answers, error) {
	backends, err := w.Backends(ctx)
	if err != nil {
		return Answers{}, err
	}
	if len(backends) == 0 {
		return Answers{}, fmt.Errorf("setup: no backends are available")
	}
	answers, err := runCollectForm(ctx, paths, backends, input, output)
	if err != nil {
		return Answers{}, err
	}
	if err := w.applySafetyGates(ctx, backends, input, output, &answers); err != nil {
		return Answers{}, err
	}
	return answers, nil
}

// runCollectForm builds and runs the interactive setup form, then normalizes
// the raw answers (trimmed strings, default token env, expanded storage
// path, startup service list) into their stored form.
func runCollectForm(
	ctx context.Context, paths Paths, backends []*controlv1.BackendInfo, input io.Reader, output io.Writer,
) (Answers, error) {
	answers := defaultAnswers(paths, backends[0].GetName())
	startup := "both"
	proceed := true
	form := huh.NewForm(
		huh.NewGroup(huh.NewNote().
			Title(config.ProjectDisplayName+" setup").
			Description("This will create:\n  "+paths.Config+
				"\n\nModels and profiles are set up afterwards in the TUI or web interface.")),
		huh.NewGroup(
			huh.NewInput().Title("Public web listen address").Value(&answers.ListenAddr).Validate(validateListen),
			huh.NewInput().Title("Bearer token environment variable").Value(&answers.AuthTokenEnv),
			huh.NewConfirm().
				Title("Run without authentication? (recommended for loopback binds only)").
				Value(&answers.DisableAuth),
			huh.NewInput().Title("Model storage directory").Value(&answers.ModelStorageDir).Validate(validateStorageDir),
			huh.NewSelect[string]().Title("Start automatically").Options(
				huh.NewOption("control + web", "both"),
				huh.NewOption("control", "control"),
				huh.NewOption("web", "web"),
			).Value(&startup),
		),
		huh.NewGroup(
			huh.NewSelect[string]().Title("Backend to install").
				Description("Installed for this machine; profiles choose their own backend later").
				Options(backendOptions(backends)...).Value(&answers.Backend),
		),
		huh.NewGroup(
			huh.NewNote().Title("Review").DescriptionFunc(func() string {
				return reviewSummary(paths, answers, startup)
			}, []any{&answers, &startup}),
			huh.NewConfirm().Title("Write configuration?").Value(&proceed),
		),
	).WithTheme(prompt.Theme()).WithInput(input).WithOutput(output)

	if err := runForm(ctx, form); err != nil {
		return Answers{}, err
	}
	if !proceed {
		return Answers{}, ErrCancelled
	}
	answers.ListenAddr = strings.TrimSpace(answers.ListenAddr)
	answers.AuthTokenEnv = strings.TrimSpace(answers.AuthTokenEnv)
	if answers.AuthTokenEnv == "" {
		answers.AuthTokenEnv = config.DefaultAuthTokenEnv
	}
	answers.ModelStorageDir = config.ExpandHome(strings.TrimSpace(answers.ModelStorageDir))
	answers.StartupServices = startupServices(startup)
	return answers, nil
}

// applySafetyGates ensures the selected backend is installed and, for a
// remote-capable bind without auth, requires an explicit confirmation before
// setup is allowed to proceed.
func (w *Wizard) applySafetyGates(
	ctx context.Context, backends []*controlv1.BackendInfo, input io.Reader, output io.Writer, answers *Answers,
) error {
	selected := backendByName(backends, answers.Backend)
	if selected == nil {
		return fmt.Errorf("setup: backend %q is not available", answers.Backend)
	}
	if err := w.ensureBackend(ctx, input, output, selected); err != nil {
		return err
	}
	if prompt := remoteBindWarning(*answers); prompt != "" {
		if err := requireConfirm(ctx, input, output, prompt); err != nil {
			return err
		}
	}
	return nil
}

// remoteBindWarning returns the confirmation prompt for a remote-capable bind,
// or "" when the bind is loopback-only or no confirmation is warranted. A
// remote-capable bind may serve unauthenticated, but only deliberately: this
// confirmation is what renderConfig turns into security.allow_exposed_without_auth,
// without which such a config is refused at load.
func remoteBindWarning(answers Answers) string {
	if !(&config.Config{ListenAddr: answers.ListenAddr}).AllowsNonLoopback() {
		return ""
	}
	if answers.DisableAuth {
		return "Remote-capable bind without authentication — anyone who can reach " + answers.ListenAddr + " gets full control. Continue?"
	}
	if os.Getenv(answers.AuthTokenEnv) == "" {
		return "Remote-capable bind without a populated token environment — continue anyway?"
	}
	return ""
}

func (w *Wizard) ensureBackend(ctx context.Context, input io.Reader, output io.Writer, backend *controlv1.BackendInfo) error {
	response, err := w.client.GetBackendInstallStatus(ctx, &controlv1.GetBackendInstallStatusRequest{Backend: backend.GetName()})
	if err != nil || response.GetInstalled() {
		return err
	}
	if !backend.GetCapabilities().GetManagedInstall() {
		return requireConfirm(ctx, input, output, "The selected backend is not currently usable — continue anyway?")
	}
	install := true
	form := huh.NewForm(huh.NewGroup(huh.NewConfirm().
		Title("The selected backend is not installed. Install it now?").
		Value(&install))).WithTheme(prompt.Theme()).WithInput(input).WithOutput(output)
	if err := runForm(ctx, form); err != nil {
		return err
	}
	if !install {
		return requireConfirm(ctx, input, output, "Continue without an installed backend?")
	}
	_, _ = fmt.Fprintln(output, "Installing backend…")
	_, err = w.client.InstallBackend(ctx, &controlv1.InstallBackendRequest{Backend: backend.GetName()})
	return err
}

var runForm = func(ctx context.Context, form *huh.Form) error {
	if err := form.RunWithContext(ctx); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return ErrCancelled
		}
		return err
	}
	return nil
}

func requireConfirm(ctx context.Context, input io.Reader, output io.Writer, title string) error {
	proceed := false
	form := huh.NewForm(huh.NewGroup(huh.NewConfirm().Title(title).Value(&proceed))).
		WithTheme(prompt.Theme()).WithInput(input).WithOutput(output)
	if err := runForm(ctx, form); err != nil {
		return err
	}
	if !proceed {
		return ErrCancelled
	}
	return nil
}

func backendOptions(backends []*controlv1.BackendInfo) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(backends))
	for _, backend := range backends {
		options = append(options, huh.NewOption(backend.GetName(), backend.GetName()))
	}
	return options
}

func backendByName(backends []*controlv1.BackendInfo, name string) *controlv1.BackendInfo {
	for _, backend := range backends {
		if backend.GetName() == name {
			return backend
		}
	}
	return nil
}

func validateListen(value string) error {
	if _, _, err := net.SplitHostPort(strings.TrimSpace(value)); err != nil {
		return fmt.Errorf("listen address must be host:port: %w", err)
	}
	return nil
}

func validateStorageDir(value string) error {
	path := config.ExpandHome(strings.TrimSpace(value))
	if path == "" {
		return fmt.Errorf("model storage directory is required")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("model storage directory must be an absolute path or start with ~")
	}
	return nil
}

func startupServices(selected string) []string {
	switch selected {
	case "control":
		return []string{config.StartupServiceControl}
	case "web":
		return []string{config.StartupServiceWeb}
	default:
		return config.DefaultStartupServices()
	}
}

// authSummary describes the effective auth posture, calling out the exposed
// case so the confirmation screen never understates it.
func authSummary(answers Answers) string {
	if !answers.DisableAuth {
		return "bearer token from $" + answers.AuthTokenEnv
	}
	if (&config.Config{ListenAddr: answers.ListenAddr}).AllowsNonLoopback() {
		return "DISABLED on a non-loopback bind (exposed)"
	}
	return "disabled (loopback only)"
}

func reviewSummary(paths Paths, answers Answers, startup string) string {
	return config.ProjectDisplayName + " home: " + paths.Home + "\n" +
		"Config file:       " + paths.Config + "\n" +
		"Model storage:     " + answers.ModelStorageDir + "\n" +
		"Public address:    " + answers.ListenAddr + "\n" +
		"Authentication:    " + authSummary(answers) + "\n" +
		"Startup services:  " + strings.Join(startupServices(startup), ", ") + "\n" +
		"Backend:           " + answers.Backend
}
