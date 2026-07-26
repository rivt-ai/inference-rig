package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"

	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

const pageCount = 6

type snapshot struct {
	info     *controlv1.GetInfoResponse
	backends *controlv1.ListBackendsResponse
	profiles *controlv1.ListProfilesResponse
	catalog  *controlv1.ListModelCatalogResponse
	local    *controlv1.ListLocalModelsResponse
	runtime  *controlv1.GetRuntimeStatusResponse
	download *controlv1.ModelDownload
	signals  *controlv1.GetSignalsResponse
	events   *controlv1.ListEventsResponse
}

type snapshotMsg struct {
	value snapshot
	err   error
}

type actionMsg struct {
	download *controlv1.ModelDownload
	err      error
}

type model struct {
	ctx      context.Context
	client   controlv1connect.ControlServiceClient
	page     int
	selected int
	data     snapshot
	err      error
}

func newModel(ctx context.Context, client controlv1connect.ControlServiceClient) *model {
	return &model{ctx: ctx, client: client}
}

func (m *model) Init() tea.Cmd { return m.refresh() }

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case snapshotMsg:
		m.data, m.err = message.value, message.err
	case actionMsg:
		m.err = message.err
		if message.download != nil {
			m.data.download = message.download
		}
		return m, m.refresh()
	case tea.KeyPressMsg:
		return m.handleKey(message.String())
	}
	return m, nil
}

func (m *model) handleKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab", "right":
		m.page, m.selected = (m.page+1)%pageCount, 0
		return m, m.refresh()
	case "shift+tab", "left":
		m.page, m.selected = (m.page+pageCount-1)%pageCount, 0
		return m, m.refresh()
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
	case "down", "j":
		if m.selected+1 < len(m.data.profiles.GetProfiles()) {
			m.selected++
		}
	case "r":
		return m, m.refresh()
	case "s", "x", "R", "d", "c", "a", "i":
		return m, m.action(key)
	}
	return m, nil
}

func (m *model) View() tea.View {
	var content strings.Builder
	fmt.Fprintf(&content, "InferenceRig  [%s]\n\n", []string{"overview", "profiles", "models", "runtime", "downloads", "events"}[m.page])
	switch m.page {
	case 0:
		m.viewOverview(&content)
	case 1:
		m.viewProfiles(&content)
	case 2:
		m.viewModels(&content)
	case 3:
		m.viewRuntime(&content)
	case 4:
		m.viewDownloads(&content)
	case 5:
		m.viewEvents(&content)
	}
	if m.err != nil {
		fmt.Fprintf(&content, "\nerror: %v\n", m.err)
	}
	content.WriteString("\n←/→ pages  j/k select  r refresh  s start  x stop  R restart  d download  c cancel  a autostart  i install  q quit\n")
	view := tea.NewView(content.String())
	view.AltScreen = true
	return view
}

func (m *model) refresh() tea.Cmd {
	return func() tea.Msg {
		value, err := loadSnapshot(m.ctx, m.client, m.page, m.selected, m.data.download)
		return snapshotMsg{value: value, err: err}
	}
}

func loadSnapshot(ctx context.Context, client controlv1connect.ControlServiceClient, page, selected int, download *controlv1.ModelDownload) (snapshot, error) {
	value := snapshot{download: download}
	var err error
	if value.info, err = client.GetInfo(ctx, &controlv1.GetInfoRequest{}); err != nil {
		return value, err
	}
	if value.backends, err = client.ListBackends(ctx, &controlv1.ListBackendsRequest{}); err != nil {
		return value, err
	}
	if value.profiles, err = client.ListProfiles(ctx, &controlv1.ListProfilesRequest{}); err != nil {
		return value, err
	}
	profile := selectedProfile(value.profiles, selected)
	backend := selectedBackend(value.backends)
	switch page {
	case 2:
		value.catalog, err = client.ListModelCatalog(ctx, &controlv1.ListModelCatalogRequest{Backend: backend})
		if err == nil {
			value.local, err = client.ListLocalModels(ctx, &controlv1.ListLocalModelsRequest{Backend: backend})
		}
	case 3:
		if profile != "" {
			value.runtime, err = client.GetRuntimeStatus(ctx, &controlv1.GetRuntimeStatusRequest{Profile: profile})
		}
	case 4:
		if download != nil {
			status, statusErr := client.GetModelDownload(ctx, &controlv1.GetModelDownloadRequest{Id: download.GetId()})
			if statusErr == nil {
				value.download = status.GetDownload()
			}
			err = statusErr
		}
	case 5:
		value.events, err = client.ListEvents(ctx, &controlv1.ListEventsRequest{})
	default:
		value.signals, err = client.GetSignals(ctx, &controlv1.GetSignalsRequest{})
	}
	return value, err
}

