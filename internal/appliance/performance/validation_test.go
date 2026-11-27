package performance

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestValidateRequest(t *testing.T) {
	now := testPerformanceTime()

	request := testPerformanceRequest(now)

	before := proto.Clone(request)

	if err := ValidateRequest(request, now); err != nil {
		t.Fatal(err)
	}

	if !proto.Equal(request, before) {
		t.Fatal("validation modified the original request")
	}
}

func TestValidateRequestMissing(t *testing.T) {
	requireInvalidPerformance(
		t,
		ValidateRequest(nil, testPerformanceTime()),
	)
}

func TestValidateRequestIdentity(t *testing.T) {
	tests := []struct {
		name   string
		change func(*rackosv1.ReportPerformanceRequest)
	}{
		{
			name: "missing device identity",
			change: func(r *rackosv1.ReportPerformanceRequest) {
				r.DeviceId = ""
			},
		},
		{
			name: "missing sample identity",
			change: func(r *rackosv1.ReportPerformanceRequest) {
				r.SampleId = ""
			},
		},
		{
			name: "missing snapshot",
			change: func(r *rackosv1.ReportPerformanceRequest) {
				r.Snapshot = nil
			},
		},
	}

	now := testPerformanceTime()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := testPerformanceRequest(now)
			tt.change(request)

			requireInvalidPerformance(
				t,
				ValidateRequest(request, now),
			)
		})
	}
}

func TestValidateSampleID(t *testing.T) {
	valid := []string{
		"7a91d0c4e8524b36a39f186d027c5e81",
		strings.Repeat("a", 32),
		strings.Repeat("0", 32),
		strings.Repeat("f", 32),
	}

	for _, value := range valid {
		if err := ValidateSampleID(value); err != nil {
			t.Fatal(err)
		}
	}

	invalidIDs := []string{
		"",
		strings.Repeat("a", 31),
		strings.Repeat("a", 33),
		strings.Repeat("A", 32),
		strings.Repeat("g", 32),
		strings.Repeat(" ", 32),
		strings.Repeat("a", 31) + "\n",
		"7a91d0c4-e852-4b36-a39f-186d027c5e81",
	}

	for _, value := range invalidIDs {
		requireInvalidPerformance(
			t,
			ValidateSampleID(value),
		)
	}
}

func TestValidatePerformanceTimestamp(t *testing.T) {
	now := testPerformanceTime()

	tests := []struct {
		name    string
		value   *timestamppb.Timestamp
		invalid bool
	}{
		{
			name:  "current timestamp",
			value: timestamppb.New(now),
		},
		{
			name: "historical timestamp",
			value: timestamppb.New(
				now.Add(-365 * 24 * time.Hour),
			),
		},
		{
			name: "maximum future skew",
			value: timestamppb.New(
				now.Add(5 * time.Minute),
			),
		},
		{
			name:    "missing timestamp",
			invalid: true,
		},
		{
			name:    "Unix epoch",
			value:   timestamppb.New(time.Unix(0, 0)),
			invalid: true,
		},
		{
			name:    "sub-millisecond timestamp",
			value:   timestamppb.New(time.Unix(0, 1)),
			invalid: true,
		},
		{
			name:    "negative timestamp",
			value:   timestamppb.New(time.Unix(-1, 0)),
			invalid: true,
		},
		{
			name: "excessive future skew",
			value: timestamppb.New(
				now.Add(5*time.Minute + time.Nanosecond),
			),
			invalid: true,
		},
		{
			name: "invalid nanoseconds",
			value: &timestamppb.Timestamp{
				Seconds: now.Unix(),
				Nanos:   1000000000,
			},
			invalid: true,
		},
		{
			name: "invalid seconds",
			value: &timestamppb.Timestamp{
				Seconds: math.MaxInt64,
			},
			invalid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := testPerformanceRequest(now)
			request.Snapshot.CollectedAt = tt.value

			err := ValidateRequest(request, now)

			if tt.invalid {
				requireInvalidPerformance(t, err)
				return
			}

			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidatePerformanceMissingGroups(t *testing.T) {
	tests := []struct {
		name   string
		change func(*rackosv1.PerformanceSnapshot)
	}{
		{
			name: "missing CPU",
			change: func(s *rackosv1.PerformanceSnapshot) {
				s.Cpu = nil
			},
		},
		{
			name: "missing memory",
			change: func(s *rackosv1.PerformanceSnapshot) {
				s.Memory = nil
			},
		},
		{
			name: "missing storage",
			change: func(s *rackosv1.PerformanceSnapshot) {
				s.Storage = nil
			},
		},
		{
			name: "missing network",
			change: func(s *rackosv1.PerformanceSnapshot) {
				s.Network = nil
			},
		},
	}

	now := testPerformanceTime()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := testPerformanceRequest(now)
			tt.change(request.Snapshot)

			requireInvalidPerformance(
				t,
				ValidateRequest(request, now),
			)
		})
	}
}

