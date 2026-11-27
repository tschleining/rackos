package metrics

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestCalculateFilesystemCapacity(t *testing.T) {
	const gib = uint64(1024 * 1024 * 1024)

	tests := []struct {
		name      string
		total     uint64
		free      uint64
		available uint64
		used      uint64
		reserved  uint64
		expected  float64
	}{
		{
			name:      "normal filesystem",
			total:     1000 * gib,
			free:      350 * gib,
			available: 300 * gib,
			used:      650 * gib,
			reserved:  50 * gib,
			expected:  65,
		},
		{
			name:      "empty filesystem",
			total:     100 * gib,
			free:      100 * gib,
			available: 100 * gib,
			expected:  0,
		},
		{
			name:     "full filesystem",
			total:    100 * gib,
			used:     100 * gib,
			expected: 100,
		},
		{
			name:      "half utilized",
			total:     100 * gib,
			free:      50 * gib,
			available: 50 * gib,
			used:      50 * gib,
			expected:  50,
		},
		{
			name:     "only reserved space remains",
			total:    100 * gib,
			free:     5 * gib,
			used:     95 * gib,
			reserved: 5 * gib,
			expected: 95,
		},
		{
			name:      "empty filesystem with reserved space",
			total:     100 * gib,
			free:      100 * gib,
			available: 95 * gib,
			reserved:  5 * gib,
			expected:  0,
		},
		{
			name:      "fractional utilization",
			total:     3,
			free:      2,
			available: 2,
			used:      1,
			expected:  100.0 / 3.0,
		},
		{
			name:      "large filesystem",
			total:     16384 * gib,
			free:      8192 * gib,
			available: 4096 * gib,
			used:      8192 * gib,
			reserved:  4096 * gib,
			expected:  50,
		},
		{
			name:     "maximum integer capacity",
			total:    math.MaxUint64,
			used:     math.MaxUint64,
			expected: 100,
		},
		{
			name:      "maximum free capacity",
			total:     math.MaxUint64,
			free:      math.MaxUint64,
			available: math.MaxUint64,
			expected:  0,
		},
		{
			name:     "maximum reserved capacity",
			total:    math.MaxUint64,
			free:     math.MaxUint64,
			reserved: math.MaxUint64,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capacity, err := calculateFilesystemCapacity(
				tt.total,
				tt.free,
				tt.available,
			)
			if err != nil {
				t.Fatal(err)
			}

			if capacity.TotalBytes != tt.total {
				t.Fatal("incorrect total capacity")
			}

			if capacity.FreeBytes != tt.free {
				t.Fatal("incorrect free capacity")
			}

			if capacity.AvailableBytes != tt.available {
				t.Fatal("incorrect available capacity")
			}

			if capacity.UsedBytes != tt.used {
				t.Fatalf(
					"expected %d used bytes, got %d",
					tt.used,
					capacity.UsedBytes,
				)
			}

			if capacity.ReservedBytes != tt.reserved {
				t.Fatalf(
					"expected %d reserved bytes, got %d",
					tt.reserved,
					capacity.ReservedBytes,
				)
			}

			requireMetricPercent(
				t,
				capacity.UsagePercent,
				tt.expected,
			)

			requireFilesystemAccounting(t, capacity)
		})
	}
}

func TestCalculateFilesystemCapacityInvalid(t *testing.T) {
	tests := []struct {
		name      string
		total     uint64
		free      uint64
		available uint64
	}{
		{
			name: "zero total capacity",
		},
		{
			name: "free space without capacity",
			free: 100,
		},
		{
			name:  "free exceeds total",
			total: 100,
			free:  101,
		},
		{
			name:      "available exceeds free",
			total:     100,
			free:      50,
			available: 51,
		},
		{
			name:      "available exceeds total",
			total:     100,
			free:      100,
			available: 101,
		},
		{
			name:      "available without free capacity",
			total:     100,
			available: 1,
		},
		{
			name:  "maximum invalid free capacity",
			total: 100,
			free:  math.MaxUint64,
		},
		{
			name:      "maximum invalid available capacity",
			total:     100,
			free:      50,
			available: math.MaxUint64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capacity, err := calculateFilesystemCapacity(
				tt.total,
				tt.free,
				tt.available,
			)

			if !errors.Is(err, ErrInvalidFilesystem) {
				t.Fatalf(
					"expected ErrInvalidFilesystem, got %v",
					err,
				)
			}

			if capacity != (FilesystemCapacity{}) {
				t.Fatal(
					"invalid statistics returned capacity",
				)
			}
		})
	}
}

