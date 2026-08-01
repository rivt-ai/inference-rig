package rpc

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"
	"gopkg.in/yaml.v3"

	"inferencerig/backends"
	"inferencerig/core/control"
	"inferencerig/core/modelcatalog"
	"inferencerig/core/modeldownload"
	"inferencerig/core/profiles"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
	coreruntime "inferencerig/core/runtime"
	"inferencerig/core/signals"
	"inferencerig/internal/buildinfo"
)

const ServiceName = "inferencerig-control"

// ControlService exposes the canonical control manager through ConnectRPC.
type ControlService struct {
	controlv1connect.UnimplementedControlServiceHandler
	manager *control.Manager
}

// NewControlService creates the canonical RPC implementation.
func NewControlService(manager *control.Manager) *ControlService {
	if manager == nil {
		panic("rpc: control manager is required")
	}
	return &ControlService{manager: manager}
}

// ControlHandler returns the generated Connect route and handler.
func ControlHandler(service *ControlService) (string, http.Handler) {
	return controlv1connect.NewControlServiceHandler(
		service, connect.WithInterceptors(ValidateRequestInterceptor()),
	)
}

func (s *ControlService) Health(context.Context, *controlv1.HealthRequest) (*controlv1.HealthResponse, error) {
	return &controlv1.HealthResponse{Ok: true, Service: ServiceName}, nil
}

func (s *ControlService) ListBackends(context.Context, *controlv1.ListBackendsRequest) (*controlv1.ListBackendsResponse, error) {
	items := s.manager.Backends()
	out := make([]*controlv1.BackendInfo, 0, len(items))
	for _, backend := range items {
		out = append(out, backendProto(backend))
	}
	return &controlv1.ListBackendsResponse{Ok: true, Backends: out}, nil
}

func (s *ControlService) ListProfiles(ctx context.Context, _ *controlv1.ListProfilesRequest) (*controlv1.ListProfilesResponse, error) {
	docs, err := s.manager.ListProfileDocuments(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	out := make([]*controlv1.Profile, 0, len(docs))
	for _, doc := range docs {
		out = append(out, profileProto(doc))
	}
	return &controlv1.ListProfilesResponse{Ok: true, Profiles: out}, nil
}

func (s *ControlService) GetProfile(ctx context.Context, req *controlv1.GetProfileRequest) (*controlv1.GetProfileResponse, error) {
	if req.GetName() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "profile name is required"))
	}
	doc, err := s.manager.GetProfile(ctx, req.GetName())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.GetProfileResponse{Ok: true, Profile: profileProto(doc)}, nil
}

func (s *ControlService) PutProfile(ctx context.Context, req *controlv1.PutProfileRequest) (*controlv1.PutProfileResponse, error) {
	if req.GetName() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "profile name is required"))
	}
	// A structured profile is rendered here rather than by the caller, so an
	// editor never needs a YAML implementation that could disagree with ours.
	profileYAML := req.GetProfileYaml()
	if profileYAML == "" && req.GetProfile() != nil {
		rendered, err := renderProfileYAML(req.GetName(), req.GetProfile())
		if err != nil {
			return nil, rpcError(err)
		}
		profileYAML = rendered
	}
	if profileYAML == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "profile YAML or structured profile is required"))
	}
	doc, err := s.manager.PutProfile(ctx, req.GetName(), profileYAML, req.GetCreateOnly())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.PutProfileResponse{Ok: true, Profile: profileProto(doc)}, nil
}

func (s *ControlService) DeleteProfile(ctx context.Context, req *controlv1.DeleteProfileRequest) (*controlv1.DeleteProfileResponse, error) {
	if req.GetName() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "profile name is required"))
	}
	if _, err := s.manager.DeleteProfile(ctx, req.GetName()); err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.DeleteProfileResponse{Ok: true}, nil
}

func (s *ControlService) InstallBackend(ctx context.Context, req *controlv1.InstallBackendRequest) (*controlv1.InstallBackendResponse, error) {
	if req.GetBackend() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "backend is required"))
	}
	return installResponse(s.manager.InstallBackend(ctx, req.GetBackend(), backends.InstallOptions{
		Version: req.GetVersion(), Upgrade: req.GetUpgrade(), Force: req.GetForce(),
	}))
}

