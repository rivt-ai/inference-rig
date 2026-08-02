package rpc

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"inferencerig/backends"
	"inferencerig/core/control"
	"inferencerig/core/modelcatalog"
	"inferencerig/core/modeldownload"
	"inferencerig/core/profiles"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
	coreruntime "inferencerig/core/runtime"
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
	if err := requireField(req.GetName(), "profile name"); err != nil {
		return nil, err
	}
	doc, err := s.manager.GetProfile(ctx, req.GetName())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.GetProfileResponse{Ok: true, Profile: profileProto(doc)}, nil
}

func (s *ControlService) PutProfile(ctx context.Context, req *controlv1.PutProfileRequest) (*controlv1.PutProfileResponse, error) {
	if err := requireField(req.GetName(), "profile name"); err != nil {
		return nil, err
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
	if err := requireField(req.GetName(), "profile name"); err != nil {
		return nil, err
	}
	if _, err := s.manager.DeleteProfile(ctx, req.GetName()); err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.DeleteProfileResponse{Ok: true}, nil
}

func (s *ControlService) InstallBackend(ctx context.Context, req *controlv1.InstallBackendRequest) (*controlv1.InstallBackendResponse, error) {
	if err := requireField(req.GetBackend(), "backend"); err != nil {
		return nil, err
	}
	return installResponse(s.manager.InstallBackend(ctx, req.GetBackend(), backends.InstallOptions{
		Version: req.GetVersion(), Upgrade: req.GetUpgrade(), Force: req.GetForce(),
	}))
}

func (s *ControlService) RollbackBackend(ctx context.Context, req *controlv1.RollbackBackendRequest) (*controlv1.InstallBackendResponse, error) {
	if err := requireField(req.GetBackend(), "backend"); err != nil {
		return nil, err
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
	if err := requireField(req.GetBackend(), "backend"); err != nil {
		return nil, err
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
	result, status, err := s.runtimeAction(ctx, req.GetProfile(),
		func(ctx context.Context, profile string) (coreruntime.CommandResult, error) {
			return s.manager.StartRuntime(ctx, profile, req.GetReplace())
		})
	return &controlv1.StartRuntimeResponse{Ok: err == nil, Result: result, Status: status}, err
}

// ResetRuntimes stops every runtime and clears the active backend so a host can
// switch to a different one.
func (s *ControlService) ResetRuntimes(ctx context.Context, _ *controlv1.ResetRuntimesRequest) (*controlv1.ResetRuntimesResponse, error) {
	result, err := s.manager.ResetRuntimes(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.ResetRuntimesResponse{Ok: true, Result: commandResultProto(result)}, nil
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
	if err := requireField(req.GetProfile(), "profile"); err != nil {
		return nil, err
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
	var job modeldownload.Job
	var err error
	if req.GetProfile() == "" {
		job, err = s.manager.StartCatalogDownload(
			ctx, req.GetBackend(), req.GetReference(), req.GetVariantReference(), req.GetForce(),
		)
	} else {
		job, err = s.manager.StartDownload(ctx, req.GetProfile(), req.GetForce())
	}
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
	if err := requireField(req.GetId(), "download ID"); err != nil {
		return nil, err
	}
	job, err := s.manager.GetDownload(ctx, req.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.GetModelDownloadResponse{Ok: true, Download: downloadProto(job)}, nil
}

func (s *ControlService) CancelModelDownload(ctx context.Context, req *controlv1.CancelModelDownloadRequest) (*controlv1.CancelModelDownloadResponse, error) {
	if err := requireField(req.GetId(), "download ID"); err != nil {
		return nil, err
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
	if err := requireField(req.GetBackend(), "backend"); err != nil {
		return nil, err
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
	if err := requireField(req.GetBackend(), "backend"); err != nil {
		return nil, err
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
	if err := requireField(req.GetName(), "profile name"); err != nil {
		return nil, err
	}
	if err := s.manager.CleanupProfile(ctx, req.GetName()); err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.CleanupProfileResponse{Ok: true}, nil
}

func (s *ControlService) SetProfileAutostart(ctx context.Context, req *controlv1.SetProfileAutostartRequest) (*controlv1.SetProfileAutostartResponse, error) {
	if err := requireField(req.GetName(), "profile name"); err != nil {
		return nil, err
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
	if err := requireField(req.GetProfile(), "profile"); err != nil {
		return nil, err
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
		StartupServices: info.StartupServices, ActiveBackend: info.ActiveBackend,
		Build: &controlv1.BuildInfo{
			Version: buildinfo.Version, Commit: buildinfo.Commit, CommitTime: buildinfo.CommitTime,
		},
	}, nil
}

func (s *ControlService) GetBackendParams(ctx context.Context, req *controlv1.GetBackendParamsRequest) (*controlv1.GetBackendParamsResponse, error) {
	if err := requireField(req.GetBackend(), "backend"); err != nil {
		return nil, err
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

func (s *ControlService) runtimeAction(
	ctx context.Context,
	profile string,
	action func(context.Context, string) (coreruntime.CommandResult, error),
) (*controlv1.CommandResult, *controlv1.RuntimeStatus, error) {
	if err := requireField(profile, "profile"); err != nil {
		return nil, nil, err
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