func TestFilesystemCapacityFromBlocks(t *testing.T) {
	tests := []struct {
		name      string
		blockSize uint64
		total     uint64
		free      uint64
		available uint64

		expectedTotal     uint64
		expectedFree      uint64
		expectedAvailable uint64
		expectedUsed      uint64
		expectedReserved  uint64
		expectedPercent   float64
	}{
		{
			name:              "4096 byte blocks",
			blockSize:         4096,
			total:             1000,
			free:              350,
			available:         300,
			expectedTotal:     4096000,
			expectedFree:      1433600,
			expectedAvailable: 1228800,
			expectedUsed:      2662400,
			expectedReserved:  204800,
			expectedPercent:   65,
		},
		{
			name:              "512 byte blocks",
			blockSize:         512,
			total:             1000,
			free:              500,
			available:         500,
			expectedTotal:     512000,
			expectedFree:      256000,
			expectedAvailable: 256000,
			expectedUsed:      256000,
			expectedPercent:   50,
		},
		{
			name:              "16384 byte blocks",
			blockSize:         16384,
			total:             100,
			free:              25,
			available:         20,
			expectedTotal:     1638400,
			expectedFree:      409600,
			expectedAvailable: 327680,
			expectedUsed:      1228800,
			expectedReserved:  81920,
			expectedPercent:   75,
		},
		{
			name:            "full filesystem",
			blockSize:       4096,
			total:           100,
			expectedTotal:   409600,
			expectedUsed:    409600,
			expectedPercent: 100,
		},
		{
			name:              "empty filesystem",
			blockSize:         4096,
			total:             100,
			free:              100,
			available:         100,
			expectedTotal:     409600,
			expectedFree:      409600,
			expectedAvailable: 409600,
		},
		{
			name:            "maximum byte capacity",
			blockSize:       1,
			total:           math.MaxUint64,
			expectedTotal:   math.MaxUint64,
			expectedUsed:    math.MaxUint64,
			expectedPercent: 100,
		},
		{
			name:              "maximum safe block multiplication",
			blockSize:         4096,
			total:             math.MaxUint64 / 4096,
			free:              math.MaxUint64 / 4096,
			available:         math.MaxUint64 / 4096,
			expectedTotal:     math.MaxUint64 - 4095,
			expectedFree:      math.MaxUint64 - 4095,
			expectedAvailable: math.MaxUint64 - 4095,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capacity, err := filesystemCapacityFromBlocks(
				tt.blockSize,
				tt.total,
				tt.free,
				tt.available,
			)
			if err != nil {
				t.Fatal(err)
			}

			expected := FilesystemCapacity{
				TotalBytes:     tt.expectedTotal,
				FreeBytes:      tt.expectedFree,
				AvailableBytes: tt.expectedAvailable,
				UsedBytes:      tt.expectedUsed,
				ReservedBytes:  tt.expectedReserved,
			}

			actual := capacity
			actual.UsagePercent = 0

			if actual != expected {
				t.Fatalf(
					"expected %+v, got %+v",
					expected,
					actual,
				)
			}

			requireMetricPercent(
				t,
				capacity.UsagePercent,
				tt.expectedPercent,
			)

			requireFilesystemAccounting(t, capacity)
		})
	}
}

func TestFilesystemCapacityFromBlocksInvalid(t *testing.T) {
	tests := []struct {
		name      string
		blockSize uint64
		total     uint64
		free      uint64
		available uint64
	}{
		{
			name:  "zero block size",
			total: 100,
		},
		{
			name:      "zero total blocks",
			blockSize: 4096,
		},
		{
			name:      "free blocks exceed total",
			blockSize: 4096,
			total:     100,
			free:      101,
		},
		{
			name:      "available blocks exceed free",
			blockSize: 4096,
			total:     100,
			free:      50,
			available: 51,
		},
		{
			name:      "capacity multiplication overflow",
			blockSize: 4096,
			total:     math.MaxUint64/4096 + 1,
		},
		{
			name:      "maximum block size overflow",
			blockSize: math.MaxUint64,
			total:     2,
		},
		{
			name:      "invalid free block boundary",
			blockSize: 4096,
			total:     100,
			free:      math.MaxUint64,
		},
		{
			name:      "invalid available block boundary",
			blockSize: 4096,
			total:     100,
			free:      50,
			available: math.MaxUint64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capacity, err := filesystemCapacityFromBlocks(
				tt.blockSize,
				tt.total,
				tt.free,
				tt.available,
			)

			if !errors.Is(err, ErrInvalidFilesystem) {
				t.Fatalf(
					"expected ErrInvalidFilesystem, got %v",
					err,
				)
			}

			if capacity != (FilesystemCapacity{}) {
				t.Fatal(
					"invalid blocks returned capacity",
				)
			}
		})
	}
}

