package client

import (
	"errors"
	"fmt"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/agent/metrics"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// performanceToProto converts a locally collected
// performance snapshot into the agent reporting protocol.
//
// The returned message owns its records and optional
// scalar values independently of the original snapshot.
func performanceToProto(
	snapshot metrics.Snapshot,
) (*rackosv1.PerformanceSnapshot, error) {
	if snapshot.CollectedAt.IsZero() {
		return nil, errors.New(
			"performance collection timestamp is required",
		)
	}

	collectedAt := timestamppb.New(
		snapshot.CollectedAt.UTC(),
	)

	if err := collectedAt.CheckValid(); err != nil {
		return nil, fmt.Errorf(
			"invalid performance collection timestamp: %w",
			err,
		)
	}

	if snapshot.CollectedAt.UnixMilli() <= 0 {
		return nil, errors.New(
			"performance collection timestamp must be positive",
		)
	}

	if snapshot.Storage == nil {
		return nil, errors.New(
			"performance storage measurements are required",
		)
	}

	if snapshot.Network == nil {
		return nil, errors.New(
			"performance network measurements are required",
		)
	}

	return &rackosv1.PerformanceSnapshot{
		CollectedAt: collectedAt,

		Cpu: &rackosv1.CPUPerformance{
			UsagePercent: snapshot.CPU.UsagePercent,
			SampleIntervalMillis: snapshot.CPU.
				SampleIntervalMillis,
		},

		Memory: &rackosv1.MemoryPerformance{
			TotalBytes:     snapshot.Memory.TotalBytes,
			AvailableBytes: snapshot.Memory.AvailableBytes,
			UsedBytes:      snapshot.Memory.UsedBytes,
			UsagePercent:   snapshot.Memory.UsagePercent,
		},

		Storage: storagePerformanceToProto(snapshot.Storage),
		Network: networkPerformanceToProto(snapshot.Network),
	}, nil
}

// storagePerformanceToProto converts filesystem
// capacity, cumulative disk activity, and disk rates.
func storagePerformanceToProto(
	storage *metrics.StorageUsage,
) *rackosv1.StoragePerformance {
	message := &rackosv1.StoragePerformance{
		Filesystems: make(
			[]*rackosv1.FilesystemPerformance,
			0,
			len(storage.Filesystems),
		),

		Disks: make(
			[]*rackosv1.DiskPerformance,
			0,
			len(storage.Disks),
		),

		DiskRates: make(
			[]*rackosv1.DiskRate,
			0,
			len(storage.DiskRates),
		),
	}

	for _, filesystem := range storage.Filesystems {
		message.Filesystems = append(
			message.Filesystems,
			filesystemPerformanceToProto(filesystem),
		)
	}

	for _, disk := range storage.Disks {
		message.Disks = append(
			message.Disks,
			diskPerformanceToProto(disk),
		)
	}

	for _, rate := range storage.DiskRates {
		message.DiskRates = append(
			message.DiskRates,
			diskRateToProto(rate),
		)
	}

	return message
}

// filesystemPerformanceToProto converts filesystem
// metadata and capacity measurements.
func filesystemPerformanceToProto(
	filesystem metrics.FilesystemUsage,
) *rackosv1.FilesystemPerformance {
	return &rackosv1.FilesystemPerformance{
		Source:         filesystem.Source,
		MountPoint:     filesystem.MountPoint,
		FilesystemType: filesystem.FilesystemType,
		ReadOnly:       filesystem.ReadOnly,

		Capacity: &rackosv1.FilesystemCapacityPerformance{
			TotalBytes:     filesystem.Capacity.TotalBytes,
			FreeBytes:      filesystem.Capacity.FreeBytes,
			AvailableBytes: filesystem.Capacity.AvailableBytes,
			UsedBytes:      filesystem.Capacity.UsedBytes,
			ReservedBytes:  filesystem.Capacity.ReservedBytes,
			UsagePercent:   filesystem.Capacity.UsagePercent,
		},
	}
}

// diskPerformanceToProto converts cumulative disk
// activity counters without narrowing their values.
func diskPerformanceToProto(
	disk metrics.DiskUsage,
) *rackosv1.DiskPerformance {
	return &rackosv1.DiskPerformance{
		Device:          disk.Device,
		ReadBytes:       disk.ReadBytes,
		WriteBytes:      disk.WriteBytes,
		ReadOperations:  disk.ReadOperations,
		WriteOperations: disk.WriteOperations,
	}
}

// diskRateToProto converts calculated disk activity.
func diskRateToProto(
	rate metrics.DiskRate,
) *rackosv1.DiskRate {
	return &rackosv1.DiskRate{
		Device: rate.Device,

		ReadBytesPerSecond:  rate.ReadBytesPerSecond,
		WriteBytesPerSecond: rate.WriteBytesPerSecond,

		ReadOperationsPerSecond:  rate.ReadOperationsPerSecond,
		WriteOperationsPerSecond: rate.WriteOperationsPerSecond,

		SampleIntervalMillis: rate.SampleIntervalMillis,
	}
}

// networkPerformanceToProto converts network
// interface metadata, cumulative counters, and rates.
func networkPerformanceToProto(
	network *metrics.NetworkUsage,
) *rackosv1.NetworkPerformance {
	message := &rackosv1.NetworkPerformance{
		Interfaces: make(
			[]*rackosv1.NetworkInterfacePerformance,
			0,
			len(network.Interfaces),
		),

		InterfaceRates: make(
			[]*rackosv1.NetworkInterfaceRate,
			0,
			len(network.InterfaceRates),
		),
	}

	for _, iface := range network.Interfaces {
		message.Interfaces = append(
			message.Interfaces,
			networkInterfaceToProto(iface),
		)
	}

	for _, rate := range network.InterfaceRates {
		message.InterfaceRates = append(
			message.InterfaceRates,
			networkInterfaceRateToProto(rate),
		)
	}

	return message
}

// networkInterfaceToProto converts an individual
// network interface and its cumulative counters.
func networkInterfaceToProto(
	iface metrics.NetworkInterfaceUsage,
) *rackosv1.NetworkInterfacePerformance {
	return &rackosv1.NetworkInterfacePerformance{
		Name:     iface.Name,
		Index:    iface.Index,
		Up:       iface.Up,
		Loopback: iface.Loopback,
		Mtu:      iface.MTU,

		LinkSpeedBitsPerSecond: copyPerformanceUint64(
			iface.LinkSpeedBitsPerSecond,
		),

		ReceivedBytes:   iface.ReceivedBytes,
		ReceivedPackets: iface.ReceivedPackets,
		ReceiveErrors:   iface.ReceiveErrors,

		TransmittedBytes:   iface.TransmittedBytes,
		TransmittedPackets: iface.TransmittedPackets,
		TransmitErrors:     iface.TransmitErrors,

		ReceiveDropped: copyPerformanceUint64(
			iface.ReceiveDropped,
		),

		TransmitDropped: copyPerformanceUint64(
			iface.TransmitDropped,
		),
	}
}

// networkInterfaceRateToProto converts calculated
// network throughput and packet rates.
func networkInterfaceRateToProto(
	rate metrics.NetworkInterfaceRate,
) *rackosv1.NetworkInterfaceRate {
	return &rackosv1.NetworkInterfaceRate{
		Name:  rate.Name,
		Index: rate.Index,

		ReceiveBytesPerSecond:  rate.ReceiveBytesPerSecond,
		TransmitBytesPerSecond: rate.TransmitBytesPerSecond,

		ReceivePacketsPerSecond:  rate.ReceivePacketsPerSecond,
		TransmitPacketsPerSecond: rate.TransmitPacketsPerSecond,

		SampleIntervalMillis: rate.SampleIntervalMillis,
	}
}

// copyPerformanceUint64 preserves optional scalar
// presence while avoiding shared mutable pointers.
func copyPerformanceUint64(value *uint64) *uint64 {
	if value == nil {
		return nil
	}

	copied := *value

	return &copied
}
