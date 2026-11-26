package inventory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestParseLinuxCPUList(t *testing.T) {
	tests := []struct {
		input    string
		expected []int
	}{
		{
			input:    "0",
			expected: []int{0},
		},
		{
			input:    "0-3",
			expected: []int{0, 1, 2, 3},
		},
		{
			input:    "0,2,4,6",
			expected: []int{0, 2, 4, 6},
		},
		{
			input:    "0-3,8-11",
			expected: []int{0, 1, 2, 3, 8, 9, 10, 11},
		},
		{
			input:    " 0-2\n",
			expected: []int{0, 1, 2},
		},
		{
			input:    "4-4",
			expected: []int{4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseLinuxCPUList(tt.input)
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(
				result,
				tt.expected,
			) {
				t.Fatalf(
					"expected %v, got %v",
					tt.expected,
					result,
				)
			}
		})
	}
}

func TestParseLinuxCPUListInvalid(t *testing.T) {
	inputs := []string{
		"",
		"abc",
		"-1",
		"4-0",
		"0-",
		"0-abc",
		"0,,1",
		"0,0",
		"0-3,2-5",
		"0-999999999",
		"1048576",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			_, err := parseLinuxCPUList(input)

			if err == nil {
				t.Fatal("invalid CPU list accepted")
			}
		})
	}
}

func TestParseLinuxMemory(t *testing.T) {
	input := `MemTotal:       16777216 kB
MemFree:         4194304 kB
MemAvailable:    8388608 kB
`

	result, err := parseLinuxMemory(input)
	if err != nil {
		t.Fatal(err)
	}

	const expected uint64 = 16 * 1024 * 1024 * 1024

	if result != expected {
		t.Fatalf(
			"expected %d bytes, got %d",
			expected,
			result,
		)
	}
}

func TestParseLinuxMemoryInvalid(t *testing.T) {
	inputs := []string{
		"",
		"MemFree: 1024 kB",
		"MemTotal:",
		"MemTotal: 0 kB",
		"MemTotal: -1024 kB",
		"MemTotal: abc kB",
		"MemTotal: 1024 MB",
		"MemTotal: 1024",
		"MemTotal: 18446744073709551615 kB",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			_, err := parseLinuxMemory(input)

			if err == nil {
				t.Fatal("invalid memory value accepted")
			}
		})
	}
}

func TestParseLinuxBootTime(t *testing.T) {
	input := `cpu 123 456 789
intr 1000
ctxt 2000
btime 1700000000
processes 3000
`

	result, err := parseLinuxBootTime(input)
	if err != nil {
		t.Fatal(err)
	}

	expected := time.Unix(
		1700000000,
		0,
	).UTC()

	if !result.Equal(expected) {
		t.Fatalf(
			"expected %v, got %v",
			expected,
			result,
		)
	}

	if result.Location() != time.UTC {
		t.Fatal("boot time must be UTC")
	}
}

func TestParseLinuxBootTimeInvalid(t *testing.T) {
	inputs := []string{
		"",
		"processes 100",
		"btime",
		"btime 0",
		"btime -1",
		"btime abc",
		"btime 999999999999999999999",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			_, err := parseLinuxBootTime(input)

			if err == nil {
				t.Fatal("invalid boot time accepted")
			}
		})
	}
}

func TestLinuxField(t *testing.T) {
	input := `processor   : 0
vendor_id   : GenuineIntel
model name  : Test Processor
`

	if result := linuxField(
		input,
		"vendor_id",
		":",
	); result != "GenuineIntel" {
		t.Fatalf("unexpected vendor: %q", result)
	}

	if result := linuxField(
		input,
		"model name",
		":",
	); result != "Test Processor" {
		t.Fatalf("unexpected CPU model: %q", result)
	}

	if result := linuxField(
		input,
		"nonexistent",
		":",
	); result != "" {
		t.Fatal("unexpected missing field value")
	}
}

func TestLinuxRead(t *testing.T) {
	path := filepath.Join(
		t.TempDir(),
		"hardware-model",
	)

	err := os.WriteFile(
		path,
		[]byte("Test Hardware\x00\n"),
		0600,
	)
	if err != nil {
		t.Fatal(err)
	}

	result, err := linuxRead(
		context.Background(),
		path,
	)
	if err != nil {
		t.Fatal(err)
	}

	if result != "Test Hardware" {
		t.Fatalf("unexpected file contents: %q", result)
	}
}

func TestLinuxReadMissingFile(t *testing.T) {
	path := filepath.Join(
		t.TempDir(),
		"nonexistent",
	)

	_, err := linuxRead(
		context.Background(),
		path,
	)

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(
			"expected missing file error, got %v",
			err,
		)
	}
}

func TestLinuxReadCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	_, err := linuxRead(ctx, "/proc/cpuinfo")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}
}

func TestLinuxHardwareCollection(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	result, err := collectPlatformHardware(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if result.HostLogicalCPUs < 1 {
		t.Fatal("invalid logical CPU count")
	}

	if result.PhysicalCPUs > result.HostLogicalCPUs {
		t.Fatal("invalid CPU topology")
	}

	if result.MemoryBytes == 0 {
		t.Fatal("missing system memory")
	}

	if result.KernelVersion == "" {
		t.Fatal("missing kernel version")
	}

	if result.BootTime.IsZero() {
		t.Fatal("missing boot time")
	}

	if result.BootTime.After(time.Now()) {
		t.Fatal("boot time is in the future")
	}
}