func TestValidatePerformanceCPU(t *testing.T) {
	now := testPerformanceTime()

	for _, value := range []float64{
		0,
		25.5,
		100,
	} {
		request := testPerformanceRequest(now)
		request.Snapshot.Cpu.UsagePercent = value

		if err := ValidateRequest(request, now); err != nil {
			t.Fatal(err)
		}
	}

	for _, value := range []float64{
		-1,
		100.0001,
		math.NaN(),
		math.Inf(1),
		math.Inf(-1),
	} {
		request := testPerformanceRequest(now)
		request.Snapshot.Cpu.UsagePercent = value

		requireInvalidPerformance(
			t,
			ValidateRequest(request, now),
		)
	}

	for _, interval := range []int64{
		-1,
		0,
		60001,
		math.MaxInt64,
	} {
		request := testPerformanceRequest(now)
		request.Snapshot.Cpu.SampleIntervalMillis = interval

		requireInvalidPerformance(
			t,
			ValidateRequest(request, now),
		)
	}

	for _, interval := range []int64{
		1,
		1000,
		60000,
	} {
		request := testPerformanceRequest(now)
		request.Snapshot.Cpu.SampleIntervalMillis = interval

		if err := ValidateRequest(request, now); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidatePerformanceMemory(t *testing.T) {
	tests := []struct {
		name   string
		change func(*rackosv1.MemoryPerformance)
	}{
		{
			name: "zero total",
			change: func(m *rackosv1.MemoryPerformance) {
				m.TotalBytes = 0
			},
		},
		{
			name: "available exceeds total",
			change: func(m *rackosv1.MemoryPerformance) {
				m.AvailableBytes = m.TotalBytes + 1
			},
		},
		{
			name: "inconsistent used bytes",
			change: func(m *rackosv1.MemoryPerformance) {
				m.UsedBytes++
			},
		},
		{
			name: "inconsistent percentage",
			change: func(m *rackosv1.MemoryPerformance) {
				m.UsagePercent = 50
			},
		},
		{
			name: "NaN percentage",
			change: func(m *rackosv1.MemoryPerformance) {
				m.UsagePercent = math.NaN()
			},
		},
	}

	now := testPerformanceTime()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := testPerformanceRequest(now)
			tt.change(request.Snapshot.Memory)

			requireInvalidPerformance(
				t,
				ValidateRequest(request, now),
			)
		})
	}
}

func TestValidatePerformanceFilesystemCapacity(t *testing.T) {
	tests := []struct {
		name   string
		change func(*rackosv1.FilesystemCapacityPerformance)
	}{
		{
			name: "zero total",
			change: func(c *rackosv1.FilesystemCapacityPerformance) {
				c.TotalBytes = 0
			},
		},
		{
			name: "free exceeds total",
			change: func(c *rackosv1.FilesystemCapacityPerformance) {
				c.FreeBytes = c.TotalBytes + 1
			},
		},
		{
			name: "available exceeds free",
			change: func(c *rackosv1.FilesystemCapacityPerformance) {
				c.AvailableBytes = c.FreeBytes + 1
			},
		},
		{
			name: "inconsistent used bytes",
			change: func(c *rackosv1.FilesystemCapacityPerformance) {
				c.UsedBytes++
			},
		},
		{
			name: "inconsistent reserved bytes",
			change: func(c *rackosv1.FilesystemCapacityPerformance) {
				c.ReservedBytes++
			},
		},
		{
			name: "inconsistent utilization",
			change: func(c *rackosv1.FilesystemCapacityPerformance) {
				c.UsagePercent = 50
			},
		},
		{
			name: "infinite utilization",
			change: func(c *rackosv1.FilesystemCapacityPerformance) {
				c.UsagePercent = math.Inf(1)
			},
		},
	}

	now := testPerformanceTime()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := testPerformanceRequest(now)

			capacity := request.Snapshot.Storage.
				Filesystems[0].Capacity

			tt.change(capacity)

			requireInvalidPerformance(
				t,
				ValidateRequest(request, now),
			)
		})
	}
}

