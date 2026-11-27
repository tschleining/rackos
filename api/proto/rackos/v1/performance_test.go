package rackosv1

import (
	"math"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPerformanceSnapshotRoundTrip(t *testing.T) {
	original := testPerformanceSnapshot()

	decoded := &PerformanceSnapshot{}

	requirePerformanceRoundTrip(
		t,
		original,
		decoded,
	)

	if decoded.Cpu == nil ||
		decoded.Memory == nil ||
		decoded.Storage == nil ||
		decoded.Network == nil {
		t.Fatal("missing performance measurement group")
	}

	if len(decoded.Storage.Filesystems) != 1 {
		t.Fatal("incorrect filesystem count")
	}

	if len(decoded.Storage.Disks) != 1 {
		t.Fatal("incorrect disk count")
	}

	if len(decoded.Storage.DiskRates) != 1 {
		t.Fatal("incorrect disk rate count")
	}

	if len(decoded.Network.Interfaces) != 1 {
		t.Fatal("incorrect network interface count")
	}

	if len(decoded.Network.InterfaceRates) != 1 {
		t.Fatal("incorrect network rate count")
	}
}

func TestPerformanceSnapshotLargeCounters(t *testing.T) {
	original := testPerformanceSnapshot()

	original.Memory.TotalBytes = math.MaxUint64
	original.Memory.AvailableBytes = 0
	original.Memory.UsedBytes = math.MaxUint64
	original.Memory.UsagePercent = 100

	capacity := original.Storage.Filesystems[0].Capacity

	capacity.TotalBytes = math.MaxUint64
	capacity.FreeBytes = 0
	capacity.AvailableBytes = 0
	capacity.UsedBytes = math.MaxUint64
	capacity.ReservedBytes = 0
	capacity.UsagePercent = 100

	disk := original.Storage.Disks[0]

	disk.ReadBytes = math.MaxUint64
	disk.WriteBytes = math.MaxUint64 - 1
	disk.ReadOperations = math.MaxUint64 - 2
	disk.WriteOperations = math.MaxUint64 - 3

	iface := original.Network.Interfaces[0]

	iface.ReceivedBytes = math.MaxUint64
	iface.TransmittedBytes = math.MaxUint64 - 1
	iface.ReceivedPackets = math.MaxUint64 - 2
	iface.TransmittedPackets = math.MaxUint64 - 3
	iface.ReceiveErrors = math.MaxUint64 - 4
	iface.TransmitErrors = math.MaxUint64 - 5

	iface.ReceiveDropped = performanceUint64Ptr(
		math.MaxUint64 - 6,
	)

	iface.TransmitDropped = performanceUint64Ptr(
		math.MaxUint64 - 7,
	)

	decoded := &PerformanceSnapshot{}

	requirePerformanceRoundTrip(
		t,
		original,
		decoded,
	)

	if decoded.Memory.TotalBytes != math.MaxUint64 {
		t.Fatal("memory capacity lost integer precision")
	}

	if decoded.Storage.Disks[0].ReadBytes != math.MaxUint64 {
		t.Fatal("disk counter lost integer precision")
	}

	if decoded.Network.Interfaces[0].ReceivedBytes != math.MaxUint64 {
		t.Fatal("network counter lost integer precision")
	}
}

func TestPerformanceOptionalNetworkMeasurements(t *testing.T) {
	fields := []struct {
		name string

		set func(
			*NetworkInterfacePerformance,
			*uint64,
		)
	}{
		{
			name: "link_speed_bits_per_second",
			set: func(
				iface *NetworkInterfacePerformance,
				value *uint64,
			) {
				iface.LinkSpeedBitsPerSecond = value
			},
		},
		{
			name: "receive_dropped",
			set: func(
				iface *NetworkInterfacePerformance,
				value *uint64,
			) {
				iface.ReceiveDropped = value
			},
		},
		{
			name: "transmit_dropped",
			set: func(
				iface *NetworkInterfacePerformance,
				value *uint64,
			) {
				iface.TransmitDropped = value
			},
		},
	}

	values := []struct {
		name  string
		value *uint64
	}{
		{
			name: "unavailable",
		},
		{
			name:  "explicit zero",
			value: performanceUint64Ptr(0),
		},
		{
			name:  "positive value",
			value: performanceUint64Ptr(1000000000),
		},
		{
			name:  "maximum uint64",
			value: performanceUint64Ptr(math.MaxUint64),
		},
	}

	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			for _, tt := range values {
				t.Run(tt.name, func(t *testing.T) {
					original := &NetworkInterfacePerformance{
						Name:  "eth0",
						Index: 2,
						Up:    true,
						Mtu:   1500,
					}

					field.set(original, tt.value)

					decoded := &NetworkInterfacePerformance{}

					requirePerformanceRoundTrip(
						t,
						original,
						decoded,
					)

					message := decoded.ProtoReflect()

					descriptor := message.Descriptor().
						Fields().
						ByName(protoreflect.Name(field.name))

					if descriptor == nil {
						t.Fatal("missing field descriptor")
					}

					if descriptor.Kind() != protoreflect.Uint64Kind {
						t.Fatal("incorrect optional field type")
					}

					if !descriptor.HasPresence() {
						t.Fatal("optional field has no presence tracking")
					}

					expectedPresence := tt.value != nil

					if message.Has(descriptor) != expectedPresence {
						t.Fatal("optional field presence was not preserved")
					}

					if tt.value != nil {
						if message.Get(descriptor).Uint() != *tt.value {
							t.Fatal("optional field value changed")
						}
					}
				})
			}
		})
	}
}

