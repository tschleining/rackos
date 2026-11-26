package inventory

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestParseDarwinBootTime(t *testing.T) {
	input := "{ sec = 1700000000, usec = 123456 }"

	result, err := parseDarwinBootTime(input)
	if err != nil {
		t.Fatal(err)
	}

	expected := time.Unix(
		1700000000,
		123456000,
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

func TestParseDarwinBootTimeInvalid(t *testing.T) {
	inputs := []string{
		"",
		"invalid",
		"{ sec = 0, usec = 0 }",
		"{ sec = -1, usec = 0 }",
		"{ sec = 1700000000 }",
		"{ sec = 1700000000, usec = -1 }",
		"{ sec = 1700000000, usec = 1000000 }",
		"{ sec = abc, usec = 123456 }",
		"{ sec = 999999999999999999999, usec = 0 }",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			_, err := parseDarwinBootTime(input)

			if err == nil {
				t.Fatal("invalid boot time accepted")
			}
		})
	}
}

func TestDarwinCommandCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	_, err := darwinCommand(
		ctx,
		"/usr/bin/sw_vers",
		"-productVersion",
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}
}

func TestDarwinCommandFailure(t *testing.T) {
	_, err := darwinCommand(
		context.Background(),
		"/usr/bin/false",
	)

	if err == nil {
		t.Fatal("expected command failure")
	}
}

func TestDarwinSysctlInvalidKey(t *testing.T) {
	_, err := darwinSysctl(
		context.Background(),
		"rackos.nonexistent.test.key",
	)

	if err == nil {
		t.Fatal("invalid sysctl key accepted")
	}
}

func TestDarwinHardwareCollection(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	result, err := collectPlatformHardware(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if result.PhysicalCPUs < 1 {
		t.Fatal("invalid physical core count")
	}

	if result.HostLogicalCPUs < result.PhysicalCPUs {
		t.Fatal("invalid CPU topology")
	}

	if result.MemoryBytes == 0 {
		t.Fatal("missing physical memory")
	}

	if result.HardwareModel == "" {
		t.Fatal("missing hardware model")
	}

	if result.OSVersion == "" {
		t.Fatal("missing macOS version")
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