func (s *ControlService) RollbackBackend(ctx context.Context, req *controlv1.RollbackBackendRequest) (*controlv1.InstallBackendResponse, error) {
	if req.GetBackend() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "backend is required"))
	}
	return installResponse(s.manager.RollbackBackend(ctx, req.GetBackend()))
}

// installResponse renders whichever install the manager left active — a fresh
// one or a restored one; both answer the same question.
func installResponse(result backends.InstallResult, err error) (*controlv1.InstallBackendResponse, error) {
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.InstallBackendResponse{
		Ok: true, Version: result.Version, Path: result.Path,
		Changed: result.Changed, Message: result.Message,
	}, nil
}

func (s *ControlService) GetBackendInstallStatus(ctx context.Context, req *controlv1.GetBackendInstallStatusRequest) (*controlv1.GetBackendInstallStatusResponse, error) {
	if req.GetBackend() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "backend is required"))
	}
	status, err := s.manager.BackendInstallStatus(ctx, req.GetBackend())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.GetBackendInstallStatusResponse{
		Ok: true, Installed: status.Installed, Managed: status.Managed,
		Version: status.Version, Path: status.Path,
	}, nil
}

func (s *ControlService) StartRuntime(ctx context.Context, req *controlv1.StartRuntimeRequest) (*controlv1.StartRuntimeResponse, error) {
	result, status, err := s.runtimeAction(ctx, req.GetProfile(), s.manager.StartRuntime)
	return &controlv1.StartRuntimeResponse{Ok: err == nil, Result: result, Status: status}, err
}

func (s *ControlService) StopRuntime(ctx context.Context, req *controlv1.StopRuntimeRequest) (*controlv1.StopRuntimeResponse, error) {
	result, status, err := s.runtimeAction(ctx, req.GetProfile(), s.manager.StopRuntime)
	return &controlv1.StopRuntimeResponse{Ok: err == nil, Result: result, Status: status}, err
}

func (s *ControlService) GetRuntimeStatus(ctx context.Context, req *controlv1.GetRuntimeStatusRequest) (*controlv1.GetRuntimeStatusResponse, error) {
	if req.GetProfile() != "" {
		status, err := s.manager.RuntimeStatus(ctx, req.GetProfile())
		if err != nil {
			return nil, rpcError(err)
		}
		return &controlv1.GetRuntimeStatusResponse{Ok: true, Status: statusProto(status)}, nil
	}
	// No profile named: report them all, so a dashboard polling every few
	// seconds makes one call instead of one per profile.
	docs, err := s.manager.ListProfileDocuments(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	all := make([]*controlv1.ProfileRuntimeStatus, 0, len(docs))
	aggregate := &controlv1.RuntimeStatus{
		State:     string(coreruntime.Stopped),
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}
	for _, doc := range docs {
		status, statusErr := s.manager.RuntimeStatus(ctx, doc.Name)
		if statusErr != nil {
			// One unreadable profile must not blank the whole dashboard.
			continue
		}
		message := statusProto(status)
		all = append(all, &controlv1.ProfileRuntimeStatus{
			Name: doc.Name, Backend: doc.Effective.Backend, Status: message,
		})
		aggregate.Processes = append(aggregate.Processes, message.GetProcesses()...)
		if status.State == coreruntime.Running {
			aggregate.State = string(coreruntime.Running)
		}
	}
	return &controlv1.GetRuntimeStatusResponse{Ok: true, Status: aggregate, Profiles: all}, nil
}

func (s *ControlService) ResolveProfileModel(ctx context.Context, req *controlv1.ResolveProfileModelRequest) (*controlv1.ResolveProfileModelResponse, error) {
	if req.GetProfile() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "profile is required"))
	}
	resolved, plan, err := s.manager.ResolveProfileModel(ctx, req.GetProfile())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.ResolveProfileModelResponse{
		Ok: true, Model: resolvedProto(resolved), Plan: planProto(plan),
	}, nil
}

func (s *ControlService) StartModelDownload(ctx context.Context, req *controlv1.StartModelDownloadRequest) (*controlv1.StartModelDownloadResponse, error) {
	// Either form is valid: a profile downloads that profile's model, while
	// backend + reference downloads a catalog entry before any profile exists.
	if req.GetProfile() == "" {
		job, err := s.manager.StartCatalogDownload(
			ctx, req.GetBackend(), req.GetReference(), req.GetVariantReference(), req.GetForce(),
		)
		if err != nil {
			return nil, rpcError(err)
		}
		return &controlv1.StartModelDownloadResponse{Ok: true, Download: downloadProto(job)}, nil
	}
	job, err := s.manager.StartDownload(ctx, req.GetProfile(), req.GetForce())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.StartModelDownloadResponse{Ok: true, Download: downloadProto(job)}, nil
}