func TestPerformanceOptionalZeroDiffersFromAbsent(t *testing.T) {
	absent := &NetworkInterfacePerformance{
		Name:  "eth0",
		Index: 2,
	}

	zero := &NetworkInterfacePerformance{
		Name:           "eth0",
		Index:          2,
		ReceiveDropped: performanceUint64Ptr(0),
	}

	if proto.Equal(absent, zero) {
		t.Fatal("absent and explicit zero fields are equal")
	}

	if absent.GetReceiveDropped() != 0 ||
		zero.GetReceiveDropped() != 0 {
		t.Fatal("unexpected generated getter behavior")
	}

	// Getters alone cannot distinguish absence from
	// an explicitly supplied zero value.
}

func TestPerformanceFractionalMeasurements(t *testing.T) {
	original := testPerformanceSnapshot()

	original.Cpu.UsagePercent = 24.725
	original.Memory.UsagePercent = 61.125

	diskRate := original.Storage.DiskRates[0]

	diskRate.ReadBytesPerSecond = 154823.75
	diskRate.WriteBytesPerSecond = 18239.125
	diskRate.ReadOperationsPerSecond = 12.5
	diskRate.WriteOperationsPerSecond = 3.25

	networkRate := original.Network.InterfaceRates[0]

	networkRate.ReceiveBytesPerSecond = 625010.42
	networkRate.TransmitBytesPerSecond = 128.125
	networkRate.ReceivePacketsPerSecond = 124.5
	networkRate.TransmitPacketsPerSecond = 0.25

	decoded := &PerformanceSnapshot{}

	requirePerformanceRoundTrip(
		t,
		original,
		decoded,
	)

	if decoded.Cpu.UsagePercent != 24.725 {
		t.Fatal("CPU utilization changed")
	}

	if decoded.Storage.DiskRates[0].ReadBytesPerSecond != 154823.75 {
		t.Fatal("fractional disk rate changed")
	}

	if decoded.Network.InterfaceRates[0].ReceivePacketsPerSecond != 124.5 {
		t.Fatal("fractional network packet rate changed")
	}
}

func TestPerformanceTimestampRoundTrip(t *testing.T) {
	collectedAt := time.Date(
		2026, 1, 1,
		12, 30, 45,
		123456789,
		time.UTC,
	)

	original := testPerformanceSnapshot()

	original.CollectedAt = timestamppb.New(
		collectedAt,
	)

	if err := original.CollectedAt.CheckValid(); err != nil {
		t.Fatal(err)
	}

	decoded := &PerformanceSnapshot{}

	requirePerformanceRoundTrip(
		t,
		original,
		decoded,
	)

	if err := decoded.CollectedAt.CheckValid(); err != nil {
		t.Fatal(err)
	}

	if !decoded.CollectedAt.AsTime().Equal(collectedAt) {
		t.Fatal("collection timestamp changed")
	}

	if decoded.CollectedAt.Nanos != 123456789 {
		t.Fatal("timestamp nanosecond precision was lost")
	}
}

