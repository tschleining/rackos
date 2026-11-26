package inventory

import (
	"errors"
	"strings"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"google.golang.org/protobuf/proto"
)

func validationTestTime() time.Time {
	return time.Date(
		2026, time.November, 26,
		20, 0, 0, 0,
		time.UTC,
	)
}

func validInventorySnapshot(
	now time.Time,
) *rackosv1.HardwareInventory {
	return &rackosv1.HardwareInventory{
		Hostname:              "test-workstation.local",
		Os:                    "darwin",
		Architecture:          "arm64",
		LogicalCpus:           14,
		HostLogicalCpus:       14,
		PhysicalCores:         14,
		CpuModel:              "Apple M4 Pro",
		CpuManufacturer:       "Apple",
		MemoryBytes:           24 * 1024 * 1024 * 1024,
		HardwareModel:         "Mac16,8",
		OsVersion:             "26.6.2",
		KernelVersion:         "25.6.0",
		BootTimeUnixMillis:    now.Add(-24 * time.Hour).UnixMilli(),
		CollectedAtUnixMillis: now.UnixMilli(),
	}
}

func TestValidateInventory(t *testing.T) {
	now := validationTestTime()

	snapshot := validInventorySnapshot(now)

	if err := Validate(snapshot, now); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMinimalInventory(t *testing.T) {
	now := validationTestTime()

	snapshot := &rackosv1.HardwareInventory{
		Hostname:              "test-server",
		Os:                    "linux",
		Architecture:          "amd64",
		LogicalCpus:           4,
		CollectedAtUnixMillis: now.UnixMilli(),
	}

	if err := Validate(snapshot, now); err != nil {
		t.Fatal(err)
	}
}

func TestValidateNilInventory(t *testing.T) {
	err := Validate(nil, validationTestTime())

	if !errors.Is(err, ErrInvalidInventory) {
		t.Fatalf(
			"expected ErrInvalidInventory, got %v",
			err,
		)
	}
}

func TestValidateRequiredFields(t *testing.T) {
	now := validationTestTime()

	tests := []struct {
		name   string
		mutate func(*rackosv1.HardwareInventory)
	}{
		{
			name: "hostname",
			mutate: func(s *rackosv1.HardwareInventory) {
				s.Hostname = ""
			},
		},
		{
			name: "os",
			mutate: func(s *rackosv1.HardwareInventory) {
				s.Os = ""
			},
		},
		{
			name: "architecture",
			mutate: func(s *rackosv1.HardwareInventory) {
				s.Architecture = ""
			},
		},
		{
			name: "logical_cpus",
			mutate: func(s *rackosv1.HardwareInventory) {
				s.LogicalCpus = 0
			},
		},
		{
			name: "collected_at_unix_millis",
			mutate: func(s *rackosv1.HardwareInventory) {
				s.CollectedAtUnixMillis = 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validInventorySnapshot(now)
			tt.mutate(snapshot)

			err := Validate(snapshot, now)

			if !errors.Is(err, ErrInvalidInventory) {
				t.Fatalf(
					"expected validation error, got %v",
					err,
				)
			}

			if !strings.Contains(err.Error(), tt.name) {
				t.Fatal("error does not identify invalid field")
			}
		})
	}
}

func TestValidateText(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		required bool
		valid    bool
	}{
		{
			name:     "valid text",
			value:    "Apple M4 Pro",
			required: true,
			valid:    true,
		},
		{
			name:  "optional empty text",
			value: "",
			valid: true,
		},
		{
			name:     "required empty text",
			value:    "",
			required: true,
		},
		{
			name:     "whitespace only",
			value:    "   ",
			required: true,
		},
		{
			name:  "leading whitespace",
			value: " Apple",
		},
		{
			name:  "trailing whitespace",
			value: "Apple ",
		},
		{
			name:  "newline",
			value: "Apple\nProcessor",
		},
		{
			name:  "tab",
			value: "Apple\tProcessor",
		},
		{
			name:  "null character",
			value: "Apple\x00Processor",
		},
		{
			name:  "escape character",
			value: "Apple\x1bProcessor",
		},
		{
			name:  "invalid UTF-8",
			value: string([]byte{0xff, 0xfe}),
		},
		{
			name:  "valid Unicode",
			value: "测试处理器",
			valid: true,
		},
		{
			name:  "maximum length",
			value: strings.Repeat("a", 64),
			valid: true,
		},
		{
			name:  "excessive length",
			value: strings.Repeat("a", 65),
		},
		{
			name:  "Unicode byte length exceeded",
			value: strings.Repeat("é", 33),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateText(
				"test_field",
				tt.value,
				64,
				tt.required,
			)

			if tt.valid {
				if err != nil {
					t.Fatal(err)
				}
				return
			}

			if !errors.Is(err, ErrInvalidInventory) {
				t.Fatalf(
					"expected validation error, got %v",
					err,
				)
			}
		})
	}
}