func (s *ControlService) ResolveModel(ctx context.Context, req *controlv1.ResolveModelRequest) (*controlv1.ResolveModelResponse, error) {
	resolved, plan, err := s.manager.ResolveModel(ctx, req.GetBackend(), req.GetReference(), req.GetVariantReference())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.ResolveModelResponse{
		Ok: true, Model: resolvedProto(resolved), Plan: planProto(plan),
	}, nil
}

func (s *ControlService) GetModelDownload(ctx context.Context, req *controlv1.GetModelDownloadRequest) (*controlv1.GetModelDownloadResponse, error) {
	if req.GetId() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "download ID is required"))
	}
	job, err := s.manager.GetDownload(ctx, req.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.GetModelDownloadResponse{Ok: true, Download: downloadProto(job)}, nil
}

func (s *ControlService) CancelModelDownload(ctx context.Context, req *controlv1.CancelModelDownloadRequest) (*controlv1.CancelModelDownloadResponse, error) {
	if req.GetId() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "download ID is required"))
	}
	job, err := s.manager.CancelDownload(ctx, req.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.CancelModelDownloadResponse{Ok: true, Download: downloadProto(job)}, nil
}

func (s *ControlService) GetSignals(ctx context.Context, _ *controlv1.GetSignalsRequest) (*controlv1.GetSignalsResponse, error) {
	snapshot, err := s.manager.Signals(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.GetSignalsResponse{Ok: true, Signals: signalsProto(snapshot)}, nil
}

func (s *ControlService) ListEvents(context.Context, *controlv1.ListEventsRequest) (*controlv1.ListEventsResponse, error) {
	events := s.manager.Events().List()
	out := make([]*controlv1.Event, 0, len(events))
	for _, event := range events {
		out = append(out, eventProto(event))
	}
	return &controlv1.ListEventsResponse{Ok: true, Events: out}, nil
}

func (s *ControlService) WatchEvents(ctx context.Context, _ *controlv1.WatchEventsRequest, stream *connect.ServerStream[controlv1.WatchEventsResponse]) error {
	events, backlog, unsubscribe := s.manager.Events().SubscribeAndList()
	defer unsubscribe()
	for _, event := range backlog {
		if err := stream.Send(&controlv1.WatchEventsResponse{Event: eventProto(event)}); err != nil {
			return err
		}
	}
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return nil
			}
			if err := stream.Send(&controlv1.WatchEventsResponse{Event: eventProto(event)}); err != nil {
				return err
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *ControlService) ListModelCatalog(ctx context.Context, req *controlv1.ListModelCatalogRequest) (*controlv1.ListModelCatalogResponse, error) {
	if req.GetBackend() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "backend is required"))
	}
	result, err := s.manager.ListModelCatalog(ctx, modelcatalog.SearchRequest{
		Backend: req.GetBackend(), Query: req.GetQuery(), Limit: int(req.GetLimit()),
	})
	if err != nil {
		return nil, rpcError(err)
	}
	// Fit is computed here rather than in the catalog because it depends on the
	// host and on the backend's memory model, neither of which the catalog
	// knows. A host we cannot read yields "unknown" verdicts, not an error.
	backend, err := s.manager.Backend(req.GetBackend())
	if err != nil {
		return nil, rpcError(err)
	}
	host, err := s.manager.HostResources(ctx, req.GetBackend())
	if err != nil {
		return nil, rpcError(err)
	}
	models := make([]*controlv1.CatalogModel, 0, len(result.Models))
	for _, model := range result.Models {
		message := catalogModelProto(model)
		for _, variant := range message.GetVariants() {
			estimate, fitErr := backend.Fit(profiles.Profile{}, variant.GetSizeBytes(), host)
			if fitErr != nil {
				continue
			}
			variant.Fit = fitProto(estimate)
		}
		message.BestVariant = bestVariant(message.GetVariants())
		models = append(models, message)
	}
	models = filterByFit(models, req.GetMinFit())
	sortCatalog(models, req.GetSort())
	return &controlv1.ListModelCatalogResponse{
		Ok: true, Models: models, CacheHit: result.CacheHit, Stale: result.Stale,
		Machine: machineProto(host),
		Cache: &controlv1.CatalogCacheState{
			Hit: result.CacheHit, Stale: result.Stale,
		},
	}, nil
}

