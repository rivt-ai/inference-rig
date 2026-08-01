package tui

import (
	"cmp"
	"fmt"
	"hash/crc32"
	"image/color"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/antonikliment/tuikit"

	controlv1 "inferencerig/core/rpc/gen/v1"
)

type modelPane int

const (
	paneProfiles modelPane = iota
	paneCatalog
	paneLocal
	paneDownloads
	modelPanes
)

type modelsPage struct {
	active                              modelPane
	backendIndex                        int
	profiles, catalog, local, downloads table.Model
	status                              tuikit.Status
	search                              tuikit.SearchView
}

func newModelsPage() modelsPage {
	styles := table.DefaultStyles()
	styles.Selected = selectedStyle
	makeTable := func(columns ...table.Column) table.Model {
		return table.New(table.WithColumns(columns), table.WithStyles(styles))
	}
	return modelsPage{
		profiles:  makeTable(table.Column{Title: "Profile", Width: 18}, table.Column{Title: "Backend", Width: 14}, table.Column{Title: "Model", Width: 42}, table.Column{Title: "State", Width: 10}),
		catalog:   makeTable(table.Column{Title: "Catalog model", Width: 52}, table.Column{Title: "Downloads", Width: 12}, table.Column{Title: "Variants", Width: 10}),
		local:     makeTable(table.Column{Title: "Local model", Width: 48}, table.Column{Title: "Size", Width: 12}, table.Column{Title: "Modified", Width: 22}),
		downloads: makeTable(table.Column{Title: "Profile", Width: 18}, table.Column{Title: "State", Width: 14}, table.Column{Title: "Progress", Width: 12}, table.Column{Title: "Target", Width: 38}),
		search:    tuikit.NewSearchView(),
	}
}

func (p *modelsPage) backend(backends *controlv1.ListBackendsResponse) string {
	items := backends.GetBackends()
	if len(items) == 0 {
		return ""
	}
	return items[p.backendIndex%len(items)].GetName()
}

func (p *modelsPage) CapturingInput() bool { return p.active == paneCatalog && p.search.Searching() }

func (p *modelsPage) Update(msg tea.KeyPressMsg, data snapshot) tea.Cmd {
	if p.CapturingInput() {
		p.search.Update(msg)
		return nil
	}
	if msg.String() == "n" {
		return p.createProfile(data)
	}
	if msg.String() == "R" {
		return p.restart(data)
	}
	if command, handled := p.navigate(msg, data); handled {
		return command
	}
	return p.action(msg, data)
}

func (p *modelsPage) navigate(msg tea.KeyPressMsg, data snapshot) (tea.Cmd, bool) {
	switch msg.String() {
	case "tab":
		p.active = (p.active + 1) % modelPanes
		p.disarm()
	case "shift+tab":
		p.active = (p.active + modelPanes - 1) % modelPanes
		p.disarm()
	case "[":
		p.moveBackend(-1, data.backends)
		return func() tea.Msg { return refreshMsg{} }, true
	case "]":
		p.moveBackend(1, data.backends)
		return func() tea.Msg { return refreshMsg{} }, true
	case "up":
		p.move(-1)
	case "down":
		p.move(1)
	case "/":
		if p.active == paneCatalog {
			p.search.Update(msg)
		}
	case "esc":
		p.disarm()
		p.search.Update(msg)
	default:
		return nil, false
	}
	return nil, true
}

func (p *modelsPage) action(msg tea.KeyPressMsg, data snapshot) tea.Cmd {
	switch msg.String() {
	case "enter":
		return p.enter(data)
	case "g":
		if profile := selectedItem(data.profiles.GetProfiles(), p.profiles.Cursor()); profile != nil {
			return requestCmd(rpcRequest{kind: rpcDownload, profile: profile.GetName(), notice: "download started"})
		}
	case "c":
		if item := selectedItem(data.downloads, p.downloads.Cursor()); item != nil && (item.GetState() == "queued" || item.GetState() == "running") {
			return requestCmd(rpcRequest{kind: rpcCancel, id: item.GetId(), notice: "download cancelled"})
		}
	case "i":
		if backend := p.installableBackend(data.backends); backend != "" {
			return requestCmd(rpcRequest{kind: rpcInstall, backend: backend, notice: "backend installed"})
		}
	case "d", "y":
		return p.destroy(data, msg.String() == "y")
	}
	return nil
}

