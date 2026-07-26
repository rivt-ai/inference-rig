package tui

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/antonikliment/tuikit"

	"inferencerig/config"
	"inferencerig/platform/audit"
)

type systemPage struct{ vp viewport.Model }

func (p *systemPage) Update(msg tea.KeyPressMsg) {
	if msg.String() == "up" {
		p.vp.ScrollUp(1)
	} else if msg.String() == "down" {
		p.vp.ScrollDown(1)
	}
}

func (p *systemPage) View(width, height int, data snapshot) string {
	signals := data.signals.GetSignals()
	resourceRows := []string{theme.StatusTitle("System Resources", "Live", cyan, green, width)}
	if signals == nil {
		resourceRows = append(resourceRows, warningStyle.Render("Control telemetry unavailable"))
	} else {
		used := signals.GetTotalMemoryBytes() - min(signals.GetTotalMemoryBytes(), signals.GetAvailableMemoryBytes())
		percent := 0
		if signals.GetTotalMemoryBytes() > 0 {
			percent = int(math.Round(float64(used) * 100 / float64(signals.GetTotalMemoryBytes())))
		}
		resourceRows = append(resourceRows,
			resourceRow("CPU", int(math.Round(signals.GetCpuUsedPercent())), fmt.Sprintf("%d logical cores", signals.GetLogicalCpuCores())),
			resourceRow("RAM", percent, fmt.Sprintf("(%s/%s)", tuikit.FormatBytes(int64(used)), tuikit.FormatBytes(int64(signals.GetTotalMemoryBytes())))),
		)
		for _, warning := range signals.GetWarnings() {
			resourceRows = append(resourceRows, warningStyle.Render(warning))
		}
	}
	build := data.info.GetBuild()
	controlRows := []string{
		theme.StatusTitle("Control Plane", build.GetVersion(), blue, green, width),
		tuikit.Field("Profiles", fmt.Sprint(data.info.GetProfiles())),
		tuikit.Field("Backends", fmt.Sprint(data.info.GetBackends())),
		tuikit.Field("Running", fmt.Sprint(len(data.info.GetRunningProfiles()))),
		tuikit.Field("Commit", build.GetCommit()),
	}
	content := tuikit.Flow(width, 2, []string{
		panel(cyan, false, tuikit.AdaptiveWidth(width, 2, 36, 70), 10, lipgloss.JoinVertical(lipgloss.Left, resourceRows...)),
		panel(blue, false, tuikit.AdaptiveWidth(width, 2, 36, 70), 10, lipgloss.JoinVertical(lipgloss.Left, controlRows...)),
	})
	p.vp.SetWidth(width)
	p.vp.SetHeight(height)
	p.vp.SetContent(content)
	return p.vp.View()
}

var meter = tuikit.NewMeter(20, green)

func resourceRow(label string, percent int, detail string) string {
	return fmt.Sprintf("%-8s %s %3d%%  %s", label+":", meter.View(percent), percent, mutedStyle.Render(detail))
}

type activityPage struct {
	active int
	views  [3]tuikit.SearchView
}

func newActivityPage() activityPage {
	return activityPage{views: [3]tuikit.SearchView{tuikit.NewSearchView(), tuikit.NewSearchView(), tuikit.NewSearchView()}}
}

func (p *activityPage) CapturingInput() bool { return p.views[p.active].Searching() }

func (p *activityPage) Update(msg tea.KeyPressMsg) {
	if !p.CapturingInput() {
		if msg.String() == "tab" {
			p.active = (p.active + 1) % len(p.views)
			return
		}
		if msg.String() == "shift+tab" {
			p.active = (p.active + len(p.views) - 1) % len(p.views)
			return
		}
	}
	p.views[p.active].Update(msg)
}

func (p *activityPage) View(width, height int, data snapshot) string {
	events := make([]string, 0, len(data.events.GetEvents()))
	for _, event := range data.events.GetEvents() {
		style := successStyle
		if !event.GetSuccess() {
			style = errorStyle
		}
		events = append(events, mutedStyle.Render(event.GetTime())+"  "+style.Render(event.GetAction())+"  "+event.GetDuration())
	}
	p.views[0].SetLines(events)
	p.views[1].SetLines(colorLog(data.controlLog))
	p.views[2].SetLines(colorLog(data.webLog))
	titles := []string{fmt.Sprintf("Events (%d)", len(events)), fmt.Sprintf("Control + Runtime (%d)", len(data.controlLog)), fmt.Sprintf("Web (%d)", len(data.webLog))}
	accents := []color.Color{cyan, green, blue}
	bodyHeight := max(1, height-7)
	body := p.views[p.active].View(width-4, bodyHeight)
	tabbed := theme.TabbedPanel(titles, accents, p.active, width, height-3, body)
	status := tuikit.HelpLine(
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "Pane")),
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "Scroll")),
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "Search")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("Esc", "Clear")),
	)
	if p.CapturingInput() {
		status = mutedStyle.Render("Search: " + p.views[p.active].InputView() + "  (Enter/Esc to finish)")
	} else if query := p.views[p.active].Query(); query != "" {
		status = mutedStyle.Render("Search: " + query + "  (Esc to clear)")
	}
	return lipgloss.JoinVertical(lipgloss.Left, tabbed, panel(muted, false, width, 2, status))
}

func readLogs() ([]string, []string) {
	read := func(name string) []string {
		text, err := audit.TailLogLines(name, 2000)
		if err != nil {
			return nil
		}
		lines := strings.Split(strings.TrimSpace(text), "\n")
		if len(lines) == 1 && lines[0] == "" {
			return nil
		}
		return lines
	}
	return read(config.ProjectName), read(config.StartupServiceWeb)
}

func colorLog(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		switch {
		case strings.Contains(line, "level=ERROR"), strings.Contains(line, " level=error "):
			out = append(out, errorStyle.Render(line))
		case strings.Contains(line, "level=WARN"), strings.Contains(line, " level=warn "):
			out = append(out, warningStyle.Render(line))
		default:
			out = append(out, mutedStyle.Render(line))
		}
	}
	return out
}