func TestValidatePerformancePercentageTolerance(t *testing.T) {
	now := testPerformanceTime()

	request := testPerformanceRequest(now)

	request.Snapshot.Memory.UsagePercent += 0.0000005

	if err := ValidateRequest(request, now); err != nil {
		t.Fatal(err)
	}

	request.Snapshot.Memory.UsagePercent += 0.00001

	requireInvalidPerformance(
		t,
		ValidateRequest(request, now),
	)
}

func TestValidatePerformanceLargeCounters(t *testing.T) {
	now := testPerformanceTime()

	request := testPerformanceRequest(now)
	snapshot := request.Snapshot

	snapshot.Memory.TotalBytes = math.MaxUint64
	snapshot.Memory.AvailableBytes = 0
	snapshot.Memory.UsedBytes = math.MaxUint64
	snapshot.Memory.UsagePercent = 100

	capacity := snapshot.Storage.Filesystems[0].Capacity

	capacity.TotalBytes = math.MaxUint64
	capacity.FreeBytes = 0
	capacity.AvailableBytes = 0
	capacity.UsedBytes = math.MaxUint64
	capacity.ReservedBytes = 0
	capacity.UsagePercent = 100

	disk := snapshot.Storage.Disks[0]

	disk.ReadBytes = math.MaxUint64
	disk.WriteBytes = math.MaxUint64
	disk.ReadOperations = math.MaxUint64
	disk.WriteOperations = math.MaxUint64

	iface := snapshot.Network.Interfaces[0]

	iface.ReceivedBytes = math.MaxUint64
	iface.TransmittedBytes = math.MaxUint64
	iface.ReceivedPackets = math.MaxUint64
	iface.TransmittedPackets = math.MaxUint64
	iface.ReceiveErrors = math.MaxUint64
	iface.TransmitErrors = math.MaxUint64

	iface.ReceiveDropped = testUint64Ptr(math.MaxUint64)
	iface.TransmitDropped = testUint64Ptr(math.MaxUint64)

	if err := ValidateRequest(request, now); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePerformanceStorageIdentity(t *testing.T) {
	tests := []struct {
		name   string
		change func(*rackosv1.StoragePerformance)
	}{
		{
			name: "nil filesystem",
			change: func(s *rackosv1.StoragePerformance) {
				s.Filesystems[0] = nil
			},
		},
		{
			name: "nil filesystem capacity",
			change: func(s *rackosv1.StoragePerformance) {
				s.Filesystems[0].Capacity = nil
			},
		},
		{
			name: "missing mount point",
			change: func(s *rackosv1.StoragePerformance) {
				s.Filesystems[0].MountPoint = ""
			},
		},
		{
			name: "missing filesystem type",
			change: func(s *rackosv1.StoragePerformance) {
				s.Filesystems[0].FilesystemType = ""
			},
		},
		{
			name: "nil disk",
			change: func(s *rackosv1.StoragePerformance) {
				s.Disks[0] = nil
			},
		},
		{
			name: "missing device",
			change: func(s *rackosv1.StoragePerformance) {
				s.Disks[0].Device = ""
			},
		},
		{
			name: "duplicate disk",
			change: func(s *rackosv1.StoragePerformance) {
				s.Disks = append(s.Disks, s.Disks[0])
			},
		},
		{
			name: "nil disk rate",
			change: func(s *rackosv1.StoragePerformance) {
				s.DiskRates[0] = nil
			},
		},
		{
			name: "missing rate identity",
			change: func(s *rackosv1.StoragePerformance) {
				s.DiskRates[0].Device = ""
			},
		},
		{
			name: "unmatched disk rate",
			change: func(s *rackosv1.StoragePerformance) {
				s.DiskRates[0].Device = "sdb"
			},
		},
		{
			name: "duplicate disk rate",
			change: func(s *rackosv1.StoragePerformance) {
				s.DiskRates = append(s.DiskRates, s.DiskRates[0])
			},
		},
	}

	now := testPerformanceTime()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := testPerformanceRequest(now)
			tt.change(request.Snapshot.Storage)

			requireInvalidPerformance(
				t,
				ValidateRequest(request, now),
			)
		})
	}
}

