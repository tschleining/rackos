package client

import (
	"strconv"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/agent/inventory"

	"google.golang.org/protobuf/proto"
)

func testInventorySnapshot() inventory.Inventory {
	collected := time.Date(
		2026, time.November, 26,
		20, 39, 39, 926030000,
		time.UTC,
	)

	boot := collected.Add(-24 * time.Hour)

	return inventory.Inventory{
		Hostname:        "test-workstation.local",
		OS:              "darwin",
		Architecture:    "arm64",
		LogicalCPUs:     14,
		HostLogicalCPUs: 14,
		PhysicalCores:   14,
		CPUModel:        "Apple M4 Pro",
		CPUManufacturer: "Apple",
		MemoryBytes:     25769803776,
		HardwareModel:   "Mac16,8",
		OSVersion:       "26.6.2",
		KernelVersion:   "25.6.0",
		BootTime:        &boot,
		CollectedAt:     collected,
	}
}

func TestInventoryToProto(t *testing.T) {
	snapshot := testInventorySnapshot()

	result, err := inventoryToProto(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	expected := &rackosv1.HardwareInventory{
		Hostname:              snapshot.Hostname,
		Os:                    snapshot.OS,
		Architecture:          snapshot.Architecture,
		LogicalCpus:           14,
		HostLogicalCpus:       14,
		PhysicalCores:         14,
		CpuModel:              snapshot.CPUModel,
		CpuManufacturer:       snapshot.CPUManufacturer,
		MemoryBytes:           snapshot.MemoryBytes,
		HardwareModel:         snapshot.HardwareModel,
		OsVersion:             snapshot.OSVersion,
		KernelVersion:         snapshot.KernelVersion,
		BootTimeUnixMillis:    snapshot.BootTime.UnixMilli(),
		CollectedAtUnixMillis: snapshot.CollectedAt.UnixMilli(),
	}

	if !proto.Equal(result, expected) {
		t.Fatalf(
			"unexpected inventory conversion: %v",
			result,
		)
	}
}

func TestInventoryToProtoOptionalFields(t *testing.T) {
	snapshot := inventory.Inventory{
		Hostname:     "test-server",
		OS:           "linux",
		Architecture: "amd64",
		LogicalCPUs:  4,
		CollectedAt:  time.Now().UTC(),
	}

	result, err := inventoryToProto(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	expected := &rackosv1.HardwareInventory{
		Hostname:              snapshot.Hostname,
		Os:                    snapshot.OS,
		Architecture:          snapshot.Architecture,
		LogicalCpus:           4,
		CollectedAtUnixMillis: snapshot.CollectedAt.UnixMilli(),
	}

	if !proto.Equal(result, expected) {
		t.Fatal("optional fields were not preserved")
	}
}

func TestInventoryToProtoInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*inventory.Inventory)
	}{
		{
			name: "zero logical CPUs",
			mutate: func(s *inventory.Inventory) {
				s.LogicalCPUs = 0
			},
		},
		{
			name: "negative logical CPUs",
			mutate: func(s *inventory.Inventory) {
				s.LogicalCPUs = -1
			},
		},
		{
			name: "negative host CPUs",
			mutate: func(s *inventory.Inventory) {
				s.HostLogicalCPUs = -1
			},
		},
		{
			name: "negative physical cores",
			mutate: func(s *inventory.Inventory) {
				s.PhysicalCores = -1
			},
		},
		{
			name: "invalid CPU topology",
			mutate: func(s *inventory.Inventory) {
				s.HostLogicalCPUs = 4
				s.PhysicalCores = 8
			},
		},
		{
			name: "missing collection timestamp",
			mutate: func(s *inventory.Inventory) {
				s.CollectedAt = time.Time{}
			},
		},
		{
			name: "negative collection timestamp",
			mutate: func(s *inventory.Inventory) {
				s.CollectedAt = time.Unix(-1, 0)
			},
		},
		{
			name: "invalid boot timestamp",
			mutate: func(s *inventory.Inventory) {
				boot := time.Time{}
				s.BootTime = &boot
			},
		},
		{
			name: "boot after collection",
			mutate: func(s *inventory.Inventory) {
				boot := s.CollectedAt.Add(time.Hour)
				s.BootTime = &boot
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := testInventorySnapshot()
			tt.mutate(&snapshot)

			result, err := inventoryToProto(snapshot)

			if err == nil {
				t.Fatal("expected conversion error")
			}

			if result != nil {
				t.Fatal("unexpected inventory result")
			}
		})
	}
}

func TestInventoryCPUCountOverflow(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("uint32 overflow requires a 64-bit int")
	}

	tooLarge := uint64(1) << 32

	_, err := inventoryCPUCount(
		"logical_cpus",
		int(tooLarge),
		true,
	)

	if err == nil {
		t.Fatal("CPU count overflow was accepted")
	}
}

func TestInventoryTimestampPrecision(t *testing.T) {
	value := time.Unix(
		1700000000,
		123456789,
	).UTC()

	result, err := inventoryTimestamp(
		"collected_at",
		value,
	)
	if err != nil {
		t.Fatal(err)
	}

	const expected int64 = 1700000000123

	if result != expected {
		t.Fatalf(
			"expected %d, got %d",
			expected,
			result,
		)
	}
}

func TestInventoryProtoRoundTrip(t *testing.T) {
	snapshot := testInventorySnapshot()

	message, err := inventoryToProto(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	data, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}

	var decoded rackosv1.HardwareInventory

	if err := proto.Unmarshal(
		data,
		&decoded,
	); err != nil {
		t.Fatal(err)
	}

	if !proto.Equal(message, &decoded) {
		t.Fatal("protobuf inventory round trip failed")
	}
}