func (s *ControlService) EstimateFit(ctx context.Context, req *controlv1.EstimateFitRequest) (*controlv1.EstimateFitResponse, error) {
	estimate, host, err := s.manager.EstimateFit(ctx, req.GetBackend(), req.GetProfile(), req.GetSizeBytes())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.EstimateFitResponse{
		Ok: true, Fit: fitProto(estimate), Machine: machineProto(host),
	}, nil
}

func fitProto(estimate backends.FitEstimate) *controlv1.FitEstimate {
	return &controlv1.FitEstimate{
		Level:          fitLevelProto(estimate.Level),
		Reason:         estimate.Reason,
		RequiredBytes:  estimate.RequiredBytes,
		AvailableBytes: estimate.AvailableBytes,
	}
}

func fitLevelProto(level backends.FitLevel) controlv1.FitLevel {
	switch level {
	case backends.FitFits:
		return controlv1.FitLevel_FIT_LEVEL_FITS
	case backends.FitMarginal:
		return controlv1.FitLevel_FIT_LEVEL_MARGINAL
	case backends.FitTooLarge:
		return controlv1.FitLevel_FIT_LEVEL_TOO_LARGE
	case backends.FitUnknown:
		return controlv1.FitLevel_FIT_LEVEL_UNKNOWN
	default:
		return controlv1.FitLevel_FIT_LEVEL_UNSPECIFIED
	}
}

func machineProto(host backends.HostResources) *controlv1.MachineProfile {
	return &controlv1.MachineProfile{
		TotalMemoryBytes:       uint64(max(host.TotalRAMBytes, 0)),
		AvailableMemoryBytes:   uint64(max(host.AvailableRAMBytes, 0)),
		AcceleratorName:        host.AcceleratorName,
		UnifiedMemory:          host.UnifiedMemory,
		AcceleratorMemoryBytes: uint64(max(host.VRAMBytes, 0)),
	}
}

// fitRank orders verdicts from best to worst so "at least marginal" is a
// comparison rather than a set membership test.
func fitRank(level controlv1.FitLevel) int {
	switch level {
	case controlv1.FitLevel_FIT_LEVEL_FITS:
		return 3
	case controlv1.FitLevel_FIT_LEVEL_MARGINAL:
		return 2
	case controlv1.FitLevel_FIT_LEVEL_TOO_LARGE:
		return 1
	default:
		return 0
	}
}

// bestVariant is the largest variant that still fits, since quality tracks size
// within a repository. It falls back to the largest variant overall when
// nothing fits, so a caller always has something to show.
func bestVariant(variants []*controlv1.ModelVariant) *controlv1.ModelVariant {
	var best, largest *controlv1.ModelVariant
	for _, variant := range variants {
		if largest == nil || variant.GetSizeBytes() > largest.GetSizeBytes() {
			largest = variant
		}
		if fitRank(variant.GetFit().GetLevel()) < fitRank(controlv1.FitLevel_FIT_LEVEL_MARGINAL) {
			continue
		}
		if best == nil || variant.GetSizeBytes() > best.GetSizeBytes() {
			best = variant
		}
	}
	if best != nil {
		return best
	}
	return largest
}

func filterByFit(models []*controlv1.CatalogModel, minimum controlv1.FitLevel) []*controlv1.CatalogModel {
	if minimum == controlv1.FitLevel_FIT_LEVEL_UNSPECIFIED {
		return models
	}
	kept := make([]*controlv1.CatalogModel, 0, len(models))
	for _, model := range models {
		if fitRank(model.GetBestVariant().GetFit().GetLevel()) >= fitRank(minimum) {
			kept = append(kept, model)
		}
	}
	return kept
}

