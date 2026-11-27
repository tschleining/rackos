package metrics

import (
	"errors"
	"time"
)

var (
	ErrInvalidCPUCounters = errors.New(
		"invalid CPU counters",
	)

	ErrInvalidMemory = errors.New(
		"invalid memory information",
	)

	ErrUnsupportedPlatform = errors.New(
		"metrics collection is unsupported on this platform",
	)
)

const DefaultCPUSampleInterval = time.Second

// Snapshot represents a complete system
// performance measurement.
type Snapshot struct {
	CPU     CPUUsage      `json:"cpu"`
	Memory  MemoryUsage   `json:"memory"`
	Storage *StorageUsage `json:"storage,omitempty"`

	CollectedAt time.Time `json:"collected_at"`
}

// CPUUsage represents aggregate processor utilization
// across all logical CPUs.
//
// UsagePercent ranges from zero to one hundred.
type CPUUsage struct {
	UsagePercent float64 `json:"usage_percent"`

	SampleIntervalMillis int64 `json:"sample_interval_millis"`
}

// MemoryUsage represents system memory capacity and
// estimated memory availability.
//
// UsedBytes is derived from TotalBytes minus
// AvailableBytes.
type MemoryUsage struct {
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`

	UsagePercent float64 `json:"usage_percent"`
}

// cpuCounters represents cumulative CPU activity.
//
// Platform-specific collectors normalize their native
// CPU counters into busy and idle values before
// calculating utilization.
type cpuCounters struct {
	Busy uint64
	Idle uint64
}

// calculateCPUUsage calculates aggregate utilization
// between two consecutive cumulative CPU readings.
func calculateCPUUsage(
	previous cpuCounters,
	current cpuCounters,
) (float64, error) {
	// Counter resets or regressions must not cause
	// unsigned integer underflow.
	if current.Busy < previous.Busy ||
		current.Idle < previous.Idle {
		return 0, ErrInvalidCPUCounters
	}

	busyDelta := current.Busy - previous.Busy
	idleDelta := current.Idle - previous.Idle

	// Convert before adding to avoid integer overflow.
	totalDelta := float64(busyDelta) +
		float64(idleDelta)

	if totalDelta == 0 {
		return 0, ErrInvalidCPUCounters
	}

	usage := float64(busyDelta) /
		totalDelta * 100

	return usage, nil
}

// calculateMemoryUsage constructs a memory measurement
// from total capacity and estimated available memory.
func calculateMemoryUsage(
	totalBytes uint64,
	availableBytes uint64,
) (MemoryUsage, error) {
	if totalBytes == 0 ||
		availableBytes > totalBytes {
		return MemoryUsage{}, ErrInvalidMemory
	}

	usedBytes := totalBytes - availableBytes

	usage := float64(usedBytes) /
		float64(totalBytes) * 100

	return MemoryUsage{
		TotalBytes:     totalBytes,
		AvailableBytes: availableBytes,
		UsedBytes:      usedBytes,
		UsagePercent:   usage,
	}, nil
}
