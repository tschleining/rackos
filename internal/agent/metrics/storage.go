package metrics

import (
	"errors"
	"fmt"
	"math"
)

var (
	ErrInvalidFilesystem = errors.New(
		"invalid filesystem statistics",
	)
)

// StorageUsage represents system-wide storage
// measurements collected by the agent.
type StorageUsage struct {
	Filesystems []FilesystemUsage `json:"filesystems"`
	Disks       []DiskUsage       `json:"disks"`
	DiskRates   []DiskRate        `json:"disk_rates"`
}

// FilesystemUsage represents the capacity and
// identifying information of a mounted filesystem.
type FilesystemUsage struct {
	Source         string `json:"source"`
	MountPoint     string `json:"mount_point"`
	FilesystemType string `json:"filesystem_type"`
	ReadOnly       bool   `json:"read_only"`

	Capacity FilesystemCapacity `json:"capacity"`
}

// FilesystemCapacity describes filesystem space
// in bytes.
//
// UsedBytes represents occupied filesystem capacity.
//
// ReservedBytes represents unused capacity that is
// not available to ordinary users.
type FilesystemCapacity struct {
	TotalBytes     uint64 `json:"total_bytes"`
	FreeBytes      uint64 `json:"free_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`
	ReservedBytes  uint64 `json:"reserved_bytes"`

	UsagePercent float64 `json:"usage_percent"`
}

// DiskUsage represents cumulative disk activity
// counters for an operating-system storage device.
//
// These counters are not throughput measurements.
// Rates must be calculated using consecutive
// readings over a known sampling interval.
type DiskUsage struct {
	Device string `json:"device"`

	ReadBytes       uint64 `json:"read_bytes"`
	WriteBytes      uint64 `json:"write_bytes"`
	ReadOperations  uint64 `json:"read_operations"`
	WriteOperations uint64 `json:"write_operations"`
}

// DiskRate represents storage activity measured
// over an interval between cumulative disk readings.
type DiskRate struct {
	Device string `json:"device"`

	ReadBytesPerSecond       float64 `json:"read_bytes_per_second"`
	WriteBytesPerSecond      float64 `json:"write_bytes_per_second"`
	ReadOperationsPerSecond  float64 `json:"read_operations_per_second"`
	WriteOperationsPerSecond float64 `json:"write_operations_per_second"`

	SampleIntervalMillis int64 `json:"sample_interval_millis"`
}

// calculateFilesystemCapacity constructs a validated
// filesystem capacity measurement.
//
// Utilization represents occupied capacity divided
// by the total reported filesystem capacity.
func calculateFilesystemCapacity(
	totalBytes uint64,
	freeBytes uint64,
	availableBytes uint64,
) (FilesystemCapacity, error) {
	if totalBytes == 0 {
		return FilesystemCapacity{}, fmt.Errorf(
			"%w: total capacity is zero",
			ErrInvalidFilesystem,
		)
	}

	if freeBytes > totalBytes {
		return FilesystemCapacity{}, fmt.Errorf(
			"%w: free capacity exceeds total capacity",
			ErrInvalidFilesystem,
		)
	}

	if availableBytes > freeBytes {
		return FilesystemCapacity{}, fmt.Errorf(
			"%w: available capacity exceeds free capacity",
			ErrInvalidFilesystem,
		)
	}

	usedBytes := totalBytes - freeBytes

	reservedBytes := freeBytes - availableBytes

	usagePercent := float64(usedBytes) /
		float64(totalBytes) * 100

	return FilesystemCapacity{
		TotalBytes:     totalBytes,
		FreeBytes:      freeBytes,
		AvailableBytes: availableBytes,
		UsedBytes:      usedBytes,
		ReservedBytes:  reservedBytes,
		UsagePercent:   usagePercent,
	}, nil
}

// filesystemCapacityFromBlocks converts native
// filesystem block counters into byte measurements.
func filesystemCapacityFromBlocks(
	blockSize uint64,
	totalBlocks uint64,
	freeBlocks uint64,
	availableBlocks uint64,
) (FilesystemCapacity, error) {
	if blockSize == 0 {
		return FilesystemCapacity{}, fmt.Errorf(
			"%w: block size is zero",
			ErrInvalidFilesystem,
		)
	}

	if freeBlocks > totalBlocks {
		return FilesystemCapacity{}, fmt.Errorf(
			"%w: free blocks exceed total blocks",
			ErrInvalidFilesystem,
		)
	}

	if availableBlocks > freeBlocks {
		return FilesystemCapacity{}, fmt.Errorf(
			"%w: available blocks exceed free blocks",
			ErrInvalidFilesystem,
		)
	}

	// Since free and available blocks cannot exceed
	// total blocks, validating the total multiplication
	// also protects the other byte conversions.
	if totalBlocks > math.MaxUint64/blockSize {
		return FilesystemCapacity{}, fmt.Errorf(
			"%w: filesystem capacity overflow",
			ErrInvalidFilesystem,
		)
	}

	return calculateFilesystemCapacity(
		totalBlocks*blockSize,
		freeBlocks*blockSize,
		availableBlocks*blockSize,
	)
}
