package rpc

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"inferencerig/backends"
	"inferencerig/core/control"
	"inferencerig/core/modeldownload"
	"inferencerig/core/profiles"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/core/rpc/gen/v1/controlv1connect"
	coreruntime "inferencerig/core/runtime"
	"inferencerig/core/signals"
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
	items, err := s.manager.ListProfiles(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	out := make([]*controlv1.Profile, 0, len(items))
	for _, summary := range items {
		doc, err := s.manager.GetProfile(ctx, summary.Name)
		if err != nil {
			return nil, rpcError(err)
		}
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
	if req.GetName() == "" || req.GetProfileYaml() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "profile name and YAML are required"))
	}
	doc, err := s.manager.PutProfile(ctx, req.GetName(), req.GetProfileYaml(), req.GetCreateOnly())
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
	result, err := s.manager.InstallBackend(ctx, req.GetBackend(), backends.InstallOptions{
		Version: req.GetVersion(), Upgrade: req.GetUpgrade(), Force: req.GetForce(),
	})
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.InstallBackendResponse{
		Ok: true, Version: result.Version, Path: result.Path,
		Changed: result.Changed, Message: result.Message,
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
	if req.GetProfile() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "profile is required"))
	}
	status, err := s.manager.RuntimeStatus(ctx, req.GetProfile())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.GetRuntimeStatusResponse{Ok: true, Status: statusProto(status)}, nil
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
	if req.GetProfile() == "" {
		return nil, rpcError(control.Errorf(control.ErrorInvalidInput, "profile is required"))
	}
	job, err := s.manager.StartDownload(ctx, req.GetProfile(), req.GetForce())
	if err != nil {
		return nil, rpcError(err)
	}
	return &controlv1.StartModelDownloadResponse{Ok: true, Download: downloadProto(job)}, nil
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

func backendProto(backend backends.Backend) *controlv1.BackendInfo {
	capabilities := backend.Capabilities()
	return &controlv1.BackendInfo{
		Name: backend.Name(),
		Capabilities: &controlv1.BackendCapabilities{
			SingleFileArtifacts: capabilities.SingleFileArtifacts,
			MultiFileArtifacts:  capabilities.MultiFileArtifacts,
			DiscreteVram:        capabilities.DiscreteVRAM, UnifiedMemory: capabilities.UnifiedMemory,
			ManagedInstall: capabilities.ManagedInstall, SingleActiveProfile: capabilities.SingleActiveProfile,
		},
	}
}

func profileProto(doc profiles.ProfileDocument) *controlv1.Profile {
	p := doc.Effective
	return &controlv1.Profile{
		Name: doc.Name, Backend: p.Backend, ProfileYaml: doc.ProfileYAML,
		ModelSource: p.Model.Source, ModelReference: p.Model.Reference,
		Host: p.Listen.Host, Port: int32(p.Listen.Port),
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
	}
}

func signalsProto(snapshot signals.Snapshot) *controlv1.Signals {
	return &controlv1.Signals{
		CapturedAt: snapshot.CapturedAt, TotalMemoryBytes: snapshot.Memory.TotalBytes,
		AvailableMemoryBytes: snapshot.Memory.AvailableBytes,
		LogicalCpuCores:      int32(snapshot.CPU.LogicalCores),
		CpuUsedPercent:       snapshot.CPU.UsedPercent, Warnings: snapshot.Warnings,
	}
}

func eventProto(event control.Event) *controlv1.Event {
	return &controlv1.Event{
		Id: event.ID, Time: event.Time, Action: event.Action,
		Success: event.Success, ErrorKind: string(event.ErrorKind), Duration: event.Duration,
	}
}