func sortCatalog(models []*controlv1.CatalogModel, order string) {
	switch order {
	case "downloads":
		sort.SliceStable(models, func(i, j int) bool {
			return models[i].GetDownloads() > models[j].GetDownloads()
		})
	case "likes":
		sort.SliceStable(models, func(i, j int) bool {
			return models[i].GetLikes() > models[j].GetLikes()
		})
	case "modified":
		sort.SliceStable(models, func(i, j int) bool {
			return models[i].GetLastModified() > models[j].GetLastModified()
		})
	case "fit":
		sort.SliceStable(models, func(i, j int) bool {
			return fitRank(models[i].GetBestVariant().GetFit().GetLevel()) >
				fitRank(models[j].GetBestVariant().GetFit().GetLevel())
		})
	}
}

func (s *ControlService) WatchModelCatalog(ctx context.Context, _ *controlv1.WatchModelCatalogRequest, stream *connect.ServerStream[controlv1.WatchModelCatalogResponse]) error {
	events, unsubscribe, err := s.manager.WatchModelCatalog()
	if err != nil {
		return rpcError(err)
	}
	defer unsubscribe()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return nil
			}
			if err := stream.Send(&controlv1.WatchModelCatalogResponse{
				Backend: event.Backend, Query: event.Query, Error: event.Error,
			}); err != nil {
				return err
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *ControlService) ListLocalModels(ctx context.Context, req *controlv1.ListLocalModelsRequest) (*controlv1.ListLocalModelsResponse, error) {
	if req.GetBackend() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "backend is required"))
	}
	items, err := s.manager.ListLocalModels(ctx, req.GetBackend())
	if err != nil {
		return nil, rpcError(err)
	}
	models := make([]*controlv1.LocalModel, 0, len(items))
	for _, item := range items {
		using, usedErr := s.manager.ProfilesUsingModel(ctx, item.Path)
		if usedErr != nil {
			return nil, rpcError(usedErr)
		}
		models = append(models, &controlv1.LocalModel{
			Path: item.Path, Filename: item.Filename, SizeBytes: item.SizeBytes,
			ModifiedAt: item.ModifiedAt.Format(time.RFC3339), UsedByProfiles: using,
		})
	}
	return &controlv1.ListLocalModelsResponse{Ok: true, Models: models}, nil
}

func (s *ControlService) DeleteLocalModel(ctx context.Context, req *controlv1.DeleteLocalModelRequest) (*controlv1.DeleteLocalModelResponse, error) {
	if req.GetBackend() == "" || req.GetPath() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "backend and path are required"))
	}
	if _, err := s.manager.DeleteLocalModelCascade(
		ctx, req.GetBackend(), req.GetPath(), req.GetCascadeProfiles(),
	); err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.DeleteLocalModelResponse{Ok: true}, nil
}

func (s *ControlService) ApplyDownloadToProfile(ctx context.Context, req *controlv1.ApplyDownloadToProfileRequest) (*controlv1.ApplyDownloadToProfileResponse, error) {
	if req.GetProfile() == "" || req.GetId() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "profile and download ID are required"))
	}
	if req.GetPreview() {
		original, updated, err := s.manager.PreviewDownloadApply(ctx, req.GetProfile(), req.GetId())
		if err != nil {
			return nil, rpcError(err)
		}
		return &controlv1.ApplyDownloadToProfileResponse{
			Ok: true, PreviewDiff: &controlv1.TextDiff{Original: original, Updated: updated},
		}, nil
	}
	doc, err := s.manager.ApplyDownloadToProfile(ctx, req.GetProfile(), req.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.ApplyDownloadToProfileResponse{Ok: true, Profile: profileProto(doc)}, nil
}

func (s *ControlService) CleanupProfile(ctx context.Context, req *controlv1.CleanupProfileRequest) (*controlv1.CleanupProfileResponse, error) {
	if req.GetName() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "profile name is required"))
	}
	if err := s.manager.CleanupProfile(ctx, req.GetName()); err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.CleanupProfileResponse{Ok: true}, nil
}

func (s *ControlService) SetProfileAutostart(ctx context.Context, req *controlv1.SetProfileAutostartRequest) (*controlv1.SetProfileAutostartResponse, error) {
	if req.GetName() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "profile name is required"))
	}
	if _, err := s.manager.SetProfileAutostart(ctx, req.GetName(), req.GetEnabled()); err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.SetProfileAutostartResponse{Ok: true}, nil
}

func (s *ControlService) SetStartupServices(ctx context.Context, req *controlv1.SetStartupServicesRequest) (*controlv1.SetStartupServicesResponse, error) {
	if _, err := s.manager.SetStartupServices(ctx, req.GetServices()); err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.SetStartupServicesResponse{Ok: true}, nil
}