func TestValidatePerformanceNetworkIdentity(t *testing.T) {
	tests := []struct {
		name   string
		change func(*rackosv1.NetworkPerformance)
	}{
		{
			name: "nil interface",
			change: func(n *rackosv1.NetworkPerformance) {
				n.Interfaces[0] = nil
			},
		},
		{
			name: "empty interface name",
			change: func(n *rackosv1.NetworkPerformance) {
				n.Interfaces[0].Name = ""
			},
		},
		{
			name: "zero interface index",
			change: func(n *rackosv1.NetworkPerformance) {
				n.Interfaces[0].Index = 0
			},
		},
		{
			name: "duplicate interface name",
			change: func(n *rackosv1.NetworkPerformance) {
				n.Interfaces = append(
					n.Interfaces,
					&rackosv1.NetworkInterfacePerformance{
						Name:  "eth0",
						Index: 10,
					},
				)
			},
		},
		{
			name: "duplicate interface index",
			change: func(n *rackosv1.NetworkPerformance) {
				n.Interfaces = append(
					n.Interfaces,
					&rackosv1.NetworkInterfacePerformance{
						Name:  "eth1",
						Index: 2,
					},
				)
			},
		},
		{
			name: "nil rate",
			change: func(n *rackosv1.NetworkPerformance) {
				n.InterfaceRates[0] = nil
			},
		},
		{
			name: "empty rate name",
			change: func(n *rackosv1.NetworkPerformance) {
				n.InterfaceRates[0].Name = ""
			},
		},
		{
			name: "zero rate index",
			change: func(n *rackosv1.NetworkPerformance) {
				n.InterfaceRates[0].Index = 0
			},
		},
		{
			name: "unmatched rate name",
			change: func(n *rackosv1.NetworkPerformance) {
				n.InterfaceRates[0].Name = "eth1"
			},
		},
		{
			name: "unmatched rate index",
			change: func(n *rackosv1.NetworkPerformance) {
				n.InterfaceRates[0].Index = 3
			},
		},
		{
			name: "duplicate rate",
			change: func(n *rackosv1.NetworkPerformance) {
				n.InterfaceRates = append(
					n.InterfaceRates,
					n.InterfaceRates[0],
				)
			},
		},
	}

	now := testPerformanceTime()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := testPerformanceRequest(now)
			tt.change(request.Snapshot.Network)

			requireInvalidPerformance(
				t,
				ValidateRequest(request, now),
			)
		})
	}
}

func TestValidatePerformanceActivityRates(t *testing.T) {
	now := testPerformanceTime()

	invalidValues := []float64{
		-1,
		math.NaN(),
		math.Inf(1),
		math.Inf(-1),
	}

	for index := 0; index < 8; index++ {
		for _, value := range invalidValues {
			request := testPerformanceRequest(now)

			disk := request.Snapshot.Storage.DiskRates[0]
			network := request.Snapshot.Network.InterfaceRates[0]

			fields := []*float64{
				&disk.ReadBytesPerSecond,
				&disk.WriteBytesPerSecond,
				&disk.ReadOperationsPerSecond,
				&disk.WriteOperationsPerSecond,
				&network.ReceiveBytesPerSecond,
				&network.TransmitBytesPerSecond,
				&network.ReceivePacketsPerSecond,
				&network.TransmitPacketsPerSecond,
			}

			*fields[index] = value

			requireInvalidPerformance(
				t,
				ValidateRequest(request, now),
			)
		}
	}
}

func TestValidatePerformanceRateIntervals(t *testing.T) {
	now := testPerformanceTime()

	for _, interval := range []int64{
		-1,
		0,
		60001,
		math.MaxInt64,
	} {
		request := testPerformanceRequest(now)

		request.Snapshot.Storage.DiskRates[0].
			SampleIntervalMillis = interval

		requireInvalidPerformance(
			t,
			ValidateRequest(request, now),
		)

		request = testPerformanceRequest(now)

		request.Snapshot.Network.InterfaceRates[0].
			SampleIntervalMillis = interval

		requireInvalidPerformance(
			t,
			ValidateRequest(request, now),
		)
	}
}

