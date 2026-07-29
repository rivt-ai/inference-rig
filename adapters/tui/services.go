package tui

import (
	"context"
	"fmt"
	"image/color"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/antonikliment/tuikit"

	"inferencerig/config"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
	"inferencerig/platform/audit"
	"inferencerig/platform/process"
)

type servicePanel int

const (
	panelControl servicePanel = iota
	panelWeb
	panelRuntime
	servicePanels
)

type servicesPage struct {
	focus    servicePanel
	action   [servicePanels]int
	spin     spinner.Model
	stopping [servicePanels]bool
	vp       viewport.Model
}

func newServicesPage() servicesPage {
	return servicesPage{spin: spinner.New(spinner.WithSpinner(spinner.Ellipsis)), vp: viewport.New()}
}

type serviceRequest struct {
	panel   servicePanel
	action  int
	profile string
}

func (p *servicesPage) Update(msg tea.KeyPressMsg, data snapshot, manage bool) tea.Cmd {
	switch msg.String() {
	case "tab":
		p.focus = (p.focus + 1) % servicePanels
	case "shift+tab":
		p.focus = (p.focus + servicePanels - 1) % servicePanels
	case "left":
		p.move(-1, data)
	case "right":
		p.move(1, data)
	case "up":
		p.vp.ScrollUp(1)
	case "down":
		p.vp.ScrollDown(1)
	case "a":
		if p.focus == panelRuntime {
			profiles := data.info.GetRunningProfiles()
			if profile, ok := selected(profiles, p.action[panelRuntime]); ok {
				enabled := !slices.Contains(data.info.GetAutostartProfiles(), profile)
				return func() tea.Msg {
					return rpcRequest{kind: rpcAutostart, profile: profile, enabled: enabled, notice: "autostart updated"}
				}
			}
		}
	case "enter":
		return p.run(data, manage)
	}
	return nil
}

func (p *servicesPage) run(data snapshot, manage bool) tea.Cmd {
	if p.stopping[p.focus] || (!manage && p.focus != panelRuntime) {
		return nil
	}
	profile := ""
	if p.focus == panelRuntime {
		profile, _ = selected(data.info.GetRunningProfiles(), p.action[panelRuntime])
		if profile == "" {
			return nil
		}
	} else if p.action[p.focus] == 1 {
		p.stopping[p.focus] = true
	}
	request := serviceRequest{panel: p.focus, action: p.action[p.focus], profile: profile}
	return func() tea.Msg { return request }
}

func (p *servicesPage) move(delta int, data snapshot) {
	count := 3
	if p.focus == panelRuntime {
		count = len(data.info.GetRunningProfiles())
	}
	if count > 0 && !p.stopping[p.focus] {
		p.action[p.focus] = (p.action[p.focus] + count + delta) % count
	}
}

func (p *servicesPage) complete() {
	for i := range p.stopping {
		p.stopping[i] = false
	}
}

func (p *servicesPage) advanceSpinner(msg spinner.TickMsg) tea.Cmd {
	for _, stopping := range p.stopping {
		if stopping {
			var command tea.Cmd
			p.spin, command = p.spin.Update(msg)
			return command
		}
	}
	return nil
}

