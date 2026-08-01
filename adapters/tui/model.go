package tui

import (
	"context"
	"maps"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/antonikliment/tuikit"

	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
)

const (
	pageCount       = 4
	refreshInterval = 5 * time.Second
	// catalogTimeout covers a cold remote catalog: the backend lists a search
	// page and then every repository's files before it can report variants.
	catalogTimeout = 60 * time.Second
)

type snapshot struct {
	info                          *controlv1.GetInfoResponse
	runtimes                      *controlv1.GetRuntimeStatusResponse
	backends                      *controlv1.ListBackendsResponse
	profiles                      *controlv1.ListProfilesResponse
	catalog                       *controlv1.ListModelCatalogResponse
	local                         *controlv1.ListLocalModelsResponse
	signals                       *controlv1.GetSignalsResponse
	events                        *controlv1.ListEventsResponse
	downloads                     []*controlv1.ModelDownload
	controlLog, engineLog, webLog []string
	// webReachable reports that the configured gateway address answers, which
	// is how a gateway this TUI did not start is detected. The PID file only
	// records gateways started here, so trusting it alone reports a serving
	// gateway as stopped.
	webReachable bool
	// catalogPending marks a catalog fetch that did not finish inside the poll
	// timeout. A cold cache takes several seconds to fill from the remote, far
	// longer than one poll, and the empty table it leaves behind is otherwise
	// indistinguishable from a backend with no models at all.
	catalogPending bool
	warnings       map[string]string
	refreshed      time.Time
}

type pollResult struct {
	value snapshot
	ok    map[string]bool
}

// catalogMsg carries the model catalog, which is fetched on its own command
// rather than in the poll. A cold remote catalog takes far longer than the
// poll's responsiveness budget, and cancelling it every poll meant the fetch
// restarted forever and the table never filled.
type catalogMsg struct {
	value *controlv1.ListModelCatalogResponse
	err   error
}

type tickMsg struct{}
type refreshMsg struct{}
type actionMsg struct {
	download *controlv1.ModelDownload
	notice   string
	err      error
}

type dashboard struct {
	ctx        context.Context
	client     controlv1connect.ControlServiceClient
	manage     bool
	active     int
	data       snapshot
	services   servicesPage
	models     modelsPage
	system     systemPage
	activity   activityPage
	refreshing bool
	// catalogInFlight keeps one slow catalog request outstanding at a time, so
	// the five-second tick cannot pile them up.
	catalogInFlight bool
	notice          string
}

type model struct {
	frame *tuikit.Frame
	app   *dashboard
}

func newModel(ctx context.Context, client controlv1connect.ControlServiceClient, manage bool) *model {
	app := &dashboard{
		ctx: ctx, client: client, manage: manage,
		data:     snapshot{warnings: map[string]string{}},
		services: newServicesPage(),
		models:   newModelsPage(),
		activity: newActivityPage(),
	}
	pages := []tuikit.Page{
		&page{app: app, index: 0, title: "Services"},
		&page{app: app, index: 1, title: "Models"},
		&page{app: app, index: 2, title: "System"},
		&page{app: app, index: 3, title: "Activity"},
	}
	frame := tuikit.New(
		tuikit.WithTheme(theme),
		tuikit.WithBrand("InferenceRig", "Local inference control plane"),
		tuikit.WithPages(pages...),
		tuikit.WithStatus(app.status),
	)
	return &model{frame: frame, app: app}
}