func (s *ControlService) RestartRuntime(ctx context.Context, req *controlv1.RestartRuntimeRequest) (*controlv1.RestartRuntimeResponse, error) {
	if req.GetProfile() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "profile is required"))
	}
	result, err := s.manager.RestartRuntime(ctx, req.GetProfile())
	if err != nil {
		return nil, rpcError(err)
	}
	status, err := s.manager.RuntimeStatus(ctx, req.GetProfile())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.RestartRuntimeResponse{
		Ok: true, Stopped: commandResultProto(result.Stopped),
		Started: commandResultProto(result.Started), Status: statusProto(status),
	}, nil
}

func (s *ControlService) GetInfo(ctx context.Context, _ *controlv1.GetInfoRequest) (*controlv1.GetInfoResponse, error) {
	info, err := s.manager.GetInfo(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.GetInfoResponse{
		Ok: true, Profiles: int32(info.Profiles), Backends: int32(info.Backends),
		RunningProfiles: info.RunningProfiles, AutostartProfiles: info.AutostartProfiles,
		StartupServices: info.StartupServices,
		Build: &controlv1.BuildInfo{
			Version: buildinfo.Version, Commit: buildinfo.Commit, CommitTime: buildinfo.CommitTime,
		},
	}, nil
}

func (s *ControlService) GetBackendParams(ctx context.Context, req *controlv1.GetBackendParamsRequest) (*controlv1.GetBackendParamsResponse, error) {
	if req.GetBackend() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "backend is required"))
	}
	items, err := s.manager.GetBackendParams(ctx, req.GetBackend())
	if err != nil {
		return nil, rpcError(err)
	}
	params := make([]*controlv1.BackendParameter, 0, len(items))
	for _, item := range items {
		params = append(params, &controlv1.BackendParameter{
			Name: item.Name, Description: item.Description, Required: item.Required,
			Aliases: item.Aliases, ValueHint: item.ValueHint,
			DefaultValue: item.DefaultValue, Type: parameterTypeProto(item.Type),
		})
	}
	return &controlv1.GetBackendParamsResponse{Ok: true, Params: params}, nil
}

func parameterTypeProto(kind backends.ParameterType) controlv1.ParameterType {
	switch kind {
	case backends.ParameterString:
		return controlv1.ParameterType_PARAMETER_TYPE_STRING
	case backends.ParameterInt:
		return controlv1.ParameterType_PARAMETER_TYPE_INT
	case backends.ParameterBool:
		return controlv1.ParameterType_PARAMETER_TYPE_BOOL
	case backends.ParameterList:
		return controlv1.ParameterType_PARAMETER_TYPE_LIST
	default:
		return controlv1.ParameterType_PARAMETER_TYPE_UNSPECIFIED
	}
}

func backendProto(backend backends.Backend) *controlv1.BackendInfo {
	capabilities := backend.Capabilities()
	return &controlv1.BackendInfo{
		Name: backend.Name(),
		Capabilities: &controlv1.BackendCapabilities{
			SingleFileArtifacts: capabilities.SingleFileArtifacts,
			MultiFileArtifacts:  capabilities.MultiFileArtifacts,
			DiscreteVram:        capabilities.DiscreteVRAM, UnifiedMemory: capabilities.UnifiedMemory,
			ManagedInstall: capabilities.ManagedInstall, SingleActiveProfile: capabilities.SingleActiveProfile,
			ParameterIntrospection: capabilities.ParameterIntrospection,
		},
	}
}

func catalogModelProto(model modelcatalog.Model) *controlv1.CatalogModel {
	variants := make([]*controlv1.ModelVariant, 0, len(model.Variants))
	for _, variant := range model.Variants {
		variants = append(variants, &controlv1.ModelVariant{
			Name: variant.Name, Reference: variant.Reference,
			SizeBytes: variant.SizeBytes, MultiFile: variant.MultiFile,
		})
	}
	return &controlv1.CatalogModel{
		Id: model.ID, Url: model.URL, Downloads: model.Downloads, Likes: model.Likes,
		LastModified: model.LastModified, Tags: model.Tags, Variants: variants,
	}
}

