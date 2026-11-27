package client

import (
	"math"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/agent/metrics"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPerformanceToProto(t *testing.T) {
	snapshot := testPerformanceConversionSnapshot()

	actual, err := performanceToProto(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	expected := &rackosv1.PerformanceSnapshot{
		CollectedAt: timestamppb.New(
			snapshot.CollectedAt.UTC(),
		),

		Cpu: &rackosv1.CPUPerformance{
			UsagePercent:         25.5,
			SampleIntervalMillis: 1000,
		},

		Memory: &rackosv1.MemoryPerformance{
			TotalBytes:     16384,
			AvailableBytes: 4096,
			UsedBytes:      12288,
			UsagePercent:   75,
		},

		Storage: &rackosv1.StoragePerformance{
			Filesystems: []*rackosv1.FilesystemPerformance{
				{
					Source:         "/dev/sda1",
					MountPoint:     "/",
					FilesystemType: "ext4",
					ReadOnly:       true,

					Capacity: &rackosv1.FilesystemCapacityPerformance{
						TotalBytes:     1000000,
						FreeBytes:      400000,
						AvailableBytes: 350000,
						UsedBytes:      600000,
						ReservedBytes:  50000,
						UsagePercent:   60,
					},
				},
			},

			Disks: []*rackosv1.DiskPerformance{
				{
					Device:          "sda",
					ReadBytes:       524288000,
					WriteBytes:      262144000,
					ReadOperations:  42000,
					WriteOperations: 21000,
				},
			},

			DiskRates: []*rackosv1.DiskRate{
				{
					Device: "sda",

					ReadBytesPerSecond:  1048576.5,
					WriteBytesPerSecond: 524288.25,

					ReadOperationsPerSecond:  25.5,
					WriteOperationsPerSecond: 12.5,

					SampleIntervalMillis: 1100,
				},
			},
		},

		Network: &rackosv1.NetworkPerformance{
			Interfaces: []*rackosv1.NetworkInterfacePerformance{
				{
					Name:  "eth0",
					Index: 2,
					Up:    true,
					Mtu:   1500,

					LinkSpeedBitsPerSecond: testPerformanceUint64Ptr(
						1000000000,
					),

					ReceivedBytes:      524288000,
					ReceivedPackets:    420000,
					ReceiveErrors:      2,
					TransmittedBytes:   262144000,
					TransmittedPackets: 210000,
					TransmitErrors:     1,

					ReceiveDropped:  testPerformanceUint64Ptr(0),
					TransmitDropped: testPerformanceUint64Ptr(3),
				},
				{
					Name:     "lo",
					Index:    1,
					Up:       true,
					Loopback: true,
					Mtu:      65536,

					ReceivedBytes:      1000,
					ReceivedPackets:    10,
					TransmittedBytes:   1000,
					TransmittedPackets: 10,
				},
			},

			InterfaceRates: []*rackosv1.NetworkInterfaceRate{
				{
					Name:  "eth0",
					Index: 2,

					ReceiveBytesPerSecond:  6000.5,
					TransmitBytesPerSecond: 3000.25,

					ReceivePacketsPerSecond:  60.5,
					TransmitPacketsPerSecond: 30.25,

					SampleIntervalMillis: 1250,
				},
			},
		},
	}

	if !proto.Equal(actual, expected) {
		t.Fatalf(
			"unexpected performance conversion:\nexpected: %v\nactual: %v",
			expected,
			actual,
		)
	}
}

func TestPerformanceToProtoTimestamp(t *testing.T) {
	snapshot := testPerformanceConversionSnapshot()

	snapshot.CollectedAt = time.Date(
		2026, 1, 1,
		12, 30, 45,
		123456789,
		time.FixedZone("EST", -5*60*60),
	)

	message, err := performanceToProto(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	if err := message.CollectedAt.CheckValid(); err != nil {
		t.Fatal(err)
	}

	actual := message.CollectedAt.AsTime()

	if !actual.Equal(snapshot.CollectedAt) {
		t.Fatal("collection timestamp changed")
	}

	if actual.Location() != time.UTC {
		t.Fatal("converted timestamp is not UTC")
	}

	if message.CollectedAt.Nanos != 123456789 {
		t.Fatal("timestamp nanosecond precision was lost")
	}
}

func TestPerformanceToProtoInvalidTimestamp(t *testing.T) {
	tests := []struct {
		name  string
		value time.Time
	}{
		{
			name: "zero timestamp",
		},
		{
			name:  "Unix epoch",
			value: time.Unix(0, 0),
		},
		{
			name:  "sub-millisecond Unix timestamp",
			value: time.Unix(0, 1),
		},
		{
			name:  "before Unix epoch",
			value: time.Unix(-1, 0),
		},
		{
			name: "outside Protobuf timestamp range",
			value: time.Date(
				10000, 1, 1,
				0, 0, 0, 0,
				time.UTC,
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := testPerformanceConversionSnapshot()

			snapshot.CollectedAt = tt.value

			message, err := performanceToProto(snapshot)

			if err == nil {
				t.Fatal("expected invalid timestamp error")
			}

			if message != nil {
				t.Fatal("invalid timestamp returned message")
			}
		})
	}
}

func TestPerformanceToProtoMissingMeasurements(t *testing.T) {
	tests := []struct {
		name   string
		change func(*metrics.Snapshot)
	}{
		{
			name: "missing storage",
			change: func(snapshot *metrics.Snapshot) {
				snapshot.Storage = nil
			},
		},
		{
			name: "missing network",
			change: func(snapshot *metrics.Snapshot) {
				snapshot.Network = nil
			},
		},
		{
			name: "missing storage and network",
			change: func(snapshot *metrics.Snapshot) {
				snapshot.Storage = nil
				snapshot.Network = nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := testPerformanceConversionSnapshot()

			tt.change(&snapshot)

			message, err := performanceToProto(snapshot)

			if err == nil {
				t.Fatal("expected missing measurements error")
			}

			if message != nil {
				t.Fatal("incomplete snapshot returned message")
			}
		})
	}
}

func TestPerformanceToProtoEmptyCollections(t *testing.T) {
	snapshot := testPerformanceConversionSnapshot()

	snapshot.Storage = &metrics.StorageUsage{}
	snapshot.Network = &metrics.NetworkUsage{}

	message, err := performanceToProto(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	if message.Storage == nil ||
		message.Network == nil {
		t.Fatal("empty measurement groups were lost")
	}

	if message.Storage.Filesystems == nil ||
		len(message.Storage.Filesystems) != 0 {
		t.Fatal("expected initialized empty filesystem slice")
	}

	if message.Storage.Disks == nil ||
		len(message.Storage.Disks) != 0 {
		t.Fatal("expected initialized empty disk slice")
	}

	if message.Storage.DiskRates == nil ||
		len(message.Storage.DiskRates) != 0 {
		t.Fatal("expected initialized empty disk rate slice")
	}

	if message.Network.Interfaces == nil ||
		len(message.Network.Interfaces) != 0 {
		t.Fatal("expected initialized empty interface slice")
	}

	if message.Network.InterfaceRates == nil ||
		len(message.Network.InterfaceRates) != 0 {
		t.Fatal("expected initialized empty network rate slice")
	}

	data, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}

	decoded := &rackosv1.PerformanceSnapshot{}

	if err := proto.Unmarshal(data, decoded); err != nil {
		t.Fatal(err)
	}

	if !proto.Equal(message, decoded) {
		t.Fatal("empty collection round trip failed")
	}

	if decoded.Storage == nil ||
		decoded.Network == nil {
		t.Fatal("measurement groups lost during serialization")
	}
}

func TestPerformanceToProtoLargeCounters(t *testing.T) {
	snapshot := testPerformanceConversionSnapshot()

	snapshot.Memory.TotalBytes = math.MaxUint64
	snapshot.Memory.AvailableBytes = 0
	snapshot.Memory.UsedBytes = math.MaxUint64
	snapshot.Memory.UsagePercent = 100

	capacity := &snapshot.Storage.Filesystems[0].Capacity

	capacity.TotalBytes = math.MaxUint64
	capacity.FreeBytes = 0
	capacity.AvailableBytes = 0
	capacity.UsedBytes = math.MaxUint64
	capacity.ReservedBytes = 0
	capacity.UsagePercent = 100

	disk := &snapshot.Storage.Disks[0]

	disk.ReadBytes = math.MaxUint64
	disk.WriteBytes = math.MaxUint64 - 1
	disk.ReadOperations = math.MaxUint64 - 2
	disk.WriteOperations = math.MaxUint64 - 3

	iface := &snapshot.Network.Interfaces[0]

	iface.ReceivedBytes = math.MaxUint64
	iface.TransmittedBytes = math.MaxUint64 - 1
	iface.ReceivedPackets = math.MaxUint64 - 2
	iface.TransmittedPackets = math.MaxUint64 - 3
	iface.ReceiveErrors = math.MaxUint64 - 4
	iface.TransmitErrors = math.MaxUint64 - 5

	iface.ReceiveDropped = testPerformanceUint64Ptr(
		math.MaxUint64 - 6,
	)

	iface.TransmitDropped = testPerformanceUint64Ptr(
		math.MaxUint64 - 7,
	)

	message, err := performanceToProto(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	actualDisk := message.Storage.Disks[0]
	actualIface := message.Network.Interfaces[0]

	if message.Memory.TotalBytes != math.MaxUint64 ||
		message.Memory.UsedBytes != math.MaxUint64 {
		t.Fatal("memory capacity lost precision")
	}

	if message.Storage.Filesystems[0].Capacity.TotalBytes != math.MaxUint64 ||
		message.Storage.Filesystems[0].Capacity.UsedBytes != math.MaxUint64 {
		t.Fatal("filesystem capacity lost precision")
	}

	if actualDisk.ReadBytes != disk.ReadBytes ||
		actualDisk.WriteBytes != disk.WriteBytes ||
		actualDisk.ReadOperations != disk.ReadOperations ||
		actualDisk.WriteOperations != disk.WriteOperations {
		t.Fatal("disk counters lost precision")
	}

	if actualIface.ReceivedBytes != iface.ReceivedBytes ||
		actualIface.TransmittedBytes != iface.TransmittedBytes ||
		actualIface.ReceivedPackets != iface.ReceivedPackets ||
		actualIface.TransmittedPackets != iface.TransmittedPackets ||
		actualIface.ReceiveErrors != iface.ReceiveErrors ||
		actualIface.TransmitErrors != iface.TransmitErrors {
		t.Fatal("network counters lost precision")
	}

	if actualIface.ReceiveDropped == nil ||
		*actualIface.ReceiveDropped != *iface.ReceiveDropped {
		t.Fatal("receive drops lost precision")
	}

	if actualIface.TransmitDropped == nil ||
		*actualIface.TransmitDropped != *iface.TransmitDropped {
		t.Fatal("transmit drops lost precision")
	}

	data, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}

	decoded := &rackosv1.PerformanceSnapshot{}

	if err := proto.Unmarshal(data, decoded); err != nil {
		t.Fatal(err)
	}

	if !proto.Equal(message, decoded) {
		t.Fatal("large counter serialization failed")
	}
}

func TestPerformanceToProtoOptionalMeasurements(t *testing.T) {
	tests := []struct {
		name  string
		value *uint64
	}{
		{
			name: "unavailable",
		},
		{
			name:  "explicit zero",
			value: testPerformanceUint64Ptr(0),
		},
		{
			name:  "positive value",
			value: testPerformanceUint64Ptr(1000000000),
		},
		{
			name:  "maximum uint64",
			value: testPerformanceUint64Ptr(math.MaxUint64),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := testPerformanceConversionSnapshot()

			iface := &snapshot.Network.Interfaces[0]

			iface.LinkSpeedBitsPerSecond = tt.value
			iface.ReceiveDropped = tt.value
			iface.TransmitDropped = tt.value

			message, err := performanceToProto(snapshot)
			if err != nil {
				t.Fatal(err)
			}

			actual := message.Network.Interfaces[0]

			requirePerformanceOptionalUint64(
				t,
				actual.LinkSpeedBitsPerSecond,
				tt.value,
			)

			requirePerformanceOptionalUint64(
				t,
				actual.ReceiveDropped,
				tt.value,
			)

			requirePerformanceOptionalUint64(
				t,
				actual.TransmitDropped,
				tt.value,
			)
		})
	}
}

func TestPerformanceToProtoIndependentOwnership(t *testing.T) {
	snapshot := testPerformanceConversionSnapshot()

	message, err := performanceToProto(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	expected := proto.Clone(message)

	// Mutate the source after conversion.
	snapshot.CPU.UsagePercent = 99
	snapshot.Memory.UsedBytes = 100

	snapshot.Storage.Filesystems[0].Source = "changed"
	snapshot.Storage.Filesystems[0].Capacity.TotalBytes = 10

	snapshot.Storage.Disks[0].ReadBytes = 999
	snapshot.Storage.DiskRates[0].ReadBytesPerSecond = 999

	snapshot.Network.Interfaces[0].Name = "changed"
	snapshot.Network.Interfaces[0].ReceivedBytes = 999

	*snapshot.Network.Interfaces[0].LinkSpeedBitsPerSecond = 100
	*snapshot.Network.Interfaces[0].ReceiveDropped = 999
	*snapshot.Network.Interfaces[0].TransmitDropped = 999

	snapshot.Network.InterfaceRates[0].ReceiveBytesPerSecond = 999

	snapshot.CollectedAt = snapshot.CollectedAt.Add(
		time.Hour,
	)

	if !proto.Equal(message, expected) {
		t.Fatal("source mutation changed transport message")
	}
}

func TestPerformanceToProtoDoesNotModifySource(t *testing.T) {
	snapshot := testPerformanceConversionSnapshot()

	message, err := performanceToProto(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	// Mutate the converted transport message.
	message.Cpu.UsagePercent = 99
	message.Memory.UsedBytes = 100

	message.Storage.Filesystems[0].Capacity.TotalBytes = 10
	message.Storage.Disks[0].ReadBytes = 999
	message.Storage.DiskRates[0].ReadBytesPerSecond = 999

	message.Network.Interfaces[0].Name = "changed"

	*message.Network.Interfaces[0].LinkSpeedBitsPerSecond = 100
	*message.Network.Interfaces[0].ReceiveDropped = 999
	*message.Network.Interfaces[0].TransmitDropped = 999

	message.Network.InterfaceRates[0].ReceiveBytesPerSecond = 999

	message.CollectedAt.Seconds++

	expected := testPerformanceConversionSnapshot()

	actual, err := performanceToProto(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	expectedMessage, err := performanceToProto(expected)
	if err != nil {
		t.Fatal(err)
	}

	if !proto.Equal(actual, expectedMessage) {
		t.Fatal("transport mutation changed source snapshot")
	}
}

func TestPerformanceToProtoCollectionOrder(t *testing.T) {
	snapshot := testPerformanceConversionSnapshot()

	snapshot.Storage.Disks = []metrics.DiskUsage{
		{Device: "sdc"},
		{Device: "sda"},
		{Device: "sdb"},
	}

	snapshot.Network.Interfaces = []metrics.NetworkInterfaceUsage{
		{Name: "utun0", Index: 12},
		{Name: "en0", Index: 6},
		{Name: "lo0", Index: 1},
	}

	message, err := performanceToProto(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	if len(message.Storage.Disks) != 3 {
		t.Fatal("incorrect disk count")
	}

	for i, disk := range snapshot.Storage.Disks {
		if message.Storage.Disks[i].Device != disk.Device {
			t.Fatal("disk record order changed")
		}
	}

	if len(message.Network.Interfaces) != 3 {
		t.Fatal("incorrect interface count")
	}

	for i, iface := range snapshot.Network.Interfaces {
		actual := message.Network.Interfaces[i]

		if actual.Name != iface.Name ||
			actual.Index != iface.Index {
			t.Fatal("interface record order changed")
		}
	}
}

func TestPerformanceToProtoBinaryRoundTrip(t *testing.T) {
	snapshot := testPerformanceConversionSnapshot()

	message, err := performanceToProto(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	data, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}

	if len(data) == 0 {
		t.Fatal("empty serialized performance snapshot")
	}

	decoded := &rackosv1.PerformanceSnapshot{}

	if err := proto.Unmarshal(data, decoded); err != nil {
		t.Fatal(err)
	}

	if !proto.Equal(message, decoded) {
		t.Fatal("converted snapshot round trip failed")
	}

	t.Logf(
		"Serialized fixture size: %d bytes",
		len(data),
	)
}

func requirePerformanceOptionalUint64(
	t *testing.T,
	actual *uint64,
	expected *uint64,
) {
	t.Helper()

	if expected == nil {
		if actual != nil {
			t.Fatal("unavailable measurement became present")
		}

		return
	}

	if actual == nil {
		t.Fatal("available measurement became absent")
	}

	if *actual != *expected {
		t.Fatalf(
			"expected %d, got %d",
			*expected,
			*actual,
		)
	}

	if actual == expected {
		t.Fatal("optional measurement pointer was shared")
	}
}

func testPerformanceUint64Ptr(value uint64) *uint64 {
	return &value
}

// testPerformanceConversionSnapshot returns a complete
// representative local performance snapshot.
func testPerformanceConversionSnapshot() metrics.Snapshot {
	return metrics.Snapshot{
		CPU: metrics.CPUUsage{
			UsagePercent:         25.5,
			SampleIntervalMillis: 1000,
		},

		Memory: metrics.MemoryUsage{
			TotalBytes:     16384,
			AvailableBytes: 4096,
			UsedBytes:      12288,
			UsagePercent:   75,
		},

		Storage: &metrics.StorageUsage{
			Filesystems: []metrics.FilesystemUsage{
				{
					Source:         "/dev/sda1",
					MountPoint:     "/",
					FilesystemType: "ext4",
					ReadOnly:       true,

					Capacity: metrics.FilesystemCapacity{
						TotalBytes:     1000000,
						FreeBytes:      400000,
						AvailableBytes: 350000,
						UsedBytes:      600000,
						ReservedBytes:  50000,
						UsagePercent:   60,
					},
				},
			},

			Disks: []metrics.DiskUsage{
				{
					Device:          "sda",
					ReadBytes:       524288000,
					WriteBytes:      262144000,
					ReadOperations:  42000,
					WriteOperations: 21000,
				},
			},

			DiskRates: []metrics.DiskRate{
				{
					Device: "sda",

					ReadBytesPerSecond:  1048576.5,
					WriteBytesPerSecond: 524288.25,

					ReadOperationsPerSecond:  25.5,
					WriteOperationsPerSecond: 12.5,

					SampleIntervalMillis: 1100,
				},
			},
		},

		Network: &metrics.NetworkUsage{
			Interfaces: []metrics.NetworkInterfaceUsage{
				{
					Name:  "eth0",
					Index: 2,
					Up:    true,
					MTU:   1500,

					LinkSpeedBitsPerSecond: testPerformanceUint64Ptr(
						1000000000,
					),

					ReceivedBytes:      524288000,
					ReceivedPackets:    420000,
					ReceiveErrors:      2,
					TransmittedBytes:   262144000,
					TransmittedPackets: 210000,
					TransmitErrors:     1,

					ReceiveDropped:  testPerformanceUint64Ptr(0),
					TransmitDropped: testPerformanceUint64Ptr(3),
				},
				{
					Name:     "lo",
					Index:    1,
					Up:       true,
					Loopback: true,
					MTU:      65536,

					ReceivedBytes:      1000,
					ReceivedPackets:    10,
					TransmittedBytes:   1000,
					TransmittedPackets: 10,
				},
			},

			InterfaceRates: []metrics.NetworkInterfaceRate{
				{
					Name:  "eth0",
					Index: 2,

					ReceiveBytesPerSecond:  6000.5,
					TransmitBytesPerSecond: 3000.25,

					ReceivePacketsPerSecond:  60.5,
					TransmitPacketsPerSecond: 30.25,

					SampleIntervalMillis: 1250,
				},
			},
		},

		CollectedAt: time.Date(
			2026, 1, 1,
			12, 30, 45,
			123456789,
			time.UTC,
		),
	}
}
