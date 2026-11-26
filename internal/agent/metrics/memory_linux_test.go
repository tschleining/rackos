package metrics

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestParseLinuxMemoryUsage(t *testing.T) {
	const gib = uint64(1024 * 1024 * 1024)

	tests := []struct {
		name      string
		input     string
		total     uint64
		available uint64
		expected  float64
	}{
		{
			name: "complete memory statistics",
			input: `
MemTotal:       16777216 kB
MemFree:         2097152 kB
MemAvailable:    6291456 kB
Buffers:          524288 kB
Cached:          4194304 kB
SwapTotal:       4194304 kB
SwapFree:        4194304 kB
`,
			total:     16 * gib,
			available: 6 * gib,
			expected:  62.5,
		},
		{
			name: "required fields only",
			input: `MemTotal: 1048576 kB
MemAvailable: 524288 kB`,
			total:     gib,
			available: gib / 2,
			expected:  50,
		},
		{
			name: "reversed field order",
			input: `MemAvailable: 524288 kB
MemTotal: 1048576 kB`,
			total:     gib,
			available: gib / 2,
			expected:  50,
		},
		{
			name: "no available memory",
			input: `MemTotal: 1048576 kB
MemAvailable: 0 kB`,
			total:    gib,
			expected: 100,
		},
		{
			name: "all memory available",
			input: `MemTotal: 1048576 kB
MemAvailable: 1048576 kB`,
			total:     gib,
			available: gib,
			expected:  0,
		},
		{
			name: "unrelated fields are ignored",
			input: `MemTotal: 1048576 kB
MemAvailable: 524288 kB
MemFree: 0 kB
Cached: invalid
HugePages_Total: 10
Unrelated line`,
			total:     gib,
			available: gib / 2,
			expected:  50,
		},
		{
			name: "whitespace normalization",
			input: " MemTotal :\t1048576  kB\n" +
				"MemAvailable:\t524288\tkB\n",
			total:     gib,
			available: gib / 2,
			expected:  50,
		},
		{
			name: "kilobyte conversion",
			input: `MemTotal: 3 kB
MemAvailable: 2 kB`,
			total:     3072,
			available: 2048,
			expected:  100.0 / 3.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memory, err := parseLinuxMemoryUsage(
				strings.NewReader(tt.input),
			)
			if err != nil {
				t.Fatal(err)
			}

			if memory.TotalBytes != tt.total {
				t.Fatalf(
					"expected %d total bytes, got %d",
					tt.total,
					memory.TotalBytes,
				)
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

func TestParseLinuxMemoryUsageInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "empty input",
		},
		{
			name:  "missing total memory",
			input: "MemAvailable: 100 kB",
		},
		{
			name:  "missing available memory",
			input: "MemTotal: 100 kB",
		},
		{
			name: "free memory is not a fallback",
			input: `MemTotal: 100 kB
MemFree: 50 kB`,
		},
		{
			name: "duplicate total memory",
			input: `MemTotal: 100 kB
MemTotal: 100 kB
MemAvailable: 50 kB`,
		},
		{
			name: "duplicate available memory",
			input: `MemTotal: 100 kB
MemAvailable: 50 kB
MemAvailable: 50 kB`,
		},
		{
			name: "zero capacity",
			input: `MemTotal: 0 kB
MemAvailable: 0 kB`,
		},
		{
			name: "available exceeds capacity",
			input: `MemTotal: 100 kB
MemAvailable: 101 kB`,
		},
		{
			name: "invalid numeric value",
			input: `MemTotal: invalid kB
MemAvailable: 50 kB`,
		},
		{
			name: "negative available memory",
			input: `MemTotal: 100 kB
MemAvailable: -1 kB`,
		},
		{
			name: "negative total memory",
			input: `MemTotal: -100 kB
MemAvailable: 0 kB`,
		},
		{
			name: "fractional value",
			input: `MemTotal: 100.5 kB
MemAvailable: 50 kB`,
		},
		{
			name: "missing measurement unit",
			input: `MemTotal: 100
MemAvailable: 50 kB`,
		},
		{
			name: "incorrect measurement unit",
			input: `MemTotal: 100 MB
MemAvailable: 50 kB`,
		},
		{
			name: "unexpected trailing field",
			input: `MemTotal: 100 kB extra
MemAvailable: 50 kB`,
		},
		{
			name: "integer parsing overflow",
			input: `MemTotal: 18446744073709551616 kB
MemAvailable: 0 kB`,
		},
		{
			name: "total byte conversion overflow",
			input: `MemTotal: 18014398509481984 kB
MemAvailable: 0 kB`,
		},
		{
			name: "available byte conversion overflow",
			input: `MemTotal: 100 kB
MemAvailable: 18014398509481984 kB`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memory, err := parseLinuxMemoryUsage(
				strings.NewReader(tt.input),
			)

			if !errors.Is(err, ErrInvalidMemory) {
				t.Fatalf(
					"expected ErrInvalidMemory, got %v",
					err,
				)
			}

			if memory != (MemoryUsage{}) {
				t.Fatal("invalid input returned memory usage")
			}
		})
	}
}

func TestParseLinuxMemoryUsageReadFailure(t *testing.T) {
	readError := errors.New("memory statistics read failed")

	reader := io.MultiReader(
		strings.NewReader(
			"MemTotal: 100 kB\nMemAvailable: 50 kB\n",
		),
		iotest.ErrReader(readError),
	)

	memory, err := parseLinuxMemoryUsage(reader)

	if !errors.Is(err, readError) {
		t.Fatalf(
			"expected read error, got %v",
			err,
		)
	}

	if memory != (MemoryUsage{}) {
		t.Fatal("failed read returned memory usage")
	}
}

func TestParseLinuxMemoryBytesMaximum(t *testing.T) {
	const expected = uint64(18446744073709550592)

	value, err := parseLinuxMemoryBytes(
		"18014398509481983 kB",
	)
	if err != nil {
		t.Fatal(err)
	}

	if value != expected {
		t.Fatalf(
			"expected %d bytes, got %d",
			expected,
			value,
		)
	}
}

func TestLinuxReadMemoryUsageCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	memory, err := readMemoryUsage(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if memory != (MemoryUsage{}) {
		t.Fatal("canceled collection returned memory usage")
	}
}
