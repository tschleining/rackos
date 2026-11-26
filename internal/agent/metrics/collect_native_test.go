//go:build linux || darwin

package metrics

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestCollectNative(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	startedAt := time.Now()

	snapshot, err := Collect(ctx)
	if err != nil {
		t.Fatal(err)
	}

	finishedAt := time.Now()

	requireMetricPercent(
		t,
		snapshot.CPU.UsagePercent,
		snapshot.CPU.UsagePercent,
	)

	if snapshot.CPU.SampleIntervalMillis <
		DefaultCPUSampleInterval.Milliseconds() {
		t.Fatal("CPU sampling interval is too short")
	}

	if snapshot.Memory.TotalBytes == 0 {
		t.Fatal("memory capacity is zero")
	}

	if snapshot.Memory.AvailableBytes >
		snapshot.Memory.TotalBytes {
		t.Fatal("invalid available memory")
	}

	if snapshot.Memory.UsedBytes !=
		snapshot.Memory.TotalBytes-
			snapshot.Memory.AvailableBytes {
		t.Fatal("memory accounting is inconsistent")
	}

	expectedUsage := float64(
		snapshot.Memory.UsedBytes,
	) / float64(
		snapshot.Memory.TotalBytes,
	) * 100

	requireMetricPercent(
		t,
		snapshot.Memory.UsagePercent,
		expectedUsage,
	)

	if snapshot.CollectedAt.IsZero() {
		t.Fatal("collection timestamp is missing")
	}

	if snapshot.CollectedAt.Location() != time.UTC {
		t.Fatal("collection timestamp is not UTC")
	}

	if snapshot.CollectedAt.Before(startedAt) ||
		snapshot.CollectedAt.After(finishedAt) {
		t.Fatal("timestamp outside collection window")
	}

	data, err := json.MarshalIndent(
		snapshot,
		"",
		"  ",
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf(
		"Native metrics snapshot:\n%s",
		data,
	)
}
