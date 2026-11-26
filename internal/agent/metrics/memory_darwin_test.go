package metrics

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
	"unsafe"
)

func TestDarwinVMStatisticsLayout(t *testing.T) {
	var statistics darwinVMStatistics

	if size := unsafe.Sizeof(statistics); size != 160 {
		t.Fatalf(
			"expected native structure size 160, got %d",
			size,
		)
	}

	if alignment := unsafe.Alignof(statistics); alignment != 8 {
		t.Fatalf(
			"expected native alignment 8, got %d",
			alignment,
		)
	}

	if offset := unsafe.Offsetof(statistics.InactiveCount); offset != 8 {
		t.Fatalf(
			"unexpected inactive counter offset: %d",
			offset,
		)
	}

	if offset := unsafe.Offsetof(statistics.Reserved); offset != 16 {
		t.Fatalf(
			"unexpected native storage offset: %d",
			offset,
		)
	}
}

func TestDarwinReadMemoryUsage(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	memory, err := readMemoryUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if memory.TotalBytes == 0 {
		t.Fatal("physical memory capacity is zero")
	}

	if memory.AvailableBytes > memory.TotalBytes {
		t.Fatal("available memory exceeds capacity")
	}

	if memory.UsedBytes+memory.AvailableBytes != memory.TotalBytes {
		t.Fatal("memory accounting is inconsistent")
	}

	expected := float64(memory.UsedBytes) /
		float64(memory.TotalBytes) * 100

	requireMetricPercent(
		t,
		memory.UsagePercent,
		expected,
	)

	reader, err := getDarwinMemoryReader()
	if err != nil {
		t.Fatal(err)
	}

	t.Logf(
		"Kernel page size: %d bytes",
		reader.pageSize,
	)

	t.Logf(
		"Total memory: %d bytes",
		memory.TotalBytes,
	)

	t.Logf(
		"Available memory: %d bytes",
		memory.AvailableBytes,
	)

	t.Logf(
		"Used memory: %d bytes",
		memory.UsedBytes,
	)

	t.Logf(
		"Memory utilization: %.2f%%",
		memory.UsagePercent,
	)
}

func TestDarwinReadMemoryUsageCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	memory, err := readMemoryUsage(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected cancellation error, got %v",
			err,
		)
	}

	if memory != (MemoryUsage{}) {
		t.Fatal("canceled collection returned memory")
	}
}

func TestNormalizeDarwinMemoryUsage(t *testing.T) {
	const gib = uint64(1024 * 1024 * 1024)

	tests := []struct {
		name      string
		total     uint64
		pageSize  uint64
		free      uint32
		inactive  uint32
		available uint64
		expected  float64
	}{
		{
			name:      "Apple Silicon pages",
			total:     24 * gib,
			pageSize:  16384,
			free:      131072,
			inactive:  262144,
			available: 6 * gib,
			expected:  75,
		},
		{
			name:      "Intel pages",
			total:     16 * gib,
			pageSize:  4096,
			free:      524288,
			inactive:  524288,
			available: 4 * gib,
			expected:  75,
		},
		{
			name:      "no available memory",
			total:     24 * gib,
			pageSize:  16384,
			available: 0,
			expected:  100,
		},
		{
			name:      "all memory available",
			total:     gib,
			pageSize:  4096,
			free:      131072,
			inactive:  131072,
			available: gib,
			expected:  0,
		},
		{
			name:      "page counter sum exceeds uint32",
			total:     1 << 46,
			pageSize:  4096,
			free:      math.MaxUint32,
			inactive:  1,
			available: 1 << 44,
			expected:  75,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memory, err := normalizeDarwinMemoryUsage(
				tt.total,
				tt.pageSize,
				tt.free,
				tt.inactive,
			)
			if err != nil {
				t.Fatal(err)
			}

			if memory.TotalBytes != tt.total {
				t.Fatal("incorrect total memory")
			}

			if memory.AvailableBytes != tt.available {
				t.Fatalf(
					"expected %d available bytes, got %d",
					tt.available,
					memory.AvailableBytes,
				)
			}

			if memory.UsedBytes != tt.total-tt.available {
				t.Fatal("incorrect used memory")
			}

			requireMetricPercent(
				t,
				memory.UsagePercent,
				tt.expected,
			)
		})
	}
}

func TestNormalizeDarwinMemoryUsageInvalid(t *testing.T) {
	tests := []struct {
		name     string
		total    uint64
		pageSize uint64
		free     uint32
		inactive uint32
	}{
		{
			name:     "zero memory capacity",
			pageSize: 4096,
		},
		{
			name:  "zero page size",
			total: 16384,
		},
		{
			name:     "invalid page size",
			total:    16384,
			pageSize: 3000,
		},
		{
			name:     "page size exceeds capacity",
			total:    4096,
			pageSize: 16384,
		},
		{
			name:     "available memory exceeds capacity",
			total:    4096,
			pageSize: 4096,
			free:     1,
			inactive: 1,
		},
		{
			name:     "available memory multiplication overflow",
			total:    math.MaxUint64,
			pageSize: 1 << 63,
			free:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memory, err := normalizeDarwinMemoryUsage(
				tt.total,
				tt.pageSize,
				tt.free,
				tt.inactive,
			)

			if !errors.Is(err, ErrInvalidMemory) {
				t.Fatalf(
					"expected ErrInvalidMemory, got %v",
					err,
				)
			}

			if memory != (MemoryUsage{}) {
				t.Fatal("invalid data returned memory usage")
			}
		})
	}
}