func TestPerformanceEmptyCollections(t *testing.T) {
	original := testPerformanceSnapshot()

	original.Storage = &StoragePerformance{}
	original.Network = &NetworkPerformance{}

	decoded := &PerformanceSnapshot{}

	requirePerformanceRoundTrip(
		t,
		original,
		decoded,
	)

	if decoded.Storage == nil {
		t.Fatal("empty storage message was lost")
	}

	if decoded.Network == nil {
		t.Fatal("empty network message was lost")
	}

	if len(decoded.Storage.Filesystems) != 0 ||
		len(decoded.Storage.Disks) != 0 ||
		len(decoded.Storage.DiskRates) != 0 {
		t.Fatal("unexpected storage records")
	}

	if len(decoded.Network.Interfaces) != 0 ||
		len(decoded.Network.InterfaceRates) != 0 {
		t.Fatal("unexpected network records")
	}
}

func TestPerformanceMissingMeasurementGroups(t *testing.T) {
	original := &PerformanceSnapshot{}

	decoded := &PerformanceSnapshot{}

	requirePerformanceRoundTrip(
		t,
		original,
		decoded,
	)

	if decoded.CollectedAt != nil ||
		decoded.Cpu != nil ||
		decoded.Memory != nil ||
		decoded.Storage != nil ||
		decoded.Network != nil {
		t.Fatal("missing measurement groups became present")
	}
}

func TestReportPerformanceRequestRoundTrip(t *testing.T) {
	original := &ReportPerformanceRequest{
		DeviceId: "e2a104d8-9c72-4b31-8fa2-63001b24c9e8",
		SampleId: "7a91d0c4e8524b36a39f186d027c5e81",
		Snapshot: testPerformanceSnapshot(),
	}

	decoded := &ReportPerformanceRequest{}

	requirePerformanceRoundTrip(
		t,
		original,
		decoded,
	)

	if decoded.DeviceId != original.DeviceId {
		t.Fatal("device identity changed")
	}

	if decoded.SampleId != original.SampleId {
		t.Fatal("sample identity changed")
	}

	if decoded.Snapshot == nil {
		t.Fatal("performance snapshot was lost")
	}
}

func TestReportPerformanceResponseRoundTrip(t *testing.T) {
	receivedAt := time.Date(
		2026, 1, 1,
		12, 30, 46,
		125000000,
		time.UTC,
	)

	original := &ReportPerformanceResponse{
		Accepted: true,
		SampleId: "7a91d0c4e8524b36a39f186d027c5e81",
		ReceivedAt: timestamppb.New(
			receivedAt,
		),
	}

	decoded := &ReportPerformanceResponse{}

	requirePerformanceRoundTrip(
		t,
		original,
		decoded,
	)

	if !decoded.Accepted {
		t.Fatal("acknowledgement was lost")
	}

	if decoded.SampleId != original.SampleId {
		t.Fatal("acknowledged sample identity changed")
	}

	if decoded.ReceivedAt == nil {
		t.Fatal("receipt timestamp was lost")
	}

	if err := decoded.ReceivedAt.CheckValid(); err != nil {
		t.Fatal(err)
	}

	if !decoded.ReceivedAt.AsTime().Equal(receivedAt) {
		t.Fatal("receipt timestamp changed")
	}
}

func TestReportPerformanceMethodContract(t *testing.T) {
	const expectedPath = "/rackos.v1.AgentService/ReportPerformance"

	if AgentService_ReportPerformance_FullMethodName != expectedPath {
		t.Fatal("unexpected performance RPC method path")
	}

	service := File_rackos_v1_agent_proto.Services().
		ByName("AgentService")

	if service == nil {
		t.Fatal("missing agent service descriptor")
	}

	method := service.Methods().ByName(
		"ReportPerformance",
	)

	if method == nil {
		t.Fatal("missing performance RPC descriptor")
	}

	if method.Input().FullName() != "rackos.v1.ReportPerformanceRequest" {
		t.Fatal("incorrect RPC request type")
	}

	if method.Output().FullName() != "rackos.v1.ReportPerformanceResponse" {
		t.Fatal("incorrect RPC response type")
	}

	if method.IsStreamingClient() ||
		method.IsStreamingServer() {
		t.Fatal("performance reporting must be a unary RPC")
	}
}

