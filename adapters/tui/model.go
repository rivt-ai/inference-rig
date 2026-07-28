package tui

import (
	"context"
	"fmt"
	"maps"
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
)

type snapshot struct {
	info               *controlv1.GetInfoResponse
	backends           *controlv1.ListBackendsResponse
	profiles           *controlv1.ListProfilesResponse
	catalog            *controlv1.ListModelCatalogResponse
	local              *controlv1.ListLocalModelsResponse
	signals            *controlv1.GetSignalsResponse
	events             *controlv1.ListEventsResponse
	downloads          []*controlv1.ModelDownload
	controlLog, webLog []string
	warnings           map[string]string
	refreshed          time.Time
}

type pollResult struct {
	value snapshot
	ok    map[string]bool
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
	notice     string
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
		commands = append(commands, autostartServices(m.app.ctx, m.app.client))
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
	return poll(d.ctx, d.client, backend, d.data.downloads, d.manage)
}

func (d *dashboard) applyPoll(result pollResult) {
	current, next := d.data, result.value
	if !result.ok["base"] {
		next.info, next.backends, next.profiles = current.info, current.backends, current.profiles
	}
	if !result.ok["catalog"] {
		next.catalog = current.catalog
	}
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
		next.controlLog, next.webLog = current.controlLog, current.webLog
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
			_, msg.err = d.client.StartRuntime(d.ctx, &controlv1.StartRuntimeRequest{Profile: request.profile})
		case rpcStop:
			_, msg.err = d.client.StopRuntime(d.ctx, &controlv1.StopRuntimeRequest{Profile: request.profile})
		case rpcRestart:
			_, msg.err = d.client.RestartRuntime(d.ctx, &controlv1.RestartRuntimeRequest{Profile: request.profile})
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
		p.fetch("catalog", p.catalog)
		p.fetch("local", p.local)
		p.fetch("signals", p.signals)
		p.fetch("events", p.events)
		p.fetch("downloads", p.downloads)
		if local {
			p.fetch("logs", p.logs)
		}
		p.wg.Wait()
		return p.result
	}
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
	if p.result.value.backends, err = p.client.ListBackends(p.ctx, &controlv1.ListBackendsRequest{}); err != nil {
		return err
	}
	p.result.value.profiles, err = p.client.ListProfiles(p.ctx, &controlv1.ListProfilesRequest{})
	return err
}

func (p *poller) catalog() (err error) {
	if p.backend != "" {
		p.result.value.catalog, err = p.client.ListModelCatalog(p.ctx, &controlv1.ListModelCatalogRequest{Backend: p.backend})
	}
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
	p.result.value.controlLog, p.result.value.webLog = readLogs()
	return nil
}

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

type rpcKind int

const (
	rpcStart rpcKind = iota
	rpcStop
	rpcRestart
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
	enabled                    bool
	notice                     string
}

func running(info *controlv1.GetInfoResponse, profile string) bool {
	for _, name := range info.GetRunningProfiles() {
		if name == profile {
			return true
		}
	}
	return false
}

func selected[T any](items []T, index int) (T, bool) {
	var zero T
	if index < 0 || index >= len(items) {
		return zero, false
	}
	return items[index], true
}

func countText(count int, singular string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %ss", count, singular)
}
