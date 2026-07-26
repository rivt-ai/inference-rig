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
	"strconv"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"gopkg.in/yaml.v3"

	"inferencerig/config"
	"inferencerig/core/profiles"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

var ErrCancelled = errors.New("setup cancelled")

// Answers are the normalized values collected by the setup form.
type Answers struct {
	ListenAddr, AuthTokenEnv, ModelStorageDir, ProfileName string
	Backend, ModelSource, ModelReference, Host             string
	Port                                                   int
	StartupServices                                        []string
	Autostart                                              bool
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

// Create validates backend availability and creates one canonical YAML profile.
func (w *Wizard) Create(ctx context.Context, answers Answers) (*controlv1.Profile, error) {
	return w.putProfile(ctx, answers, true)
}

func (w *Wizard) putProfile(ctx context.Context, answers Answers, createOnly bool) (*controlv1.Profile, error) {
	backends, err := w.Backends(ctx)
	if err != nil {
		return nil, err
	}
	if backendByName(backends, answers.Backend) == nil {
		return nil, fmt.Errorf("setup: backend %q is not available", answers.Backend)
	}
	data, err := yaml.Marshal(profiles.Profile{
		Version: "1", Name: answers.ProfileName, Backend: answers.Backend,
		Model:  profiles.ModelSpec{Source: answers.ModelSource, Reference: answers.ModelReference},
		Listen: profiles.ListenSpec{Host: answers.Host, Port: answers.Port},
	})
	if err != nil {
		return nil, err
	}
	response, err := w.client.PutProfile(ctx, &controlv1.PutProfileRequest{
		Name: answers.ProfileName, ProfileYaml: string(data), CreateOnly: createOnly,
	})
	if err != nil {
		return nil, err
	}
	return response.GetProfile(), nil
}

//nolint:gocognit,gocyclo,funlen // The linear form keeps conditional validation and safety gates together.
func (w *Wizard) collect(ctx context.Context, paths Paths, input io.Reader, output io.Writer, force bool) (Answers, error) {
	backends, err := w.Backends(ctx)
	if err != nil {
		return Answers{}, err
	}
	if len(backends) == 0 {
		return Answers{}, fmt.Errorf("setup: no backends are available")
	}
	existing, err := w.client.ListProfiles(ctx, &controlv1.ListProfilesRequest{})
	if err != nil {
		return Answers{}, err
	}
	names := make(map[string]bool, len(existing.GetProfiles()))
	for _, profile := range existing.GetProfiles() {
		names[profile.GetName()] = true
	}

	answers := defaultAnswers(paths, backends[0].GetName())
	port := strconv.Itoa(answers.Port)
	startup := "both"
	proceed := true
	form := huh.NewForm(
		huh.NewGroup(huh.NewNote().
			Title(config.ProjectDisplayName+" setup").
			Description("This will create:\n  "+paths.Config+"\n  "+paths.ProfilesDir+"/"+answers.ProfileName+"/profile.yaml")),
		huh.NewGroup(
			huh.NewInput().Title("Public web listen address").Value(&answers.ListenAddr).Validate(validateListen),
			huh.NewInput().Title("Bearer token environment variable").Value(&answers.AuthTokenEnv),
			huh.NewInput().Title("Model storage directory").Value(&answers.ModelStorageDir).Validate(validateStorageDir),
			huh.NewSelect[string]().Title("Start automatically").Options(
				huh.NewOption("control + web", "both"),
				huh.NewOption("control", "control"),
				huh.NewOption("web", "web"),
			).Value(&startup),
		),
		huh.NewGroup(
			huh.NewInput().Title("Profile name").Value(&answers.ProfileName).Validate(func(value string) error {
				value = strings.TrimSpace(value)
				if err := profiles.ValidateName(value); err != nil {
					return err
				}
				if !force && names[value] {
					return fmt.Errorf("profile %q already exists; choose another name", value)
				}
				return nil
			}),
			huh.NewSelect[string]().Title("Backend").Options(backendOptions(backends)...).Value(&answers.Backend),
			huh.NewInput().Title("Model source").Description("Repository, URL, or local path").Value(&answers.ModelSource).Validate(required("model source")),
		),
		huh.NewGroup(
			huh.NewInput().Title("Model reference").Description("Optional artifact filename within the repository").Value(&answers.ModelReference),
		).WithHideFunc(func() bool {
			selected := backendByName(backends, answers.Backend)
			return selected == nil || !selected.GetCapabilities().GetSingleFileArtifacts()
		}),
		huh.NewGroup(
			huh.NewInput().Title("Runtime listen host").Value(&answers.Host).Validate(required("runtime listen host")),
			huh.NewInput().Title("Runtime listen port").Value(&port).Validate(validatePort),
			huh.NewConfirm().Title("Autostart this profile?").Value(&answers.Autostart),
		),
		huh.NewGroup(
			huh.NewNote().Title("Review").DescriptionFunc(func() string {
				return reviewSummary(paths, answers, port, startup, names[answers.ProfileName] && force)
			}, []any{&answers, &port, &startup}),
			huh.NewConfirm().Title("Write configuration and profile?").Value(&proceed),
		),
	).WithTheme(huh.ThemeFunc(tealStyles)).WithInput(input).WithOutput(output)

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
	answers.ProfileName = strings.TrimSpace(answers.ProfileName)
	answers.ModelSource = strings.TrimSpace(answers.ModelSource)
	answers.ModelReference = strings.TrimSpace(answers.ModelReference)
	answers.Host = strings.TrimSpace(answers.Host)
	answers.Port, _ = strconv.Atoi(strings.TrimSpace(port))
	answers.StartupServices = startupServices(startup)
	selected := backendByName(backends, answers.Backend)
	if selected == nil {
		return Answers{}, fmt.Errorf("setup: backend %q is not available", answers.Backend)
	}
	if !selected.GetCapabilities().GetSingleFileArtifacts() {
		answers.ModelReference = ""
	}
	if err := w.ensureBackend(ctx, input, output, selected); err != nil {
		return Answers{}, err
	}
	if (&config.Config{ListenAddr: answers.ListenAddr}).AllowsNonLoopback() &&
		os.Getenv(answers.AuthTokenEnv) == "" {
		if err := requireConfirm(ctx, input, output, "Remote-capable bind without a populated token environment — continue anyway?"); err != nil {
			return Answers{}, err
		}
	}
	return answers, nil
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
		Value(&install))).WithTheme(huh.ThemeFunc(tealStyles)).WithInput(input).WithOutput(output)
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
		WithTheme(huh.ThemeFunc(tealStyles)).WithInput(input).WithOutput(output)
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