func TestFilesystemReservedSpaceIsNotUsed(t *testing.T) {
	capacity, err := calculateFilesystemCapacity(
		1000,
		350,
		300,
	)
	if err != nil {
		t.Fatal(err)
	}

	if capacity.UsedBytes != 650 {
		t.Fatal("reserved capacity counted as used")
	}

	if capacity.ReservedBytes != 50 {
		t.Fatal("incorrect reserved capacity")
	}

	requireMetricPercent(
		t,
		capacity.UsagePercent,
		65,
	)
}

func TestStorageUsageJSON(t *testing.T) {
	storage := StorageUsage{
		Filesystems: []FilesystemUsage{
			{
				Source:         "/dev/sda1",
				MountPoint:     "/",
				FilesystemType: "ext4",
				ReadOnly:       false,
				Capacity: FilesystemCapacity{
					TotalBytes:     1000,
					FreeBytes:      350,
					AvailableBytes: 300,
					UsedBytes:      650,
					ReservedBytes:  50,
					UsagePercent:   65,
				},
			},
		},
		Disks: []DiskUsage{
			{
				Device:          "sda",
				ReadBytes:       4096000,
				WriteBytes:      2048000,
				ReadOperations:  1000,
				WriteOperations: 500,
			},
		},
		DiskRates: []DiskRate{},
	}

	data, err := json.Marshal(storage)
	if err != nil {
		t.Fatal(err)
	}

	const expected = `{
		"filesystems": [
			{
				"source": "/dev/sda1",
				"mount_point": "/",
				"filesystem_type": "ext4",
				"read_only": false,
				"capacity": {
					"total_bytes": 1000,
					"free_bytes": 350,
					"available_bytes": 300,
					"used_bytes": 650,
					"reserved_bytes": 50,
					"usage_percent": 65
				}
			}
		],
		"disks": [
			{
				"device": "sda",
				"read_bytes": 4096000,
				"write_bytes": 2048000,
				"read_operations": 1000,
				"write_operations": 500
			}
		],
		"disk_rates": []
	}`

	var actualJSON map[string]any
	var expectedJSON map[string]any

	if err := json.Unmarshal(data, &actualJSON); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal([]byte(expected), &expectedJSON); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(actualJSON, expectedJSON) {
		t.Fatalf(
			"unexpected storage JSON: %s",
			data,
		)
	}

	var decoded StorageUsage

	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(storage, decoded) {
		t.Fatal("storage JSON round trip failed")
	}
}
func TestStorageUsageEmptyJSON(t *testing.T) {
	storage := StorageUsage{
		Filesystems: []FilesystemUsage{},
		Disks:       []DiskUsage{},
		DiskRates:   []DiskRate{},
	}

	data, err := json.Marshal(storage)
	if err != nil {
		t.Fatal(err)
	}

	const expected = `{"filesystems":[],"disks":[],"disk_rates":[]}`

	if string(data) != expected {
		t.Fatalf(
			"expected %s, got %s",
			expected,
			data,
		)
	}
}

func TestDiskUsageMaximumCountersJSON(t *testing.T) {
	disk := DiskUsage{
		Device:          "disk0",
		ReadBytes:       math.MaxUint64,
		WriteBytes:      math.MaxUint64,
		ReadOperations:  math.MaxUint64,
		WriteOperations: math.MaxUint64,
	}

	data, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}

	var decoded DiskUsage

	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded != disk {
		t.Fatal("disk counters lost precision during JSON round trip")
	}
}

// requireFilesystemAccounting verifies the accounting
// invariants for every successful capacity measurement.
func requireFilesystemAccounting(
	t *testing.T,
	capacity FilesystemCapacity,
) {
	t.Helper()

	if capacity.FreeBytes > capacity.TotalBytes {
		t.Fatal("free capacity exceeds total")
	}

	if capacity.AvailableBytes > capacity.FreeBytes {
		t.Fatal("available capacity exceeds free")
	}

	if capacity.UsedBytes !=
		capacity.TotalBytes-capacity.FreeBytes {
		t.Fatal("used capacity accounting is inconsistent")
	}

	if capacity.ReservedBytes !=
		capacity.FreeBytes-capacity.AvailableBytes {
		t.Fatal("reserved capacity accounting is inconsistent")
	}

	if capacity.UsedBytes+
		capacity.FreeBytes != capacity.TotalBytes {
		t.Fatal("total capacity accounting is inconsistent")
	}

	if capacity.AvailableBytes+
		capacity.ReservedBytes != capacity.FreeBytes {
		t.Fatal("free capacity accounting is inconsistent")
	}
}
