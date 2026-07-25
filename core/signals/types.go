package signals

import (
	"context"
	"time"

	"inferencerig/core/runtime"
)

type Snapshot struct {
	CapturedAt string                `json:"captured_at"`
	Host       HostStats             `json:"host"`
	Memory     MemoryStats           `json:"memory"`
	Disks      []DiskStats           `json:"disks,omitempty"`
	CPU        CPUStats              `json:"cpu"`
	Runtime    []RuntimeProcessStats `json:"runtime,omitempty"`
	Warnings   []string              `json:"warnings,omitempty"`
}

type MachineSnapshot struct {
	Memory   MemoryStats
	Warnings []string
}

type HostStats struct {
	Hostname string `json:"hostname,omitempty"`
	OS       string `json:"os,omitempty"`
	Platform string `json:"platform,omitempty"`
}

type MemoryStats struct {
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	UsedPercent    float64 `json:"used_percent"`
}

type DiskStats struct {
	Label       string  `json:"label"`
	Path        string  `json:"path"`
	TotalBytes  uint64  `json:"total_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type DiskTarget struct {
	Label string
	Path  string
}

type CPUStats struct {
	LogicalCores int     `json:"logical_cores"`
	UsedPercent  float64 `json:"used_percent"`
}

type RuntimeProcessStats struct {
	Name       string  `json:"name"`
	PID        int     `json:"pid"`
	RSSBytes   uint64  `json:"rss_bytes"`
	CPUPercent float64 `json:"cpu_percent"`
	Command    string  `json:"command,omitempty"`
}

type Collector interface {
	Snapshot(ctx context.Context) (Snapshot, error)
}

type MachineCollector interface {
	Machine(ctx context.Context) (MachineSnapshot, error)
}

type RuntimeStatusProvider interface {
	Status(context.Context) (runtime.Status, error)
}

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}