func TestReportPerformanceRequestFieldContract(t *testing.T) {
	descriptor := (&ReportPerformanceRequest{}).
		ProtoReflect().
		Descriptor()

	fields := []struct {
		name   protoreflect.Name
		number protoreflect.FieldNumber
		kind   protoreflect.Kind
	}{
		{
			name:   "device_id",
			number: 1,
			kind:   protoreflect.StringKind,
		},
		{
			name:   "sample_id",
			number: 2,
			kind:   protoreflect.StringKind,
		},
		{
			name:   "snapshot",
			number: 3,
			kind:   protoreflect.MessageKind,
		},
	}

	if descriptor.Fields().Len() != len(fields) {
		t.Fatal("unexpected request field count")
	}

	for _, expected := range fields {
		field := descriptor.Fields().ByName(expected.name)

		if field == nil {
			t.Fatalf("missing field %q", expected.name)
		}

		if field.Number() != expected.number {
			t.Fatalf("incorrect field number for %q", expected.name)
		}

		if field.Kind() != expected.kind {
			t.Fatalf("incorrect field type for %q", expected.name)
		}
	}

	if descriptor.Fields().ByName("agent_token") != nil {
		t.Fatal("credentials must not be embedded in the request")
	}
}

// requirePerformanceRoundTrip verifies that a message
// survives binary Protobuf serialization unchanged.
func requirePerformanceRoundTrip(
	t *testing.T,
	original proto.Message,
	decoded proto.Message,
) {
	t.Helper()

	data, err := proto.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	if err := proto.Unmarshal(data, decoded); err != nil {
		t.Fatal(err)
	}

	if !proto.Equal(original, decoded) {
		t.Fatal("decoded message differs from original")
	}
}

func performanceUint64Ptr(value uint64) *uint64 {
	return &value
}

// testPerformanceSnapshot returns a representative
// complete performance snapshot for protocol testing.
func testPerformanceSnapshot() *PerformanceSnapshot {
	return &PerformanceSnapshot{
		CollectedAt: timestamppb.New(
			time.Date(
				2026, 1, 1,
				12, 30, 45, 0,
				time.UTC,
			),
		),

		Cpu: &CPUPerformance{
			UsagePercent:         25.5,
			SampleIntervalMillis: 1000,
		},

		Memory: &MemoryPerformance{
			TotalBytes:     16384,
			AvailableBytes: 4096,
			UsedBytes:      12288,
			UsagePercent:   75,
		},

		Storage: &StoragePerformance{
			Filesystems: []*FilesystemPerformance{
				{
					Source:         "/dev/sda1",
					MountPoint:     "/",
					FilesystemType: "ext4",

					Capacity: &FilesystemCapacityPerformance{
						TotalBytes:     1000000,
						FreeBytes:      400000,
						AvailableBytes: 350000,
						UsedBytes:      600000,
						ReservedBytes:  50000,
						UsagePercent:   60,
					},
				},
			},

			Disks: []*DiskPerformance{
				{
					Device:          "sda",
					ReadBytes:       524288000,
					WriteBytes:      262144000,
					ReadOperations:  42000,
					WriteOperations: 21000,
				},
			},

			DiskRates: []*DiskRate{
				{
					Device:                   "sda",
					ReadBytesPerSecond:       1048576,
					WriteBytesPerSecond:      524288,
					ReadOperationsPerSecond:  25.5,
					WriteOperationsPerSecond: 12.5,
					SampleIntervalMillis:     1100,
				},
			},
		},

		Network: &NetworkPerformance{
			Interfaces: []*NetworkInterfacePerformance{
				{
					Name:  "eth0",
					Index: 2,
					Up:    true,
					Mtu:   1500,

					LinkSpeedBitsPerSecond: performanceUint64Ptr(
						1000000000,
					),

					ReceivedBytes:      524288000,
					ReceivedPackets:    420000,
					ReceiveErrors:      2,
					TransmittedBytes:   262144000,
					TransmittedPackets: 210000,
					TransmitErrors:     1,

					ReceiveDropped:  performanceUint64Ptr(0),
					TransmitDropped: nil,
				},
			},

			InterfaceRates: []*NetworkInterfaceRate{
				{
					Name:                     "eth0",
					Index:                    2,
					ReceiveBytesPerSecond:    6000.5,
					TransmitBytesPerSecond:   3000.25,
					ReceivePacketsPerSecond:  60.5,
					TransmitPacketsPerSecond: 30.25,
					SampleIntervalMillis:     1250,
				},
			},
		},
	}
}