func TestValidateInventoryTextLimits(t *testing.T) {
	now := validationTestTime()

	tests := []struct {
		name   string
		mutate func(*rackosv1.HardwareInventory)
	}{
		{
			name: "hostname",
			mutate: func(s *rackosv1.HardwareInventory) {
				s.Hostname = strings.Repeat("a", 256)
			},
		},
		{
			name: "os",
			mutate: func(s *rackosv1.HardwareInventory) {
				s.Os = strings.Repeat("a", 65)
			},
		},
		{
			name: "architecture",
			mutate: func(s *rackosv1.HardwareInventory) {
				s.Architecture = strings.Repeat("a", 65)
			},
		},
		{
			name: "cpu_model",
			mutate: func(s *rackosv1.HardwareInventory) {
				s.CpuModel = strings.Repeat("a", 257)
			},
		},
		{
			name: "cpu_manufacturer",
			mutate: func(s *rackosv1.HardwareInventory) {
				s.CpuManufacturer = strings.Repeat("a", 257)
			},
		},
		{
			name: "hardware_model",
			mutate: func(s *rackosv1.HardwareInventory) {
				s.HardwareModel = strings.Repeat("a", 257)
			},
		},
		{
			name: "os_version",
			mutate: func(s *rackosv1.HardwareInventory) {
				s.OsVersion = strings.Repeat("a", 129)
			},
		},
		{
			name: "kernel_version",
			mutate: func(s *rackosv1.HardwareInventory) {
				s.KernelVersion = strings.Repeat("a", 129)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validInventorySnapshot(now)
			tt.mutate(snapshot)

			err := Validate(snapshot, now)

			if !errors.Is(err, ErrInvalidInventory) {
				t.Fatalf(
					"expected validation error, got %v",
					err,
				)
			}

			if !strings.Contains(err.Error(), tt.name) {
				t.Fatal("incorrect validation field")
			}
		})
	}
}

func TestValidateCPUCounts(t *testing.T) {
	now := validationTestTime()

	tests := []struct {
		name     string
		logical  uint32
		host     uint32
		physical uint32
		valid    bool
	}{
		{
			name:     "normal topology",
			logical:  16,
			host:     16,
			physical: 8,
			valid:    true,
		},
		{
			name:     "restricted process CPUs",
			logical:  4,
			host:     16,
			physical: 8,
			valid:    true,
		},
		{
			name:    "unknown host topology",
			logical: 4,
			valid:   true,
		},
		{
			name:    "unknown physical cores",
			logical: 8,
			host:    8,
			valid:   true,
		},
		{
			name:     "maximum supported count",
			logical:  maxCPUCount,
			host:     maxCPUCount,
			physical: maxCPUCount,
			valid:    true,
		},
		{
			name: "zero logical CPUs",
		},
		{
			name:    "excessive logical CPUs",
			logical: maxCPUCount + 1,
		},
		{
			name:    "excessive host CPUs",
			logical: 4,
			host:    maxCPUCount + 1,
		},
		{
			name:     "excessive physical cores",
			logical:  4,
			physical: maxCPUCount + 1,
		},
		{
			name:     "invalid physical topology",
			logical:  8,
			host:     8,
			physical: 16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validInventorySnapshot(now)

			snapshot.LogicalCpus = tt.logical
			snapshot.HostLogicalCpus = tt.host
			snapshot.PhysicalCores = tt.physical

			err := Validate(snapshot, now)

			if tt.valid {
				if err != nil {
					t.Fatal(err)
				}
				return
			}

			if !errors.Is(err, ErrInvalidInventory) {
				t.Fatalf(
					"expected validation error, got %v",
					err,
				)
			}
		})
	}
}