func (p *servicesPage) View(width, height int, data snapshot, manage bool) string {
	const gap = 2
	column := tuikit.AdaptiveWidth(width, gap, 28, 48)
	if width >= 118 {
		column = (width - 2*gap) / 3
	}
	configPath, _ := config.ConfigPath()
	logPath, _ := audit.GetLogPath(config.ProjectName)
	controlStatus, _ := process.StatusDetached(config.ProjectName)
	webStatus, _ := process.StatusDetached(config.StartupServiceWeb)
	// A gateway started outside this TUI has no PID file here, so the address
	// answering is what proves it is up.
	external := !webStatus.Running && data.webReachable
	top := []string{
		serviceBox(column, 10, "Control Daemon", green, controlStatus, p.action[panelControl], p.focus == panelControl, p.stopping[panelControl], p.spin.View(),
			[]string{tuikit.Field("PID", pidText(controlStatus)), tuikit.Field("Uptime", uptimeText(controlStatus)), tuikit.Field("Config", shortPath(configPath)), tuikit.Field("Log", shortPath(logPath))},
			[]string{"Start", "Stop", "Status"}, manage),
		webServiceBox(column, webStatus, external, p, manage,
			[]string{tuikit.Field("Address", listenAddress()), tuikit.Field("Base URL", publicURL(listenAddress())), tuikit.Field("MCP endpoint", "/mcp"), tuikit.Field("Transport", "Streamable HTTP")}),
		runtimeBox(column, 10, data, p.action[panelRuntime], p.focus == panelRuntime),
	}
	help := tuikit.HelpLine(
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab/Shift+Tab", "Focus")),
		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "Select")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "Run")),
		key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "Autostart")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "Refresh")),
	)
	body := lipgloss.JoinVertical(lipgloss.Left, tuikit.Flow(width, gap, top), panel(muted, false, width, 3, help))
	p.vp.SetWidth(width)
	p.vp.SetHeight(height)
	p.vp.SetContent(body)
	return p.vp.View()
}

// webServiceBox renders the gateway box, labelling a gateway this TUI does not
// own as external: its Stop and Start actions act on the PID file, so they
// cannot manage a process started elsewhere.
func webServiceBox(width int, status process.DetachedStatus, external bool, p *servicesPage, manage bool, fields []string) string {
	if external {
		rows := append([]string{theme.StatusTitle("Web Gateway", "Running (external)", blue, green, width)}, fields...)
		rows = append(rows, theme.Rule(width))
		rows = append(rows, mutedStyle.Render("Started outside this TUI; stop it where it was started"))
		return panel(blue, p.focus == panelWeb, width, 10, lipgloss.JoinVertical(lipgloss.Left, rows...))
	}
	return serviceBox(width, 10, "Web Gateway", blue, status, p.action[panelWeb], p.focus == panelWeb,
		p.stopping[panelWeb], p.spin.View(), fields, []string{"Start", "Stop", "Open"}, manage)
}