func (m *model) Init() tea.Cmd {
	commands := []tea.Cmd{m.app.refresh(), tick()}
	if m.app.manage {
		commands = append(commands, autostartServices())
	}
	return tea.Batch(commands...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return m.frame.Update(msg) }
func (m *model) View() tea.View                          { return m.frame.View() }

type page struct {
	app   *dashboard
	index int
	title string
}

func (p *page) Title() string { return p.title }
func (p *page) Update(msg tea.Msg) tea.Cmd {
	p.app.active = p.index
	return p.app.update(msg)
}
func (p *page) View(width, height int) string {
	p.app.active = p.index
	switch p.index {
	case 1:
		return p.app.models.View(width, height, p.app.data)
	case 2:
		return p.app.system.View(width, height, p.app.data)
	case 3:
		return p.app.activity.View(width, height, p.app.data)
	default:
		return p.app.services.View(width, height, p.app.data, p.app.manage)
	}
}
func (p *page) CapturingInput() bool {
	return p.index == 3 && p.app.activity.CapturingInput() ||
		p.index == 1 && p.app.models.CapturingInput()
}

func (d *dashboard) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return d.updateKey(msg)
	case tickMsg:
		return tea.Batch(d.refresh(), tick())
	case refreshMsg:
		return d.refresh()
	case pollResult:
		d.applyPoll(msg)
	case catalogMsg:
		d.applyCatalog(msg)
	default:
		return d.updateAction(msg)
	}
	return nil
}

func (d *dashboard) updateAction(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case serviceRequest:
		command := runServiceAction(d.ctx, d.client, msg)
		if msg.panel != panelRuntime && msg.action == 1 {
			return tea.Batch(command, d.services.spin.Tick)
		}
		return command
	case spinner.TickMsg:
		return d.services.advanceSpinner(msg)
	case rpcRequest:
		return d.runRPC(msg)
	case actionMsg:
		if msg.err != nil {
			d.notice = ""
			d.data.warnings["action"] = msg.err.Error()
		} else {
			delete(d.data.warnings, "action")
			d.notice = msg.notice
			if msg.download != nil {
				d.upsertDownload(msg.download)
			}
		}
		d.services.complete()
		d.models.complete(msg)
		return d.refresh()
	}
	return nil
}

func (d *dashboard) updateKey(msg tea.KeyPressMsg) tea.Cmd {
	if !d.capturing() && msg.String() == "r" {
		return d.refresh()
	}
	switch d.active {
	case 0:
		return d.services.Update(msg, d.data, d.manage)
	case 1:
		return d.models.Update(msg, d.data)
	case 2:
		d.system.Update(msg)
	case 3:
		d.activity.Update(msg)
	}
	return nil
}

func (d *dashboard) capturing() bool {
	return d.active == 3 && d.activity.CapturingInput() ||
		d.active == 1 && d.models.CapturingInput()
}

func (d *dashboard) refresh() tea.Cmd {
	if d.refreshing {
		return nil
	}
	d.refreshing = true
	backend := d.models.backend(d.data.backends)
	commands := []tea.Cmd{poll(d.ctx, d.client, backend, d.data.downloads, d.manage)}
	if backend != "" && !d.catalogInFlight {
		d.catalogInFlight, d.data.catalogPending = true, true
		commands = append(commands, fetchCatalog(d.ctx, d.client, backend))
	}
	return tea.Batch(commands...)
}

// fetchCatalog runs outside the poll with a timeout sized for a remote fetch
// that has to enumerate repository files before it can answer.
func fetchCatalog(ctx context.Context, client controlv1connect.ControlServiceClient, backend string) tea.Cmd {
	return func() tea.Msg {
		catalogCtx, cancel := context.WithTimeout(ctx, catalogTimeout)
		defer cancel()
		value, err := client.ListModelCatalog(catalogCtx, &controlv1.ListModelCatalogRequest{Backend: backend})
		return catalogMsg{value: value, err: err}
	}
}

func (d *dashboard) applyCatalog(msg catalogMsg) {
	d.catalogInFlight, d.data.catalogPending = false, false
	if msg.err != nil {
		d.data.warnings["catalog"] = msg.err.Error()
		return
	}
	delete(d.data.warnings, "catalog")
	d.data.catalog = msg.value
}