func profileProto(doc profiles.ProfileDocument) *controlv1.Profile {
	p := doc.Effective
	message := &controlv1.Profile{
		Name: doc.Name, Backend: p.Backend, ProfileYaml: doc.ProfileYAML,
		ModelSource: p.Model.Source, ModelReference: p.Model.Reference,
		Host: p.Listen.Host, Port: int32(p.Listen.Port),
	}
	if args, err := structpb.NewStruct(normalizeEngineArgs(p.EngineArgs)); err == nil {
		message.EngineArgs = args
	}
	return message
}

// normalizeEngineArgs coerces YAML-decoded values into the types structpb
// accepts. yaml.v3 yields int and map[string]any, neither of which structpb
// handles, so an unconverted profile would silently lose its engine args.
func normalizeEngineArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for key, value := range args {
		out[key] = normalizeEngineValue(value)
	}
	return out
}

func normalizeEngineValue(value any) any {
	switch typed := value.(type) {
	case int:
		return float64(typed)
	case int32:
		return float64(typed)
	case int64:
		return float64(typed)
	case uint64:
		return float64(typed)
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, normalizeEngineValue(item))
		}
		return items
	case map[string]any:
		return normalizeEngineArgs(typed)
	case map[any]any:
		// yaml.v3 produces this for nested mappings with non-string keys.
		nested := make(map[string]any, len(typed))
		for key, item := range typed {
			nested[fmt.Sprint(key)] = normalizeEngineValue(item)
		}
		return nested
	default:
		return value
	}
}

// renderProfileYAML turns a structured profile into the canonical YAML the
// store validates. Integral engine-arg values are emitted as integers: JSON and
// structpb have only float64, and a rendered "threads: 8.0" is not a value any
// engine accepts.
func renderProfileYAML(name string, message *controlv1.Profile) (string, error) {
	profile := profiles.Profile{
		Version: 1,
		Name:    name,
		Backend: message.GetBackend(),
		Model: profiles.ModelSpec{
			Source: message.GetModelSource(), Reference: message.GetModelReference(),
		},
		Listen: profiles.ListenSpec{Host: message.GetHost(), Port: int(message.GetPort())},
	}
	if args := message.GetEngineArgs(); args != nil {
		profile.EngineArgs = make(map[string]any, len(args.GetFields()))
		for key, value := range args.AsMap() {
			profile.EngineArgs[key] = demoteWholeFloats(value)
		}
	}
	rendered, err := yaml.Marshal(profile)
	if err != nil {
		return "", control.Errorf(control.ErrorInvalidInput, "render profile: %v", err)
	}
	return string(rendered), nil
}

func demoteWholeFloats(value any) any {
	switch typed := value.(type) {
	case float64:
		if typed == math.Trunc(typed) && math.Abs(typed) < math.MaxInt64 {
			return int64(typed)
		}
		return typed
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, demoteWholeFloats(item))
		}
		return items
	case map[string]any:
		nested := make(map[string]any, len(typed))
		for key, item := range typed {
			nested[key] = demoteWholeFloats(item)
		}
		return nested
	default:
		return value
	}
}

func (s *ControlService) runtimeAction(
	ctx context.Context,
	profile string,
	action func(context.Context, string) (coreruntime.CommandResult, error),
) (*controlv1.CommandResult, *controlv1.RuntimeStatus, error) {
	if profile == "" {
		return nil, nil, rpcError(control.Errorf(control.ErrorInvalidInput, "profile is required"))
	}
	result, err := action(ctx, profile)
	if err != nil {
		return nil, nil, rpcError(err)
	}
	status, err := s.manager.RuntimeStatus(ctx, profile)
	if err != nil {
		return nil, nil, rpcError(err)
	}
	return commandResultProto(result), statusProto(status), nil
}

func commandResultProto(result coreruntime.CommandResult) *controlv1.CommandResult {
	return &controlv1.CommandResult{
		Action: result.Action, ExitCode: int32(result.ExitCode), Stdout: result.Stdout,
		Stderr: result.Stderr, DurationMs: result.DurationMS,
	}
}

func statusProto(status coreruntime.Status) *controlv1.RuntimeStatus {
	processes := make([]*controlv1.ProcessStatus, 0, len(status.Processes))
	for _, process := range status.Processes {
		processes = append(processes, &controlv1.ProcessStatus{
			Name: process.Name, State: string(process.State), Pid: int32(process.PID),
			Host: process.Host, Port: int32(process.Port), Ready: process.Ready, LastError: process.LastError,
		})
	}
	return &controlv1.RuntimeStatus{
		State: string(status.State), Detail: status.Detail,
		CheckedAt: status.CheckedAt.Format(time.RFC3339), Processes: processes,
	}
}