func (m *model) action(key string) tea.Cmd {
	profile := selectedProfile(m.data.profiles, m.selected)
	backend := selectedBackend(m.data.backends)
	return func() tea.Msg {
		var result actionMsg
		switch key {
		case "s":
			_, result.err = m.client.StartRuntime(m.ctx, &controlv1.StartRuntimeRequest{Profile: profile})
		case "x":
			_, result.err = m.client.StopRuntime(m.ctx, &controlv1.StopRuntimeRequest{Profile: profile})
		case "R":
			_, result.err = m.client.RestartRuntime(m.ctx, &controlv1.RestartRuntimeRequest{Profile: profile})
		case "d":
			var response *controlv1.StartModelDownloadResponse
			response, result.err = m.client.StartModelDownload(m.ctx, &controlv1.StartModelDownloadRequest{Profile: profile})
			result.download = response.GetDownload()
		case "c":
			var response *controlv1.CancelModelDownloadResponse
			response, result.err = m.client.CancelModelDownload(m.ctx, &controlv1.CancelModelDownloadRequest{Id: m.data.download.GetId()})
			result.download = response.GetDownload()
		case "a":
			enabled := !contains(m.data.info.GetAutostartProfiles(), profile)
			_, result.err = m.client.SetProfileAutostart(m.ctx, &controlv1.SetProfileAutostartRequest{Name: profile, Enabled: enabled})
		case "i":
			_, result.err = m.client.InstallBackend(m.ctx, &controlv1.InstallBackendRequest{Backend: backend})
		}
		return result
	}
}

func (m *model) viewOverview(out *strings.Builder) {
	fmt.Fprintf(out, "Backends: %d\nProfiles: %d\nRunning: %s\n", m.data.info.GetBackends(), m.data.info.GetProfiles(), strings.Join(m.data.info.GetRunningProfiles(), ", "))
	if signals := m.data.signals.GetSignals(); signals != nil {
		fmt.Fprintf(out, "Memory available: %d bytes\nCPU: %.1f%%\n", signals.GetAvailableMemoryBytes(), signals.GetCpuUsedPercent())
	}
}

func (m *model) viewProfiles(out *strings.Builder) {
	for index, profile := range m.data.profiles.GetProfiles() {
		marker := " "
		if index == m.selected {
			marker = ">"
		}
		fmt.Fprintf(out, "%s %s  %s  %s\n", marker, profile.GetName(), profile.GetBackend(), profile.GetModelSource())
	}
}

func (m *model) viewModels(out *strings.Builder) {
	for _, item := range m.data.catalog.GetModels() {
		fmt.Fprintf(out, "%s  %d downloads\n", item.GetId(), item.GetDownloads())
	}
	fmt.Fprintln(out, "\nLocal:")
	for _, item := range m.data.local.GetModels() {
		fmt.Fprintf(out, "%s  %d bytes\n", item.GetPath(), item.GetSizeBytes())
	}
}

func (m *model) viewRuntime(out *strings.Builder) {
	fmt.Fprintf(out, "%s: %s\n", selectedProfile(m.data.profiles, m.selected), m.data.runtime.GetStatus().GetState())
}

func (m *model) viewDownloads(out *strings.Builder) {
	if m.data.download == nil {
		out.WriteString("No download started in this session.\n")
		return
	}
	fmt.Fprintf(out, "%s  %s  %.1f%%\n", m.data.download.GetId(), m.data.download.GetState(), m.data.download.GetPercent())
}

func (m *model) viewEvents(out *strings.Builder) {
	for _, event := range m.data.events.GetEvents() {
		fmt.Fprintf(out, "%s  %s  success=%v\n", event.GetTime(), event.GetAction(), event.GetSuccess())
	}
}

func selectedProfile(profiles *controlv1.ListProfilesResponse, index int) string {
	items := profiles.GetProfiles()
	if index < 0 || index >= len(items) {
		return ""
	}
	return items[index].GetName()
}

func selectedBackend(backends *controlv1.ListBackendsResponse) string {
	if len(backends.GetBackends()) == 0 {
		return ""
	}
	return backends.GetBackends()[0].GetName()
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// RunInteractive starts the full-screen canonical control dashboard.
func RunInteractive(ctx context.Context, input io.Reader, output io.Writer, client controlv1connect.ControlServiceClient) error {
	if client == nil {
		return fmt.Errorf("tui: control client is required")
	}
	_, err := tea.NewProgram(newModel(ctx, client), tea.WithContext(ctx), tea.WithInput(input), tea.WithOutput(output)).Run()
	return err
}