func (d *dashboard) applyPoll(result pollResult) {
	current, next := d.data, result.value
	if !result.ok["base"] {
		next.info, next.runtimes, next.backends, next.profiles = current.info, current.runtimes, current.backends, current.profiles
	}
	next.catalogPending = !result.ok["catalog"]
	// The catalog arrives on its own command, so a poll never carries one.
	next.catalog, next.catalogPending = current.catalog, current.catalogPending
	if !result.ok["local"] {
		next.local = current.local
	}
	if !result.ok["signals"] {
		next.signals = current.signals
	}
	if !result.ok["events"] {
		next.events = current.events
	}
	if !result.ok["downloads"] {
		next.downloads = current.downloads
	}
	if !result.ok["logs"] {
		next.controlLog, next.engineLog, next.webLog = current.controlLog, current.engineLog, current.webLog
	}
	d.data, d.refreshing = next, false
}

func (d *dashboard) status() (string, tuikit.Level) {
	if len(d.data.warnings) > 0 {
		keys := slices.Sorted(maps.Keys(d.data.warnings))
		for i, key := range keys {
			keys[i] = key + ": " + d.data.warnings[key]
		}
		return strings.Join(keys, ", ") + refreshedText(d.data.refreshed), tuikit.LevelWarning
	}
	if d.notice != "" {
		return d.notice + refreshedText(d.data.refreshed), tuikit.LevelSuccess
	}
	if !d.data.refreshed.IsZero() && len(d.data.profiles.GetProfiles()) == 0 {
		return "No profiles yet — press n on Models to create one" + refreshedText(d.data.refreshed), tuikit.LevelWarning
	}
	return "Ready" + refreshedText(d.data.refreshed), tuikit.LevelInfo
}

func refreshedText(value time.Time) string {
	if value.IsZero() {
		return "   Last refreshed: --:--:--"
	}
	return "   Last refreshed: " + value.Format("15:04:05")
}

func (d *dashboard) upsertDownload(value *controlv1.ModelDownload) {
	for i, item := range d.data.downloads {
		if item.GetId() == value.GetId() {
			d.data.downloads[i] = value
			return
		}
	}
	d.data.downloads = append(d.data.downloads, value)
}

func (d *dashboard) runRPC(request rpcRequest) tea.Cmd {
	return func() tea.Msg {
		msg := actionMsg{notice: request.notice}
		switch request.kind {
		case rpcStart:
			_, msg.err = d.client.StartRuntime(d.ctx, &controlv1.StartRuntimeRequest{Profile: request.profile, Replace: request.replace})
		case rpcReset:
			_, msg.err = d.client.ResetRuntimes(d.ctx, &controlv1.ResetRuntimesRequest{})
		case rpcStop:
			_, msg.err = d.client.StopRuntime(d.ctx, &controlv1.StopRuntimeRequest{Profile: request.profile})
		case rpcPutProfile:
			_, msg.err = d.client.PutProfile(d.ctx, &controlv1.PutProfileRequest{Name: request.create.GetName(), Profile: request.create, CreateOnly: true})
		case rpcAutostart:
			_, msg.err = d.client.SetProfileAutostart(d.ctx, &controlv1.SetProfileAutostartRequest{Name: request.profile, Enabled: request.enabled})
		case rpcDownload:
			var response *controlv1.StartModelDownloadResponse
			response, msg.err = d.client.StartModelDownload(d.ctx, &controlv1.StartModelDownloadRequest{Profile: request.profile})
			msg.download = response.GetDownload()
		case rpcCancel:
			var response *controlv1.CancelModelDownloadResponse
			response, msg.err = d.client.CancelModelDownload(d.ctx, &controlv1.CancelModelDownloadRequest{Id: request.id})
			msg.download = response.GetDownload()
		case rpcApply:
			_, msg.err = d.client.ApplyDownloadToProfile(d.ctx, &controlv1.ApplyDownloadToProfileRequest{Profile: request.profile, Id: request.id})
		case rpcCleanup:
			_, msg.err = d.client.CleanupProfile(d.ctx, &controlv1.CleanupProfileRequest{Name: request.profile})
		case rpcDelete:
			_, msg.err = d.client.DeleteLocalModel(d.ctx, &controlv1.DeleteLocalModelRequest{Backend: request.backend, Path: request.path})
		case rpcInstall:
			_, msg.err = d.client.InstallBackend(d.ctx, &controlv1.InstallBackendRequest{Backend: request.backend})
		}
		return msg
	}
}

