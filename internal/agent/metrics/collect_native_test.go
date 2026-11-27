//go:build linux || darwin

package metrics

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestCollectNative(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	startedAt := time.Now()

	snapshot, err := Collect(ctx)
	if err != nil {
		t.Fatal(err)
	}

	finishedAt := time.Now()

	requireMetricPercent(
		t,
		snapshot.CPU.UsagePercent,
		snapshot.CPU.UsagePercent,
	)

	if snapshot.CPU.SampleIntervalMillis <
		DefaultCPUSampleInterval.Milliseconds() {
		t.Fatal("CPU sampling interval is too short")
	}

	if snapshot.Memory.TotalBytes == 0 {
		t.Fatal("memory capacity is zero")
	}

	if snapshot.Memory.AvailableBytes >
		snapshot.Memory.TotalBytes {
		t.Fatal("invalid available memory")
	}

	if snapshot.Memory.UsedBytes !=
		snapshot.Memory.TotalBytes-
			snapshot.Memory.AvailableBytes {
		t.Fatal("memory accounting is inconsistent")
	}

	expectedUsage := float64(
		snapshot.Memory.UsedBytes,
	) / float64(
		snapshot.Memory.TotalBytes,
	) * 100

	requireMetricPercent(
		t,
		snapshot.Memory.UsagePercent,
		expectedUsage,
	)

	if snapshot.CollectedAt.IsZero() {
		t.Fatal("collection timestamp is missing")
	}

	if snapshot.CollectedAt.Location() != time.UTC {
		t.Fatal("collection timestamp is not UTC")
	}

	if snapshot.CollectedAt.Before(startedAt) ||
		snapshot.CollectedAt.After(finishedAt) {
		t.Fatal("timestamp outside collection window")
	}

	data, err := json.MarshalIndent(
		snapshot,
		"",
		"  ",
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf(
		"Native metrics snapshot:\n%s",
		data,
	)
}

func TestCollectNativeStorage(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	snapshot, err := Collect(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.Storage == nil {
		t.Fatal("storage measurements are missing")
	}

	storage := snapshot.Storage

	if storage.Filesystems == nil ||
		storage.Disks == nil ||
		storage.DiskRates == nil {
		t.Fatal("storage contains uninitialized slices")
	}

	for _, filesystem := range storage.Filesystems {
		if filesystem.Capacity.TotalBytes == 0 {
			t.Fatal("filesystem capacity is zero")
		}

		requireFilesystemAccounting(
			t,
			filesystem.Capacity,
		)

		expected := float64(
			filesystem.Capacity.UsedBytes,
		) / float64(
			filesystem.Capacity.TotalBytes,
		) * 100

		requireMetricPercent(
			t,
			filesystem.Capacity.UsagePercent,
			expected,
		)

		t.Logf(
			"filesystem=%q source=%q used=%d available=%d usage=%.2f%%",
			filesystem.MountPoint,
			filesystem.Source,
			filesystem.Capacity.UsedBytes,
			filesystem.Capacity.AvailableBytes,
			filesystem.Capacity.UsagePercent,
		)
	}

	disks, err := indexDiskUsage(storage.Disks)
	if err != nil {
		t.Fatal(err)
	}

	for _, rate := range storage.DiskRates {
		if _, exists := disks[rate.Device]; !exists {
			t.Fatal("disk rate has no corresponding disk")
		}

		if rate.SampleIntervalMillis < 1000 {
			t.Fatal("disk sampling interval is too short")
		}

		values := []float64{
			rate.ReadBytesPerSecond,
			rate.WriteBytesPerSecond,
			rate.ReadOperationsPerSecond,
			rate.WriteOperationsPerSecond,
		}

		for _, value := range values {
			requireDiskRateValue(t, value, value)
		}

		t.Logf(
			"disk=%q read=%.2f B/s write=%.2f B/s read_iops=%.2f write_iops=%.2f interval=%dms",
			rate.Device,
			rate.ReadBytesPerSecond,
			rate.WriteBytesPerSecond,
			rate.ReadOperationsPerSecond,
			rate.WriteOperationsPerSecond,
			rate.SampleIntervalMillis,
		)
	}

	t.Logf(
		"Collected %d filesystems, %d disks, and %d disk rates",
		len(storage.Filesystems),
		len(storage.Disks),
		len(storage.DiskRates),
	)
}

func TestCollectNativeNetwork(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	snapshot, err := Collect(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.Storage == nil {
		t.Fatal("missing storage measurements")
	}

	if snapshot.Network == nil {
		t.Fatal("missing network measurements")
	}

	network := snapshot.Network

	if network.Interfaces == nil {
		t.Fatal("network interfaces slice is nil")
	}

	if network.InterfaceRates == nil {
		t.Fatal("network rates slice is nil")
	}

	if err := validateNetworkInterfaces(
		network.Interfaces,
	); err != nil {
		t.Fatal(err)
	}

	interfaces := make(
		map[string]NetworkInterfaceUsage,
		len(network.Interfaces),
	)

	for i, iface := range network.Interfaces {
		if i > 0 &&
			network.Interfaces[i-1].Name >= iface.Name {
			t.Fatal("invalid interface ordering")
		}

		interfaces[iface.Name] = iface

		if iface.LinkSpeedBitsPerSecond != nil &&
			*iface.LinkSpeedBitsPerSecond == 0 {
			t.Fatal("invalid zero link speed")
		}

		if (iface.Loopback || !iface.Up) &&
			iface.LinkSpeedBitsPerSecond != nil {
			t.Fatal("unexpected interface link speed")
		}
	}

	seenRates := make(map[string]struct{})

	for i, rate := range network.InterfaceRates {
		if i > 0 &&
			network.InterfaceRates[i-1].Name >= rate.Name {
			t.Fatal("invalid network rate ordering")
		}

		iface, exists := interfaces[rate.Name]

		if !exists || iface.Index != rate.Index {
			t.Fatal("rate does not match a current interface")
		}

		if _, exists := seenRates[rate.Name]; exists {
			t.Fatal("duplicate network rate")
		}

		seenRates[rate.Name] = struct{}{}

		if rate.SampleIntervalMillis < 1000 {
			t.Fatal("network sampling interval is too short")
		}

		values := []float64{
			rate.ReceiveBytesPerSecond,
			rate.TransmitBytesPerSecond,
			rate.ReceivePacketsPerSecond,
			rate.TransmitPacketsPerSecond,
		}

		for _, value := range values {
			// Validate finite, nonnegative measurements.
			requireNetworkRateValue(t, value, value)
		}

		t.Logf(
			"interface=%q index=%d rx=%.2f B/s tx=%.2f B/s rx_packets=%.2f/s tx_packets=%.2f/s interval=%dms",
			rate.Name,
			rate.Index,
			rate.ReceiveBytesPerSecond,
			rate.TransmitBytesPerSecond,
			rate.ReceivePacketsPerSecond,
			rate.TransmitPacketsPerSecond,
			rate.SampleIntervalMillis,
		)
	}

	if snapshot.CollectedAt.Location() != time.UTC {
		t.Fatal("collection timestamp is not UTC")
	}

	t.Logf(
		"Collected %d network interfaces and %d network rates",
		len(network.Interfaces),
		len(network.InterfaceRates),
	)
}
