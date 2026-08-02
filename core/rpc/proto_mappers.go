package rpc

import (
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	"inferencerig/backends"
	"inferencerig/core/control"
	"inferencerig/core/modelcatalog"
	"inferencerig/core/modeldownload"
	"inferencerig/core/profiles"
	controlv1 "inferencerig/core/rpc/gen/v1"
	coreruntime "inferencerig/core/runtime"
	"inferencerig/core/signals"
)

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
		OperationId: event.OperationID, Profile: event.Profile,
		Backend: event.Backend, State: string(event.State), Recovery: string(event.Recovery),
		Detail: event.Detail,
	}
}
