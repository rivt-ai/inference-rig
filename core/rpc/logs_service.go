package rpc

import (
	"context"
	"errors"
	"os"
	"time"

	"connectrpc.com/connect"

	"inferencerig/core/control"
	controlv1 "inferencerig/core/rpc/gen/v1"
	"inferencerig/platform/audit"
)

// defaultLogLines is the tail size used when a caller leaves lines unset. It is
// large enough to cover a failed startup without shipping a whole log file.
const defaultLogLines = 500

func (s *ControlService) GetLogs(_ context.Context, req *controlv1.GetLogsRequest) (*controlv1.GetLogsResponse, error) {
	service, err := logService(req.GetService())
	if err != nil {
		return nil, rpcError(err)
	}
	exists, err := audit.LogExists(service)
	if err != nil {
		return nil, rpcError(logError(err))
	}
	if !exists {
		return nil, rpcError(control.Errorf(control.ErrorNotFound, "no logs for service %q", service))
	}
	text, err := audit.TailLogLines(service, tailLines(req.GetLines()))
	if err != nil {
		return nil, rpcError(logError(err))
	}
	return &controlv1.GetLogsResponse{Ok: true, Service: service, Text: text}, nil
}

func (s *ControlService) WatchLogs(ctx context.Context, req *controlv1.WatchLogsRequest, stream *connect.ServerStream[controlv1.WatchLogsResponse]) error {
	service, err := logService(req.GetService())
	if err != nil {
		return rpcError(err)
	}
	exists, err := audit.LogExists(service)
	if err != nil {
		return rpcError(logError(err))
	}
	if !exists {
		return rpcError(control.Errorf(control.ErrorNotFound, "no logs for service %q", service))
	}
	// FollowLog polls, so a line reaches the client within one poll interval;
	// it returns nil once ctx is cancelled by the client disconnecting.
	err = audit.FollowLog(ctx, service, func(line string) error {
		return stream.Send(&controlv1.WatchLogsResponse{Service: service, Line: line})
	})
	if err != nil {
		// A send failure is the client going away, not a server fault.
		if ctx.Err() != nil {
			return nil
		}
		return rpcError(logError(err))
	}
	return nil
}

func (s *ControlService) ListLogArchives(context.Context, *controlv1.ListLogArchivesRequest) (*controlv1.ListLogArchivesResponse, error) {
	archives, err := audit.ListArchives()
	if err != nil {
		return nil, rpcError(logError(err))
	}
	out := make([]*controlv1.LogArchive, 0, len(archives))
	for _, archive := range archives {
		out = append(out, archiveProto(archive))
	}
	return &controlv1.ListLogArchivesResponse{Ok: true, Archives: out}, nil
}

func (s *ControlService) GetLogArchive(_ context.Context, req *controlv1.GetLogArchiveRequest) (*controlv1.GetLogArchiveResponse, error) {
	id, err := archiveID(req.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	text, err := audit.TailArchive(id, tailLines(req.GetLines()))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, rpcError(control.Errorf(control.ErrorNotFound, "log archive %q not found", id))
		}
		return nil, rpcError(logError(err))
	}
	return &controlv1.GetLogArchiveResponse{Ok: true, Service: audit.ArchiveService(id), Text: text}, nil
}

func (s *ControlService) DeleteLogArchive(_ context.Context, req *controlv1.DeleteLogArchiveRequest) (*controlv1.DeleteLogArchiveResponse, error) {
	id, err := archiveID(req.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	deleted, err := audit.RemoveArchive(id)
	if err != nil {
		return nil, rpcError(logError(err))
	}
	if !deleted {
		return nil, rpcError(control.Errorf(control.ErrorNotFound, "log archive %q not found", id))
	}
	return &controlv1.DeleteLogArchiveResponse{Ok: true, Deleted: 1}, nil
}

func (s *ControlService) ClearLogArchives(context.Context, *controlv1.ClearLogArchivesRequest) (*controlv1.ClearLogArchivesResponse, error) {
	deleted, err := audit.ClearArchives()
	if err != nil {
		return nil, rpcError(logError(err))
	}
	return &controlv1.ClearLogArchivesResponse{Ok: true, Deleted: int32(deleted)}, nil
}

// logService and archiveID validate a network-supplied value before it is used
// to derive a filesystem path, rejecting separators and traversal outright.
func logService(service string) (string, error) {
	return checkedPathPart(service, "service", audit.ValidLogName)
}

func archiveID(id string) (string, error) {
	return checkedPathPart(id, "log archive ID", audit.ValidArchiveID)
}

func checkedPathPart(value, label string, valid func(string) bool) (string, error) {
	if value == "" {
		return "", control.Errorf(control.ErrorInvalidInput, "%s is required", label)
	}
	if !valid(value) {
		return "", control.Errorf(control.ErrorInvalidInput, "invalid %s %q", label, value)
	}
	return value, nil
}

// tailLines clamps a caller-supplied line count into the range the tail reader
// accepts, so a hostile or careless value cannot drive an unbounded read.
func tailLines(lines int32) int {
	if lines <= 0 {
		return defaultLogLines
	}
	return min(int(lines), audit.MaxTailLines)
}

// logError gives filesystem failures from the audit package a control kind;
// without it they would surface as an opaque unknown code.
func logError(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return control.CoreError(control.ErrorNotFound, err.Error(), err)
	}
	if errors.Is(err, os.ErrPermission) {
		return control.CoreError(control.ErrorPermission, err.Error(), err)
	}
	return control.CoreError(control.ErrorRuntime, err.Error(), err)
}

func archiveProto(archive audit.Archive) *controlv1.LogArchive {
	return &controlv1.LogArchive{
		Id: archive.ID, Service: archive.Service, SizeBytes: archive.SizeBytes,
		ArchivedAt: archive.ArchivedAt.Format(time.RFC3339),
	}
}