func required(label string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", label)
		}
		return nil
	}
}

func validateListen(value string) error {
	if _, _, err := net.SplitHostPort(strings.TrimSpace(value)); err != nil {
		return fmt.Errorf("listen address must be host:port: %w", err)
	}
	return nil
}

func validatePort(value string) error {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port must be 1-65535")
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

func reviewSummary(paths Paths, answers Answers, port, startup string, replacing bool) string {
	replacement := ""
	if replacing {
		replacement = "\nExisting profile:  replace with backup"
	}
	return config.ProjectDisplayName + " home: " + paths.Home + "\n" +
		"Config file:       " + paths.Config + "\n" +
		"Model storage:     " + answers.ModelStorageDir + "\n" +
		"Public address:    " + answers.ListenAddr + "\n" +
		"Startup services:  " + strings.Join(startupServices(startup), ", ") + "\n" +
		"Profile:           " + answers.ProfileName + " (" + answers.Backend + ")\n" +
		"Model source:      " + answers.ModelSource + "\n" +
		"Runtime address:   " + net.JoinHostPort(answers.Host, port) + replacement
}

func tealStyles(isDark bool) *huh.Styles {
	teal := lipgloss.Color("14")
	styles := huh.ThemeCharm(isDark)
	styles.Focused.SelectSelector = styles.Focused.SelectSelector.Foreground(teal)
	styles.Focused.NextIndicator = styles.Focused.NextIndicator.Foreground(teal)
	styles.Focused.PrevIndicator = styles.Focused.PrevIndicator.Foreground(teal)
	styles.Focused.MultiSelectSelector = styles.Focused.MultiSelectSelector.Foreground(teal)
	styles.Focused.FocusedButton = styles.Focused.FocusedButton.Background(teal)
	styles.Focused.Next = styles.Focused.FocusedButton
	styles.Focused.TextInput.Prompt = styles.Focused.TextInput.Prompt.Foreground(teal)
	styles.Blurred = styles.Focused
	styles.Blurred.Base = styles.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	styles.Blurred.Card = styles.Blurred.Base
	styles.Blurred.NextIndicator = lipgloss.NewStyle()
	styles.Blurred.PrevIndicator = lipgloss.NewStyle()
	return styles
}