func (p *modelsPage) installableBackend(backends *controlv1.ListBackendsResponse) string {
	name := p.backend(backends)
	for _, backend := range backends.GetBackends() {
		if backend.GetName() == name && backend.GetCapabilities().GetManagedInstall() {
			return name
		}
	}
	return ""
}

func (p *modelsPage) moveBackend(delta int, backends *controlv1.ListBackendsResponse) {
	if count := len(backends.GetBackends()); count > 0 {
		p.backendIndex = (p.backendIndex + count + delta) % count
	}
}

func (p *modelsPage) move(delta int) {
	current := p.table()
	if delta < 0 {
		current.MoveUp(1)
	} else {
		current.MoveDown(1)
	}
	p.disarm()
}

func (p *modelsPage) table() *table.Model {
	return []*table.Model{&p.profiles, &p.catalog, &p.local, &p.downloads}[p.active]
}

func (p *modelsPage) enter(data snapshot) tea.Cmd {
	switch p.active {
	case paneProfiles:
		profile := selectedItem(data.profiles.GetProfiles(), p.profiles.Cursor())
		if profile != nil && !slices.Contains(data.info.GetRunningProfiles(), profile.GetName()) {
			if reason := backendConflictReason(data.info, profile); reason != "" {
				return p.status.Confirm(reason, false, func() tea.Cmd {
					return requestCmd(rpcRequest{kind: rpcReset, notice: "runtimes reset"})
				})
			}
			if startNeedsReplace(data, profile) {
				return p.status.Confirm("Starting "+profile.GetName()+" will stop the running profile.", false, func() tea.Cmd {
					return requestCmd(rpcRequest{kind: rpcStart, profile: profile.GetName(), replace: true, notice: "runtime started"})
				})
			}
			return requestCmd(rpcRequest{kind: rpcStart, profile: profile.GetName(), notice: "runtime started"})
		}
	case paneDownloads:
		item := selectedItem(data.downloads, p.downloads.Cursor())
		if item != nil && (item.GetState() == "completed" || item.GetState() == "already-downloaded") {
			return requestCmd(rpcRequest{kind: rpcApply, profile: item.GetProfile(), id: item.GetId(), notice: "download applied"})
		}
	}
	return nil
}

func (p *modelsPage) destroy(data snapshot, confirm bool) tea.Cmd {
	switch p.active {
	case paneProfiles:
		profile := selectedItem(data.profiles.GetProfiles(), p.profiles.Cursor())
		if profile == nil {
			return nil
		}
		return p.status.Confirm("Clean up "+profile.GetName()+" and its unshared artifacts?", confirm, func() tea.Cmd {
			return requestCmd(rpcRequest{kind: rpcCleanup, profile: profile.GetName(), notice: "profile cleaned up"})
		})
	case paneLocal:
		item := selectedItem(data.local.GetModels(), p.local.Cursor())
		if item == nil {
			return nil
		}
		return p.status.Confirm("Delete "+item.GetPath()+"?", confirm, func() tea.Cmd {
			return requestCmd(rpcRequest{kind: rpcDelete, backend: p.backend(data.backends), path: item.GetPath(), notice: "local model deleted"})
		})
	}
	return nil
}

func requestCmd(request rpcRequest) tea.Cmd { return func() tea.Msg { return request } }

func (p *modelsPage) createProfile(data snapshot) tea.Cmd {
	model := selectedItem(data.local.GetModels(), p.local.Cursor())
	if p.active != paneLocal || model == nil {
		return nil
	}
	path := model.GetPath()
	profile := &controlv1.Profile{Name: fmt.Sprintf("model-%08x", crc32.ChecksumIEEE([]byte(path))), Backend: p.backend(data.backends), ModelSource: path, Host: "127.0.0.1", Port: nextProfilePort(data.profiles.GetProfiles())}
	return requestCmd(rpcRequest{kind: rpcPutProfile, create: profile, notice: "profile created"})
}

func (p *modelsPage) restart(data snapshot) tea.Cmd {
	profile := selectedItem(data.profiles.GetProfiles(), p.profiles.Cursor())
	if p.active != paneProfiles || profile == nil || !slices.Contains(data.info.GetRunningProfiles(), profile.GetName()) {
		return nil
	}
	return p.status.Confirm("Restart "+profile.GetName()+"?", false, func() tea.Cmd {
		return func() tea.Msg { return serviceRequest{panel: panelRuntime, profile: profile.GetName(), restart: true} }
	})
}

