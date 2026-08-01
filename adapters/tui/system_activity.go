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

	controlv1 "inferencerig/core/rpc/gen/v1"
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
		total := signals.GetTotalMemoryBytes()
		used := total - min(total, signals.GetAvailableMemoryBytes())
		resourceRows = append(resourceRows,
			resourceRow("CPU", int(math.Round(signals.GetCpuUsedPercent())), fmt.Sprintf("%d logical cores", signals.GetLogicalCpuCores())),
			resourceRow("RAM", usedPercent(used, total), bytePair(used, total)),
		)
		resourceRows = append(resourceRows, acceleratorRows(signals.GetAccelerators())...)
		resourceRows = append(resourceRows, diskRows(signals.GetDisks(), modelsBytes(data.local.GetModels()))...)
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
	// Grow both panels together so added GPU/disk rows stay inside the border.
	panelHeight := max(10, len(resourceRows)+2, len(controlRows)+2)
	content := tuikit.Flow(width, 2, []string{
		panel(cyan, false, tuikit.AdaptiveWidth(width, 2, 36, 70), panelHeight, lipgloss.JoinVertical(lipgloss.Left, resourceRows...)),
		panel(blue, false, tuikit.AdaptiveWidth(width, 2, 36, 70), panelHeight, lipgloss.JoinVertical(lipgloss.Left, controlRows...)),
	})
	p.vp.SetWidth(width)
	p.vp.SetHeight(height)
	p.vp.SetContent(content)
	return p.vp.View()
}

func modelsBytes(models []*controlv1.LocalModel) uint64 {
	var total int64
	for _, model := range models {
		total += model.GetSizeBytes()
	}
	return uint64(total)
}

var meter = tuikit.NewMeter(20, green)

func resourceRow(label string, percent int, detail string) string {
	return fmt.Sprintf("%-8s %s %3d%%  %s", label+":", meter.View(percent), percent, mutedStyle.Render(detail))
}

func usedPercent(used, total uint64) int {
	if total == 0 {
		return 0
	}
	return int(math.Round(float64(used) * 100 / float64(total)))
}

func bytePair(used, total uint64) string {
	if total == 0 {
		return ""
	}
	return fmt.Sprintf("(%s/%s)", tuikit.FormatBytes(int64(used)), tuikit.FormatBytes(int64(total)))
}

// acceleratorRows renders one GPU row per reported device. A unified-memory
// device draws from system RAM, so its meter deliberately tracks the same
// figures as the RAM row and the detail says so; a discrete device shows its
// own VRAM. With no device reported the row stays, marked unavailable, so the
// panel reads the same on a CPU-only host.
func acceleratorRows(accelerators []*controlv1.Accelerator) []string {
	if len(accelerators) == 0 {
		return []string{resourceRow("GPU", 0, "(unavailable)")}
	}
	rows := make([]string, 0, len(accelerators))
	for _, accelerator := range accelerators {
		name := accelerator.GetName()
		if name == "" {
			name = "GPU"
		}
		used, total := accelerator.GetUsedMemoryBytes(), accelerator.GetTotalMemoryBytes()
		// A unified device's bytes are the RAM row's bytes, so name the sharing
		// instead of repeating the figures one line below themselves.
		detail := name + " (unified with RAM)"
		if !accelerator.GetUnifiedMemory() {
			detail = strings.TrimSpace(name + " " + bytePair(used, total))
		}
		rows = append(rows, resourceRow("GPU", usedPercent(used, total), detail))
	}
	return rows
}

// diskRows meters each filesystem, except that the model storage entry reports
// what the models themselves occupy of that filesystem: its telemetry is the
// whole mount point, which on most hosts is just the root row repeated.
func diskRows(disks []*controlv1.Disk, modelsUsed uint64) []string {
	rows := make([]string, 0, len(disks))
	for _, disk := range disks {
		used, percent := disk.GetUsedBytes(), int(math.Round(disk.GetUsedPercent()))
		if disk.GetLabel() == "model_storage" {
			used, percent = modelsUsed, usedPercent(modelsUsed, disk.GetTotalBytes())
		}
		detail := strings.TrimSpace(bytePair(used, disk.GetTotalBytes()) + " " + disk.GetPath())
		rows = append(rows, resourceRow(diskLabel(disk.GetLabel()), percent, detail))
	}
	return rows
}

func diskLabel(label string) string {
	switch label {
	case "model_storage":
		return "Models"
	case "root":
		return "Root"
	case "":
		return "Disk"
	default:
		return label
	}
}

type activityPage struct {
	active int
	views  [4]tuikit.SearchView
}

func newActivityPage() activityPage {
	return activityPage{views: [4]tuikit.SearchView{tuikit.NewSearchView(), tuikit.NewSearchView(), tuikit.NewSearchView(), tuikit.NewSearchView()}}
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
		action := event.GetAction()
		if recovery := event.GetRecovery(); recovery != "" {
			action += " " + recovery
		}
		if profile := event.GetProfile(); profile != "" {
			action += " " + profile
		}
		if detail := event.GetDetail(); detail != "" {
			action += ": " + detail
		}
		events = append(events, mutedStyle.Render(event.GetTime())+"  "+style.Render(action)+"  "+event.GetDuration())
	}
	p.views[0].SetLines(events)
	p.views[1].SetLines(colorLog(data.controlLog))
	// Engine output keeps the backend's own formatting: colorLog matches slog
	// levels, which no engine emits, so styling it would only mislead.
	p.views[2].SetLines(mutedLog(data.engineLog))
	p.views[3].SetLines(colorLog(data.webLog))
	titles := []string{
		fmt.Sprintf("Events (%d)", len(events)),
		fmt.Sprintf("Control (%d)", len(data.controlLog)),
		fmt.Sprintf("Engine (%d)", len(data.engineLog)),
		fmt.Sprintf("Web (%d)", len(data.webLog)),
	}
	accents := []color.Color{cyan, green, yellow, blue}
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

func readLogs() (control, engine, web []string) {
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
	return read(config.LogServiceControl), read(config.LogServiceEngine), read(config.StartupServiceWeb)
}

func mutedLog(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, mutedStyle.Render(line))
	}
	return out
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