func serviceBox(width, height int, title string, accent color.Color, status process.DetachedStatus, selectedAction int, focused, stopping bool, spin string, fields, actions []string, enabled bool) string {
	state, stateColor := "Stopped", muted
	if status.Running {
		state, stateColor = "Running", green
	}
	if stopping {
		state, stateColor = "Shutting down"+spin, yellow
	}
	rows := append([]string{theme.StatusTitle(title, state, accent, stateColor, width)}, fields...)
	rows = append(rows, theme.Rule(width))
	switch {
	case !enabled:
		rows = append(rows, mutedStyle.Render("Local controls disabled for custom socket"))
	case stopping:
		rows = append(rows, mutedStyle.Render("Controls locked while stopping"))
	default:
		rows = append(rows, theme.ActionRow(accent, selectedAction, actions, focused))
	}
	return panel(accent, focused, width, height, lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func runtimeBox(width, height int, data snapshot, index int, focused bool) string {
	profiles := data.info.GetRunningProfiles()
	// "running" is the state, not a countable noun: pluralising it read "0
	// runnings" for the most common case on a fresh install.
	rows := []string{theme.StatusTitle("Runtimes", fmt.Sprintf("%d running", len(profiles)), cyan, green, width)}
	if len(profiles) == 0 {
		rows = append(rows, mutedStyle.Render("No running profiles"))
	} else {
		for i, name := range profiles {
			prefix := "  "
			if i == index {
				prefix = "› "
			}
			auto := ""
			if slices.Contains(data.info.GetAutostartProfiles(), name) {
				auto = " [A]"
			}
			rows = append(rows, prefix+name+auto)
		}
	}
	rows = append(rows, theme.Rule(width))
	label := "Stop"
	if name, ok := selected(profiles, index); ok {
		label += " " + name
	}
	rows = append(rows, theme.ActionRow(cyan, 0, []string{label}, focused && len(profiles) > 0))
	return panel(cyan, focused, width, height, lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func runServiceAction(ctx context.Context, client controlv1connect.ControlServiceClient, request serviceRequest) tea.Cmd {
	return func() tea.Msg {
		msg := actionMsg{notice: "action completed"}
		switch request.panel {
		case panelControl:
			msg.err = processAction(config.ProjectName, request.action, []string{"serve"})
		case panelWeb:
			if request.action == 2 {
				msg.err = openBrowser(publicURL(listenAddress()))
			} else {
				msg.err = processAction(config.StartupServiceWeb, request.action, []string{"web"})
				if msg.err == nil {
					msg.err = persistWeb(ctx, client, request.action == 0)
				}
			}
		case panelRuntime:
			_, msg.err = client.StopRuntime(ctx, &controlv1.StopRuntimeRequest{Profile: request.profile})
		}
		return msg
	}
}

func processAction(name string, action int, args []string) error {
	switch action {
	case 0:
		return process.StartDetached(name, args...)
	case 1:
		return process.StopDetached(name)
	default:
		return nil
	}
}

func autostartServices(ctx context.Context, client controlv1connect.ControlServiceClient) tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load()
		if os.IsNotExist(err) {
			cfg, err = config.Default(), nil
		}
		if err != nil {
			return actionMsg{err: err}
		}
		started := []string{}
		for _, name := range cfg.StartupServices {
			args := map[string][]string{config.StartupServiceControl: {"serve"}, config.StartupServiceWeb: {"web"}}[name]
			if status, _ := process.StatusDetached(serviceProcessName(name)); status.Running {
				continue
			}
			if err := process.StartDetached(serviceProcessName(name), args...); err != nil {
				return actionMsg{err: err}
			}
			started = append(started, name)
		}
		notice := ""
		if len(started) > 0 {
			notice = "started " + strings.Join(started, ", ")
		}
		_ = client
		_ = ctx
		return actionMsg{notice: notice}
	}
}

func serviceProcessName(service string) string {
	if service == config.StartupServiceControl {
		return config.ProjectName
	}
	return service
}

func persistWeb(ctx context.Context, client controlv1connect.ControlServiceClient, enabled bool) error {
	info, err := client.GetInfo(ctx, &controlv1.GetInfoRequest{})
	if err != nil {
		return err
	}
	services := slices.DeleteFunc(slices.Clone(info.GetStartupServices()), func(value string) bool { return value == config.StartupServiceWeb })
	if enabled {
		services = append(services, config.StartupServiceWeb)
	}
	_, err = client.SetStartupServices(ctx, &controlv1.SetStartupServicesRequest{Services: services})
	return err
}

func listenAddress() string {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}
	return cfg.ListenAddr
}

func publicURL(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	if port == "" {
		return "http://" + host
	}
	return "http://" + net.JoinHostPort(host, port)
}

func openBrowser(target string) error {
	command := map[string][]string{
		"darwin":  {"open", target},
		"windows": {"rundll32", "url.dll,FileProtocolHandler", target},
	}[runtime.GOOS]
	if command == nil {
		command = []string{"xdg-open", target}
	}
	cmd := exec.Command(command[0], command[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func pidText(status process.DetachedStatus) string {
	if !status.Running {
		return "-"
	}
	return fmt.Sprint(status.PID)
}
func uptimeText(status process.DetachedStatus) string {
	if !status.Running {
		return "-"
	}
	return status.Uptime.String()
}
func shortPath(path string) string {
	home, err := os.UserHomeDir()
	if err == nil && strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return tuikit.TruncMiddle(path, 38)
}