func TestValidatePerformanceOptionalNetworkMeasurements(t *testing.T) {
	now := testPerformanceTime()

	tests := []struct {
		name     string
		speed    *uint64
		up       bool
		loopback bool
		invalid  bool
	}{
		{
			name: "unavailable speed",
			up:   true,
		},
		{
			name:  "gigabit link",
			speed: testUint64Ptr(1000000000),
			up:    true,
		},
		{
			name:  "100 gigabit link",
			speed: testUint64Ptr(100000000000),
			up:    true,
		},
		{
			name:    "zero link speed",
			speed:   testUint64Ptr(0),
			up:      true,
			invalid: true,
		},
		{
			name:    "maximum sentinel speed",
			speed:   testUint64Ptr(math.MaxUint64),
			up:      true,
			invalid: true,
		},
		{
			name: "disabled interface",
		},
		{
			name:    "disabled interface with speed",
			speed:   testUint64Ptr(1000000000),
			invalid: true,
		},
		{
			name:     "loopback",
			up:       true,
			loopback: true,
		},
		{
			name:     "loopback with speed",
			speed:    testUint64Ptr(1000000000),
			up:       true,
			loopback: true,
			invalid:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := testPerformanceRequest(now)

			iface := request.Snapshot.Network.Interfaces[0]

			iface.LinkSpeedBitsPerSecond = tt.speed
			iface.Up = tt.up
			iface.Loopback = tt.loopback

			// Zero MTU and unavailable drop counters
			// remain valid measurements.
			iface.Mtu = 0
			iface.ReceiveDropped = nil
			iface.TransmitDropped = testUint64Ptr(0)

			err := ValidateRequest(request, now)

			if tt.invalid {
				requireInvalidPerformance(t, err)
				return
			}

			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidatePerformanceEmptyCollections(t *testing.T) {
	now := testPerformanceTime()

	request := testPerformanceRequest(now)

	request.Snapshot.Storage = &rackosv1.StoragePerformance{}
	request.Snapshot.Network = &rackosv1.NetworkPerformance{}

	if err := ValidateRequest(request, now); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePerformanceMissingRates(t *testing.T) {
	now := testPerformanceTime()

	request := testPerformanceRequest(now)

	request.Snapshot.Storage.DiskRates = nil
	request.Snapshot.Network.InterfaceRates = nil

	if err := ValidateRequest(request, now); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePerformanceCollectionLimits(t *testing.T) {
	tests := []struct {
		name   string
		change func(*rackosv1.PerformanceSnapshot)
	}{
		{
			name: "too many filesystems",
			change: func(s *rackosv1.PerformanceSnapshot) {
				s.Storage.Filesystems = make(
					[]*rackosv1.FilesystemPerformance,
					257,
				)
			},
		},
		{
			name: "too many disks",
			change: func(s *rackosv1.PerformanceSnapshot) {
				s.Storage.Disks = make(
					[]*rackosv1.DiskPerformance,
					257,
				)
			},
		},
		{
			name: "too many disk rates",
			change: func(s *rackosv1.PerformanceSnapshot) {
				s.Storage.DiskRates = make(
					[]*rackosv1.DiskRate,
					257,
				)
			},
		},
		{
			name: "too many interfaces",
			change: func(s *rackosv1.PerformanceSnapshot) {
				s.Network.Interfaces = make(
					[]*rackosv1.NetworkInterfacePerformance,
					257,
				)
			},
		},
		{
			name: "too many network rates",
			change: func(s *rackosv1.PerformanceSnapshot) {
				s.Network.InterfaceRates = make(
					[]*rackosv1.NetworkInterfaceRate,
					257,
				)
			},
		},
	}

	now := testPerformanceTime()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := testPerformanceRequest(now)
			tt.change(request.Snapshot)

			requireInvalidPerformance(
				t,
				ValidateRequest(request, now),
			)
		})
	}
}

func TestValidatePerformanceMetadata(t *testing.T) {
	now := testPerformanceTime()

	invalidValues := []string{
		"",
		" eth0",
		"eth0 ",
		"eth0\n",
		"eth0\x00",
		string([]byte{0xff}),
		strings.Repeat("x", 1025),
		strings.Repeat("é", 513),
	}

	for _, value := range invalidValues {
		request := testPerformanceRequest(now)

		request.Snapshot.Network.Interfaces[0].Name = value

		requireInvalidPerformance(
			t,
			ValidateRequest(request, now),
		)
	}
}

func TestValidatePerformanceFilesystemPaths(t *testing.T) {
	now := testPerformanceTime()

	request := testPerformanceRequest(now)

	filesystem := request.Snapshot.Storage.Filesystems[0]

	filesystem.Source = ""
	filesystem.MountPoint = "/Volumes/Backup Drive "

	if err := ValidateRequest(request, now); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePerformanceRequestSize(t *testing.T) {
	now := testPerformanceTime()

	request := testPerformanceRequest(now)

	// Construct a valid unknown Protobuf field containing
	// more data than our request limit.
	unknown := protowire.AppendTag(
		nil,
		1000,
		protowire.BytesType,
	)

	unknown = protowire.AppendBytes(
		unknown,
		make([]byte, MaxRequestBytes),
	)

	request.ProtoReflect().SetUnknown(unknown)

	if proto.Size(request) <= MaxRequestBytes {
		t.Fatal("test request does not exceed size limit")
	}

	err := ValidateRequest(request, now)

	if !errors.Is(err, ErrPerformanceTooLarge) {
		t.Fatalf(
			"expected request size error, got %v",
			err,
		)
	}
}

func TestValidatePerformanceUnknownFields(t *testing.T) {
	now := testPerformanceTime()

	request := testPerformanceRequest(now)

	// Small unknown fields remain compatible with
	// future additive protocol extensions.
	unknown := protowire.AppendTag(
		nil,
		1000,
		protowire.VarintType,
	)

	unknown = protowire.AppendVarint(unknown, 42)

	request.Snapshot.ProtoReflect().SetUnknown(unknown)

	if err := ValidateRequest(request, now); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePerformanceErrorPrivacy(t *testing.T) {
	now := testPerformanceTime()

	request := testPerformanceRequest(now)

	const sensitiveValue = "private-interface-name\n"

	request.Snapshot.Network.Interfaces[0].Name = sensitiveValue

	err := ValidateRequest(request, now)

	requireInvalidPerformance(t, err)

	if strings.Contains(
		err.Error(),
		"private-interface-name",
	) {
		t.Fatal("validation error exposed supplied metadata")
	}
}

func requireInvalidPerformance(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, ErrInvalidPerformance) {
		t.Fatalf(
			"expected invalid performance error, got %v",
			err,
		)
	}
}

func testUint64Ptr(value uint64) *uint64 {
	return &value
}

func testPerformanceTime() time.Time {
	return time.Date(
		2026, 1, 1,
		12, 0, 0, 0,
		time.UTC,
	)
}

func testPerformanceRequest(
	now time.Time,
) *rackosv1.ReportPerformanceRequest {
	return &rackosv1.ReportPerformanceRequest{
		DeviceId: "e2a104d8-9c72-4b31-8fa2-63001b24c9e8",
		SampleId: "7a91d0c4e8524b36a39f186d027c5e81",

		Snapshot: &rackosv1.PerformanceSnapshot{
			CollectedAt: timestamppb.New(
				now.Add(-time.Second),
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
						Device:                   "sda",
						ReadBytesPerSecond:       1048576.5,
						WriteBytesPerSecond:      524288.25,
						ReadOperationsPerSecond:  25.5,
						WriteOperationsPerSecond: 12.5,
						SampleIntervalMillis:     1100,
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

						LinkSpeedBitsPerSecond: testUint64Ptr(
							1000000000,
						),

						ReceivedBytes:      524288000,
						ReceivedPackets:    420000,
						ReceiveErrors:      2,
						TransmittedBytes:   262144000,
						TransmittedPackets: 210000,
						TransmitErrors:     1,

						ReceiveDropped:  testUint64Ptr(0),
						TransmitDropped: nil,
					},
				},

				InterfaceRates: []*rackosv1.NetworkInterfaceRate{
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
		},
	}
}

func FuzzValidatePerformanceRequest(f *testing.F) {
	now := testPerformanceTime()

	valid, err := proto.Marshal(
		testPerformanceRequest(now),
	)
	if err != nil {
		f.Fatal(err)
	}

	f.Add(valid)
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff, 0xff})
	f.Add([]byte{0x0a, 0x01, 0x61})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > MaxRequestBytes {
			return
		}

		request := &rackosv1.ReportPerformanceRequest{}

		if err := proto.Unmarshal(data, request); err != nil {
			return
		}

		err := ValidateRequest(request, now)

		if err != nil &&
			!errors.Is(err, ErrInvalidPerformance) &&
			!errors.Is(err, ErrPerformanceTooLarge) {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})
}