func resolvedProto(model backends.ResolvedModel) *controlv1.ResolvedModel {
	artifacts := make([]*controlv1.Artifact, 0, len(model.Artifacts))
	for _, artifact := range model.Artifacts {
		artifacts = append(artifacts, &controlv1.Artifact{
			Name: artifact.Name, Uri: artifact.URI, SizeBytes: artifact.SizeBytes,
		})
	}
	return &controlv1.ResolvedModel{
		Source: model.Source, Reference: model.Reference,
		MultiFile: model.MultiFile, Artifacts: artifacts,
	}
}

func planProto(plan backends.ArtifactPlan) *controlv1.ArtifactPlan {
	items := make([]*controlv1.ArtifactItem, 0, len(plan.Items))
	for _, item := range plan.Items {
		items = append(items, &controlv1.ArtifactItem{
			Uri: item.URI, Filename: item.Filename,
			TargetPath: item.TargetPath, SizeBytes: item.SizeBytes,
		})
	}
	return &controlv1.ArtifactPlan{
		MultiFile: plan.MultiFile, TargetRoot: plan.TargetRoot,
		Items: items, TotalBytes: plan.TotalBytes,
	}
}

func downloadProto(job modeldownload.Job) *controlv1.ModelDownload {
	return &controlv1.ModelDownload{
		Id: job.ID, State: string(job.State), MultiFile: job.MultiFile,
		TargetPath: job.TargetPath, ItemCount: int32(job.ItemCount),
		ReceivedBytes: job.ReceivedBytes, TotalBytes: job.TotalBytes, Percent: job.Percent,
		Error: job.Error, StartedAt: job.StartedAt, CompletedAt: job.CompletedAt,
		Backend: job.Backend, Profile: job.Profile,
	}
}

func signalsProto(snapshot signals.Snapshot) *controlv1.Signals {
	accelerators := make([]*controlv1.Accelerator, 0, len(snapshot.Accelerators))
	for _, item := range snapshot.Accelerators {
		accelerators = append(accelerators, &controlv1.Accelerator{
			Name: item.Name, UnifiedMemory: item.UnifiedMemory,
			TotalMemoryBytes: item.TotalBytes, UsedMemoryBytes: item.UsedBytes,
		})
	}
	disks := make([]*controlv1.Disk, 0, len(snapshot.Disks))
	for _, item := range snapshot.Disks {
		disks = append(disks, &controlv1.Disk{
			Label: item.Label, Path: item.Path, TotalBytes: item.TotalBytes,
			UsedBytes: item.UsedBytes, UsedPercent: item.UsedPercent, FreeBytes: item.FreeBytes,
		})
	}
	processes := make([]*controlv1.RuntimeProcess, 0, len(snapshot.Runtime))
	for _, item := range snapshot.Runtime {
		processes = append(processes, &controlv1.RuntimeProcess{
			Name: item.Name, Pid: int32(item.PID), RssBytes: item.RSSBytes,
			CpuPercent: item.CPUPercent, Command: item.Command,
		})
	}
	return &controlv1.Signals{
		CapturedAt: snapshot.CapturedAt, TotalMemoryBytes: snapshot.Memory.TotalBytes,
		AvailableMemoryBytes: snapshot.Memory.AvailableBytes,
		UsedMemoryBytes:      snapshot.Memory.UsedBytes,
		UsedMemoryPercent:    snapshot.Memory.UsedPercent,
		LogicalCpuCores:      int32(snapshot.CPU.LogicalCores),
		CpuUsedPercent:       snapshot.CPU.UsedPercent, Warnings: snapshot.Warnings,
		Accelerators: accelerators, Disks: disks, Runtime: processes,
		Host: &controlv1.HostInfo{
			Hostname: snapshot.Host.Hostname, Os: snapshot.Host.OS, Platform: snapshot.Host.Platform,
		},
	}
}

func eventProto(event control.Event) *controlv1.Event {
	return &controlv1.Event{
		Id: event.ID, Time: event.Time, Action: event.Action,
		Success: event.Success, ErrorKind: string(event.ErrorKind), Duration: event.Duration,
	}
}
