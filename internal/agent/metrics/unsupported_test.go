//go:build !linux && !darwin

package metrics

import (
	"context"
	"errors"
	"testing"
)

func TestUnsupportedCPUCounters(t *testing.T) {
	counters, err := readCPUCounters(
		context.Background(),
	)

	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf(
			"expected ErrUnsupportedPlatform, got %v",
			err,
		)
	}

	if counters != (cpuCounters{}) {
		t.Fatal("unsupported platform returned CPU counters")
	}
}

func TestUnsupportedMemoryUsage(t *testing.T) {
	memory, err := readMemoryUsage(
		context.Background(),
	)

	if !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf(
			"expected ErrUnsupportedPlatform, got %v",
			err,
		)
	}

	if memory != (MemoryUsage{}) {
		t.Fatal("unsupported platform returned memory usage")
	}
}

func TestUnsupportedCPUCountersCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	counters, err := readCPUCounters(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if counters != (cpuCounters{}) {
		t.Fatal("canceled collection returned CPU counters")
	}
}

func TestUnsupportedMemoryUsageCanceled(t *testing.T) {
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
