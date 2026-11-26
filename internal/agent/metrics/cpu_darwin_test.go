package metrics

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDarwinReadCPUCounters(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	first, err := readCPUCounters(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if first.Busy == 0 && first.Idle == 0 {
		t.Fatal("all CPU counters are zero")
	}

	t.Logf(
		"Native CPU counters: busy=%d idle=%d",
		first.Busy,
		first.Idle,
	)

	timer := time.NewTimer(time.Second)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-timer.C:
	}

	second, err := readCPUCounters(ctx)
	if err != nil {
		t.Fatal(err)
	}

	usage, err := calculateCPUUsage(first, second)
	if err != nil {
		t.Fatal(err)
	}

	if usage < 0 || usage > 100 {
		t.Fatalf(
			"invalid CPU utilization: %f",
			usage,
		)
	}

	t.Logf(
		"Native CPU utilization: %.2f%%",
		usage,
	)
}

func TestDarwinReadCPUCountersCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	counters, err := readCPUCounters(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected cancellation error, got %v",
			err,
		)
	}

	if counters != (cpuCounters{}) {
		t.Fatal("canceled collection returned CPU counters")
	}
}