func TestValidateMemoryBytes(t *testing.T) {
	now := validationTestTime()

	tests := []struct {
		name   string
		memory uint64
		valid  bool
	}{
		{
			name:  "unknown memory",
			valid: true,
		},
		{
			name:   "24 GiB",
			memory: 24 * 1024 * 1024 * 1024,
			valid:  true,
		},
		{
			name:   "1 TiB",
			memory: 1024 * 1024 * 1024 * 1024,
			valid:  true,
		},
		{
			name:   "maximum SQLite integer",
			memory: maxMemoryBytes,
			valid:  true,
		},
		{
			name:   "SQLite integer overflow",
			memory: maxMemoryBytes + 1,
		},
		{
			name:   "maximum unsigned integer",
			memory: ^uint64(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validInventorySnapshot(now)
			snapshot.MemoryBytes = tt.memory

			err := Validate(snapshot, now)

			if tt.valid {
				if err != nil {
					t.Fatal(err)
				}
				return
			}

			if !errors.Is(err, ErrInvalidInventory) {
				t.Fatalf(
					"expected validation error, got %v",
					err,
				)
			}
		})
	}
}

func TestValidateTimestamps(t *testing.T) {
	now := validationTestTime()

	tests := []struct {
		name      string
		collected int64
		boot      int64
		valid     bool
	}{
		{
			name:      "normal timestamps",
			collected: now.UnixMilli(),
			boot:      now.Add(-time.Hour).UnixMilli(),
			valid:     true,
		},
		{
			name:      "unknown boot time",
			collected: now.UnixMilli(),
			valid:     true,
		},
		{
			name:      "older inventory",
			collected: now.Add(-7 * 24 * time.Hour).UnixMilli(),
			boot:      now.Add(-8 * 24 * time.Hour).UnixMilli(),
			valid:     true,
		},
		{
			name:      "allowed future clock skew",
			collected: now.Add(4 * time.Minute).UnixMilli(),
			valid:     true,
		},
		{
			name:      "exact clock skew boundary",
			collected: now.Add(maxFutureClockSkew).UnixMilli(),
			valid:     true,
		},
		{
			name: "future timestamp beyond boundary",
			collected: now.Add(
				maxFutureClockSkew + time.Millisecond,
			).UnixMilli(),
		},
		{
			name: "zero collection timestamp",
		},
		{
			name:      "negative collection timestamp",
			collected: -1,
		},
		{
			name:      "negative boot timestamp",
			collected: now.UnixMilli(),
			boot:      -1,
		},
		{
			name:      "boot after collection",
			collected: now.UnixMilli(),
			boot:      now.Add(time.Millisecond).UnixMilli(),
		},
		{
			name:      "boot equals collection",
			collected: now.UnixMilli(),
			boot:      now.UnixMilli(),
			valid:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := validInventorySnapshot(now)

			snapshot.CollectedAtUnixMillis = tt.collected
			snapshot.BootTimeUnixMillis = tt.boot

			err := Validate(snapshot, now)

			if tt.valid {
				if err != nil {
					t.Fatal(err)
				}
				return
			}

			if !errors.Is(err, ErrInvalidInventory) {
				t.Fatalf(
					"expected validation error, got %v",
					err,
				)
			}
		})
	}
}

func TestValidateDoesNotMutateInventory(t *testing.T) {
	now := validationTestTime()

	snapshot := validInventorySnapshot(now)

	original := proto.Clone(snapshot)

	if err := Validate(snapshot, now); err != nil {
		t.Fatal(err)
	}

	if !proto.Equal(snapshot, original) {
		t.Fatal("validation modified inventory")
	}
}

func TestValidateDoesNotExposeInvalidValues(t *testing.T) {
	now := validationTestTime()

	snapshot := validInventorySnapshot(now)

	const privateValue = "private-test-value"

	snapshot.Hostname = privateValue + "\n"

	err := Validate(snapshot, now)

	if !errors.Is(err, ErrInvalidInventory) {
		t.Fatalf(
			"expected validation error, got %v",
			err,
		)
	}

	if strings.Contains(err.Error(), privateValue) {
		t.Fatal("validation error exposed raw field contents")
	}
}

func TestValidateFuturePlatform(t *testing.T) {
	now := validationTestTime()

	snapshot := validInventorySnapshot(now)

	snapshot.Os = "future-os"
	snapshot.Architecture = "future-architecture"

	if err := Validate(snapshot, now); err != nil {
		t.Fatal(err)
	}
}