func (p *modelsPage) disarm() {
	p.status.Disarm()
}

func (p *modelsPage) complete(msg actionMsg) {
	if msg.err != nil || msg.notice != "" {
		p.status.SetResult(msg.err, msg.notice)
	}
}

func (p *modelsPage) View(width, height int, data snapshot) string {
	p.setRows(data)
	titles := []string{
		fmt.Sprintf("Profiles (%d)", len(data.profiles.GetProfiles())),
		catalogTitle(data),
		fmt.Sprintf("Local (%d)", len(data.local.GetModels())),
		fmt.Sprintf("Downloads (%d)", len(data.downloads)),
	}
	accents := []color.Color{cyan, blue, green, yellow}
	helpHeight, detailHeight := 3, max(4, height/3)
	tabHeight := max(6, height-detailHeight-helpHeight)
	current := p.table()
	current.SetWidth(width - 4)
	current.SetHeight(max(1, tabHeight-5))
	current.Focus()
	body := current.View()
	if p.active == paneCatalog {
		lines := make([]string, len(data.catalog.GetModels()))
		for i, item := range data.catalog.GetModels() {
			lines[i] = strings.Join([]string{item.GetId(), fmt.Sprint(item.GetDownloads()), fmt.Sprint(len(item.GetVariants()))}, "  ")
		}
		p.search.SetLines(lines)
		body = p.search.View(width-4, max(1, tabHeight-4))
	}
	body = p.statusRows(body, data)
	tabbed := theme.TabbedPanel(titles, accents, int(p.active), width, tabHeight, body)
	detail := p.detail(width, detailHeight, data)
	help := tuikit.HelpLine(
		key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "Navigate")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "Pane")),
		key.NewBinding(key.WithKeys("[", "]"), key.WithHelp("[/]", "Backend")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "Run/apply")),
		key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "Profile from local")),
		key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "Restart")),
		key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "Download")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "Delete")),
	)
	return tuikit.VerticalSlice(lipgloss.JoinVertical(lipgloss.Left, tabbed, detail, panel(muted, false, width, 2, help)), 0, height)
}

// catalogTitle reports a cold cache as refreshing rather than as an empty
// catalog. The first request for a query returns nothing while the remote fetch
// runs behind it, which is indistinguishable from "this backend has no models"
// unless the tab says so.
func catalogTitle(data snapshot) string {
	models := len(data.catalog.GetModels())
	if models == 0 && (data.catalogPending || data.catalog.GetCache().GetRefreshing()) {
		return "Catalog (refreshing…)"
	}
	return fmt.Sprintf("Catalog (%d)", models)
}

// profileModel describes what a profile serves. The reference is optional — it
// names a file inside a repository — while the source is what every profile
// carries, so reading only the reference left the column blank for every
// profile that points straight at a model.
func (p *modelsPage) setRows(data snapshot) {
	profiles := make([]table.Row, 0, len(data.profiles.GetProfiles()))
	for _, item := range data.profiles.GetProfiles() {
		row := table.Row{item.GetName(), item.GetBackend(), cmp.Or(item.GetModelReference(), item.GetModelSource()), profileRuntimeState(data.runtimes, item.GetName())}
		if backendConflictReason(data.info, item) != "" {
			for i := range row {
				row[i] = mutedStyle.Render(row[i])
			}
		}
		profiles = append(profiles, row)
	}
	p.profiles.SetRows(profiles)
	catalog := make([]table.Row, 0, len(data.catalog.GetModels()))
	for _, item := range data.catalog.GetModels() {
		catalog = append(catalog, table.Row{item.GetId(), fmt.Sprint(item.GetDownloads()), fmt.Sprint(len(item.GetVariants()))})
	}
	p.catalog.SetRows(catalog)
	local := make([]table.Row, 0, len(data.local.GetModels()))
	for _, item := range data.local.GetModels() {
		local = append(local, table.Row{item.GetFilename(), tuikit.FormatBytes(item.GetSizeBytes()), item.GetModifiedAt()})
	}
	p.local.SetRows(local)
	downloads := make([]table.Row, 0, len(data.downloads))
	for _, item := range data.downloads {
		downloads = append(downloads, table.Row{item.GetProfile(), item.GetState(), fmt.Sprintf("%.1f%%", item.GetPercent()), item.GetTargetPath()})
	}
	p.downloads.SetRows(downloads)
}

