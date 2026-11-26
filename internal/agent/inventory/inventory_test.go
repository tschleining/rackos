package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCollect(t *testing.T) {
	before := time.Now().UTC()

	result, err := Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	after := time.Now().UTC()

	if strings.TrimSpace(result.Hostname) == "" {
		t.Fatal("hostname is empty")
	}

	if result.OS != runtime.GOOS {
		t.Errorf(
			"expected OS %s, got %s",
			runtime.GOOS,
			result.OS,
		)
	}

	if result.Architecture != runtime.GOARCH {
		t.Errorf(
			"expected architecture %s, got %s",
			runtime.GOARCH,
			result.Architecture,
		)
	}

	if result.LogicalCPUs < 1 {
		t.Fatal("invalid logical CPU count")
	}

	if result.LogicalCPUs != runtime.NumCPU() {
		t.Error("logical CPU count does not match runtime")
	}

	if result.CollectedAt.Before(before) ||
		result.CollectedAt.After(after) {
		t.Fatal("collection timestamp is outside expected range")
	}

	if result.CollectedAt.Location() != time.UTC {
		t.Fatal("collection timestamp must be UTC")
	}
}

func TestCollectHostname(t *testing.T) {
	expected, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}

	result, err := Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if result.Hostname != strings.TrimSpace(expected) {
		t.Errorf(
			"expected hostname %q, got %q",
			expected,
			result.Hostname,
		)
	}
}

func TestCollectCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	result, err := Collect(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if result != (Inventory{}) {
		t.Fatal("expected empty inventory on cancellation")
	}
}

func TestCollectExpiredDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(
		context.Background(),
		time.Now().Add(-time.Second),
	)
	defer cancel()

	result, err := Collect(ctx)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf(
			"expected context.DeadlineExceeded, got %v",
			err,
		)
	}

	if result != (Inventory{}) {
		t.Fatal("expected empty inventory after deadline")
	}
}

func TestCollectRepeatedly(t *testing.T) {
	first, err := Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	second, err := Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if first.Hostname != second.Hostname ||
		first.OS != second.OS ||
		first.Architecture != second.Architecture ||
		first.LogicalCPUs != second.LogicalCPUs {
		t.Fatal("system inventory changed unexpectedly")
	}

	if second.CollectedAt.Before(first.CollectedAt) {
		t.Fatal("collection timestamp moved backwards")
	}
}

func TestInventoryJSON(t *testing.T) {
	now := time.Now().UTC()
	bootTime := now.Add(-24 * time.Hour)

	result := Inventory{
		Hostname:        "test-workstation",
		OS:              "darwin",
		Architecture:    "arm64",
		LogicalCPUs:     10,
		HostLogicalCPUs: 10,
		PhysicalCores:   10,
		CPUModel:        "Test Processor",
		CPUManufacturer: "Test Manufacturer",
		MemoryBytes:     32 * 1024 * 1024 * 1024,
		HardwareModel:   "TestModel1,1",
		OSVersion:       "26.0",
		KernelVersion:   "25.0.0",
		BootTime:        &bootTime,
		CollectedAt:     now,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage

	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}

	expected := []string{
		"hostname",
		"os",
		"architecture",
		"logical_cpus",
		"host_logical_cpus",
		"physical_cores",
		"cpu_model",
		"cpu_manufacturer",
		"memory_bytes",
		"hardware_model",
		"os_version",
		"kernel_version",
		"boot_time",
		"collected_at",
	}

	if len(fields) != len(expected) {
		t.Fatalf(
			"expected %d JSON fields, got %d",
			len(expected),
			len(fields),
		)
	}

	for _, field := range expected {
		if _, ok := fields[field]; !ok {
			t.Errorf("missing JSON field: %s", field)
		}
	}

	var decoded Inventory

	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.BootTime == nil ||
		!decoded.BootTime.Equal(bootTime) {
		t.Fatal("boot timestamp did not survive JSON round trip")
	}

	// Compare all serialized fields after decoding.
	encodedAgain, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != string(encodedAgain) {
		t.Fatal("inventory JSON round trip failed")
	}
}

func TestCollectConcurrent(t *testing.T) {
	const workers = 16

	for i := 0; i < workers; i++ {
		t.Run(
			time.Duration(i).String(),
			func(t *testing.T) {
				t.Parallel()

				result, err := Collect(
					context.Background(),
				)
				if err != nil {
					t.Fatal(err)
				}

				if result.Hostname == "" ||
					result.LogicalCPUs < 1 {
					t.Fatal("invalid concurrent inventory result")
				}
			},
		)
	}
}

func TestInventoryOptionalFields(t *testing.T) {
	result := Inventory{
		Hostname:     "test-workstation",
		OS:           "test",
		Architecture: "amd64",
		LogicalCPUs:  4,
		CollectedAt:  time.Now().UTC(),
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage

	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}

	expected := []string{
		"hostname",
		"os",
		"architecture",
		"logical_cpus",
		"collected_at",
	}

	if len(fields) != len(expected) {
		t.Fatal("unavailable hardware fields were not omitted")
	}

	for _, field := range expected {
		if _, ok := fields[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}
}

func TestCollectPlatformIntegration(t *testing.T) {
	result, err := Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	switch runtime.GOOS {
	case "darwin", "linux":
		if result.HostLogicalCPUs < 1 {
			t.Fatal("missing host logical CPU count")
		}

		if result.MemoryBytes == 0 {
			t.Fatal("missing system memory")
		}

		if result.KernelVersion == "" {
			t.Fatal("missing kernel version")
		}

		if result.BootTime == nil ||
			result.BootTime.IsZero() {
			t.Fatal("missing system boot time")
		}

		if result.PhysicalCores > result.HostLogicalCPUs {
			t.Fatal("invalid CPU topology")
		}

	default:
		if result.HostLogicalCPUs != 0 ||
			result.MemoryBytes != 0 ||
			result.BootTime != nil {
			t.Fatal("unexpected platform hardware information")
		}
	}
}