type poller struct {
	ctx     context.Context
	client  controlv1connect.ControlServiceClient
	backend string
	result  pollResult
	mu      sync.Mutex
	wg      sync.WaitGroup
}

func poll(ctx context.Context, client controlv1connect.ControlServiceClient, backend string, downloads []*controlv1.ModelDownload, local bool) tea.Cmd {
	return func() tea.Msg {
		pollCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		p := &poller{
			ctx: pollCtx, client: client, backend: backend,
			result: pollResult{value: snapshot{warnings: map[string]string{}, refreshed: time.Now(), downloads: downloads}, ok: map[string]bool{}},
		}
		if !local {
			p.result.ok["logs"] = true
		}
		p.fetch("base", p.base)
		p.fetch("local", p.local)
		p.fetch("signals", p.signals)
		p.fetch("events", p.events)
		p.fetch("downloads", p.downloads)
		if local {
			p.fetch("logs", p.logs)
		}
		p.wg.Wait()
		if local {
			p.result.value.webReachable = gatewayReachable(listenAddress())
		}
		return p.result
	}
}

// gatewayReachable dials the gateway's own address. A refused connection is the
// normal "not running" answer, not an error worth surfacing, so this reports a
// bool rather than going through fetch and its warning line.
func gatewayReachable(address string) bool {
	if address == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", address, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (p *poller) fetch(key string, call func() error) {
	p.wg.Go(func() {
		err := call()
		p.mu.Lock()
		defer p.mu.Unlock()
		p.result.ok[key] = err == nil
		if err != nil {
			p.result.value.warnings[key] = err.Error()
		}
	})
}

func (p *poller) base() (err error) {
	if p.result.value.info, err = p.client.GetInfo(p.ctx, &controlv1.GetInfoRequest{}); err != nil {
		return err
	}
	if p.result.value.runtimes, err = p.client.GetRuntimeStatus(p.ctx, &controlv1.GetRuntimeStatusRequest{}); err != nil {
		return err
	}
	if p.result.value.backends, err = p.client.ListBackends(p.ctx, &controlv1.ListBackendsRequest{}); err != nil {
		return err
	}
	p.result.value.profiles, err = p.client.ListProfiles(p.ctx, &controlv1.ListProfilesRequest{})
	return err
}

func (p *poller) local() (err error) {
	if p.backend != "" {
		p.result.value.local, err = p.client.ListLocalModels(p.ctx, &controlv1.ListLocalModelsRequest{Backend: p.backend})
	}
	return err
}

func (p *poller) signals() (err error) {
	p.result.value.signals, err = p.client.GetSignals(p.ctx, &controlv1.GetSignalsRequest{})
	return err
}

func (p *poller) events() (err error) {
	p.result.value.events, err = p.client.ListEvents(p.ctx, &controlv1.ListEventsRequest{})
	return err
}

func (p *poller) downloads() error {
	for i, item := range p.result.value.downloads {
		response, err := p.client.GetModelDownload(p.ctx, &controlv1.GetModelDownloadRequest{Id: item.GetId()})
		if err != nil {
			return err
		}
		p.result.value.downloads[i] = response.GetDownload()
	}
	return nil
}

func (p *poller) logs() error {
	p.result.value.controlLog, p.result.value.engineLog, p.result.value.webLog = readLogs()
	return nil
}

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

type rpcKind int

const (
	rpcStart rpcKind = iota
	rpcReset
	rpcStop
	rpcPutProfile
	rpcAutostart
	rpcDownload
	rpcCancel
	rpcApply
	rpcCleanup
	rpcDelete
	rpcInstall
)

type rpcRequest struct {
	kind                       rpcKind
	profile, backend, path, id string
	create                     *controlv1.Profile
	enabled, replace           bool
	notice                     string
}

func selected[T any](items []T, index int) (T, bool) {
	var zero T
	if index < 0 || index >= len(items) {
		return zero, false
	}
	return items[index], true
}