func (p *modelsPage) statusRows(body string, data snapshot) string {
	rows := []string{body}
	if p.active == paneProfiles || p.active == paneLocal {
		pending, keyName := p.status.Pending(), "Enter"
		if strings.HasPrefix(pending, "Restart ") {
			keyName = "R"
		} else if strings.HasPrefix(pending, "Clean up ") || strings.HasPrefix(pending, "Delete ") {
			keyName = "d or y"
		}
		if pending != "" {
			rows = append(rows, warningStyle.Render(pending+" Press "+keyName+" again to confirm, Esc to cancel"))
		} else {
			rows = p.status.AppendRows(theme, rows)
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (p *modelsPage) detail(width, height int, data snapshot) string {
	accent := []color.Color{cyan, blue, green, yellow}[p.active]
	rows := []string{theme.StatusTitle([]string{"Profile", "Catalog", "Local model", "Download"}[p.active], p.backend(data.backends), accent, muted, width)}
	switch p.active {
	case paneProfiles:
		if item := selectedItem(data.profiles.GetProfiles(), p.profiles.Cursor()); item != nil {
			state := profileRuntimeState(data.runtimes, item.GetName())
			rows = []string{theme.StatusTitle(item.GetName(), state, accent, green, width), tuikit.Field("Backend", item.GetBackend()), tuikit.Field("Model", tuikit.TruncMiddle(cmp.Or(item.GetModelReference(), item.GetModelSource()), width-12)), tuikit.Field("Listen", fmt.Sprintf("%s:%d", item.GetHost(), item.GetPort()))}
			if reason := backendConflictReason(data.info, item); reason != "" {
				rows = append(rows, warningStyle.Render(reason))
			}
		}
	case paneCatalog:
		rows = append(rows, mutedStyle.Render("/: search catalog · i: install selected backend"))
	case paneLocal:
		if item := selectedItem(data.local.GetModels(), p.local.Cursor()); item != nil {
			rows = []string{theme.StatusTitle(item.GetFilename(), tuikit.FormatBytes(item.GetSizeBytes()), accent, green, width), tuikit.Field("Path", tuikit.TruncMiddle(item.GetPath(), width-12))}
		}
	case paneDownloads:
		if item := selectedItem(data.downloads, p.downloads.Cursor()); item != nil {
			rows = []string{theme.StatusTitle(item.GetProfile(), item.GetState(), accent, yellow, width), tuikit.Field("Target", tuikit.TruncMiddle(item.GetTargetPath(), width-12)), tuikit.Field("Progress", fmt.Sprintf("%.1f%%", item.GetPercent())), tuikit.Field("Error", item.GetError())}
		}
	}
	return panel(accent, false, width, height, lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func backendConflictReason(info *controlv1.GetInfoResponse, profile *controlv1.Profile) string {
	if active := info.GetActiveBackend(); profile != nil && active != "" && active != profile.GetBackend() {
		return fmt.Sprintf("%s is active — reset to start %s profiles", active, profile.GetBackend())
	}
	return ""
}

func startNeedsReplace(data snapshot, target *controlv1.Profile) bool {
	if len(data.info.GetRunningProfiles()) == 0 {
		return false
	}
	for _, backend := range data.backends.GetBackends() {
		if backend.GetName() == target.GetBackend() && backend.GetCapabilities().GetSingleActiveProfile() {
			return true
		}
	}
	for _, profile := range data.profiles.GetProfiles() {
		if slices.Contains(data.info.GetRunningProfiles(), profile.GetName()) && (profile.GetHost() != target.GetHost() || profile.GetPort() != target.GetPort()) {
			return true
		}
	}
	return false
}

func profileRuntimeState(runtimes *controlv1.GetRuntimeStatusResponse, name string) string {
	for _, profile := range runtimes.GetProfiles() {
		if profile.GetName() == name && profile.GetStatus().GetState() != "" {
			state := profile.GetStatus().GetState()
			return strings.ToUpper(state[:1]) + state[1:]
		}
	}
	return "Stopped"
}

func nextProfilePort(items []*controlv1.Profile) int32 {
	for port := int32(8080); port <= 65535; port++ {
		if !slices.ContainsFunc(items, func(item *controlv1.Profile) bool { return item.GetPort() == port }) {
			return port
		}
	}
	return 8080
}

func selectedItem[T any](items []T, index int) T { item, _ := selected(items, index); return item }
