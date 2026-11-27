package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/agent/metrics"
)

func testMetricsSnapshot() metrics.Snapshot {
	return metrics.Snapshot{
		CPU: metrics.CPUUsage{
			UsagePercent:         25,
			SampleIntervalMillis: 1000,
		},
		Memory: metrics.MemoryUsage{
			TotalBytes:     1000,
			AvailableBytes: 400,
			UsedBytes:      600,
			UsagePercent:   60,
		},
		CollectedAt: time.Date(
			2026, time.November, 26,
			23, 0, 0, 0,
			time.UTC,
		),
	}
}

func TestRunMetricsJSON(t *testing.T) {
	expected := testMetricsSnapshot()

	var stdout bytes.Buffer
	calls := 0

	err := runMetricsWithCollector(
		context.Background(),
		nil,
		&stdout,
		func(ctx context.Context) (metrics.Snapshot, error) {
			calls++

			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("collection deadline is missing")
			}

			remaining := time.Until(deadline)

			if remaining <= 0 ||
				remaining > metricsCollectionTimeout {
				t.Fatal("invalid collection deadline")
			}

			return expected, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 1 {
		t.Fatalf("expected one collection, got %d", calls)
	}

	if !json.Valid(stdout.Bytes()) {
		t.Fatalf(
			"invalid JSON output: %s",
			stdout.String(),
		)
	}

	var actual metrics.Snapshot

	if err := json.Unmarshal(
		stdout.Bytes(),
		&actual,
	); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf(
			"expected %+v, got %+v",
			expected,
			actual,
		)
	}
}

func TestRunMetricsRejectsArguments(t *testing.T) {
	tests := [][]string{
		{"unexpected"},
		{"--unknown"},
		{"--state-dir", "./data/agent"},
		{"--address", "127.0.0.1:50051"},
	}

	for _, args := range tests {
		t.Run(args[0], func(t *testing.T) {
			var stdout bytes.Buffer

			err := runMetricsWithCollector(
				context.Background(),
				args,
				&stdout,
				func(ctx context.Context) (metrics.Snapshot, error) {
					t.Fatal(
						"collector invoked with invalid arguments",
					)
					return metrics.Snapshot{}, nil
				},
			)

			if err == nil {
				t.Fatal("expected argument validation error")
			}

			if stdout.Len() != 0 {
				t.Fatal("invalid arguments produced output")
			}
		})
	}
}

func TestRunMetricsCollectionFailure(t *testing.T) {
	expectedError := errors.New("collection failed")

	var stdout bytes.Buffer

	err := runMetricsWithCollector(
		context.Background(),
		nil,
		&stdout,
		func(ctx context.Context) (metrics.Snapshot, error) {
			return metrics.Snapshot{}, expectedError
		},
	)

	if !errors.Is(err, expectedError) {
		t.Fatalf(
			"expected collection error, got %v",
			err,
		)
	}

	if stdout.Len() != 0 {
		t.Fatal("failed collection produced output")
	}
}

func TestRunMetricsUnsupportedPlatform(t *testing.T) {
	var stdout bytes.Buffer

	err := runMetricsWithCollector(
		context.Background(),
		nil,
		&stdout,
		func(ctx context.Context) (metrics.Snapshot, error) {
			return metrics.Snapshot{},
				metrics.ErrUnsupportedPlatform
		},
	)

	if !errors.Is(err, metrics.ErrUnsupportedPlatform) {
		t.Fatalf(
			"expected unsupported platform error, got %v",
			err,
		)
	}

	if stdout.Len() != 0 {
		t.Fatal("unsupported platform produced output")
	}
}

func TestRunMetricsCanceledBeforeCollection(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	var stdout bytes.Buffer

	err := runMetricsWithCollector(
		ctx,
		nil,
		&stdout,
		func(ctx context.Context) (metrics.Snapshot, error) {
			t.Fatal("collector invoked after cancellation")
			return metrics.Snapshot{}, nil
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if stdout.Len() != 0 {
		t.Fatal("canceled command produced output")
	}
}

func TestRunMetricsCanceledDuringCollection(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	var stdout bytes.Buffer

	err := runMetricsWithCollector(
		ctx,
		nil,
		&stdout,
		func(ctx context.Context) (metrics.Snapshot, error) {
			cancel()

			return testMetricsSnapshot(), nil
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if stdout.Len() != 0 {
		t.Fatal("canceled collection produced output")
	}
}

func TestRunMetricsParentDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Second,
	)
	defer cancel()

	parentDeadline, _ := ctx.Deadline()

	var stdout bytes.Buffer

	err := runMetricsWithCollector(
		ctx,
		nil,
		&stdout,
		func(ctx context.Context) (metrics.Snapshot, error) {
			deadline, ok := ctx.Deadline()

			if !ok || !deadline.Equal(parentDeadline) {
				t.Fatal("parent deadline was not preserved")
			}

			return testMetricsSnapshot(), nil
		},
	)

	if err != nil {
		t.Fatal(err)
	}
}

func TestRunMetricsWriteFailure(t *testing.T) {
	expectedError := errors.New("output write failed")

	err := runMetricsWithCollector(
		context.Background(),
		nil,
		metricsErrorWriter{err: expectedError},
		func(ctx context.Context) (metrics.Snapshot, error) {
			return testMetricsSnapshot(), nil
		},
	)

	if !errors.Is(err, expectedError) {
		t.Fatalf(
			"expected output error, got %v",
			err,
		)
	}
}

type metricsErrorWriter struct {
	err error
}

func (w metricsErrorWriter) Write(p []byte) (int, error) {
	return 0, w.err
}

func TestRunMetricsHelp(t *testing.T) {
	for _, option := range []string{"--help", "-h"} {
		t.Run(option, func(t *testing.T) {
			var stdout bytes.Buffer

			logger := slog.New(
				slog.NewTextHandler(io.Discard, nil),
			)

			err := run(
				context.Background(),
				[]string{"metrics", option},
				strings.NewReader(""),
				&stdout,
				logger,
			)
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(
				stdout.String(),
				"Usage: rackos-agent metrics",
			) {
				t.Fatalf(
					"unexpected help output: %s",
					stdout.String(),
				)
			}
		})
	}
}

func TestRunMetricsDispatch(t *testing.T) {
	var stdout bytes.Buffer

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	err := run(
		ctx,
		[]string{"metrics"},
		strings.NewReader(""),
		&stdout,
		logger,
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if stdout.Len() != 0 {
		t.Fatal("canceled metrics command produced output")
	}
}

func TestRunMetricsStorageJSON(t *testing.T) {
	expected := metrics.Snapshot{
		CPU: metrics.CPUUsage{
			UsagePercent:         25,
			SampleIntervalMillis: 1000,
		},
		Memory: metrics.MemoryUsage{
			TotalBytes:     1000,
			AvailableBytes: 400,
			UsedBytes:      600,
			UsagePercent:   60,
		},
		Storage: &metrics.StorageUsage{
			Filesystems: []metrics.FilesystemUsage{
				{
					Source:         "/dev/sda1",
					MountPoint:     "/",
					FilesystemType: "ext4",
					ReadOnly:       false,
					Capacity: metrics.FilesystemCapacity{
						TotalBytes:     1000,
						FreeBytes:      350,
						AvailableBytes: 300,
						UsedBytes:      650,
						ReservedBytes:  50,
						UsagePercent:   65,
					},
				},
			},
			Disks: []metrics.DiskUsage{
				{
					Device:          "sda",
					ReadBytes:       4096000,
					WriteBytes:      2048000,
					ReadOperations:  1000,
					WriteOperations: 500,
				},
			},
			DiskRates: []metrics.DiskRate{
				{
					Device:                   "sda",
					ReadBytesPerSecond:       1024000,
					WriteBytesPerSecond:      512000,
					ReadOperationsPerSecond:  250,
					WriteOperationsPerSecond: 125,
					SampleIntervalMillis:     1000,
				},
			},
		},
		CollectedAt: time.Date(
			2026, 11, 26, 23, 0, 0, 0,
			time.UTC,
		),
	}

	var stdout bytes.Buffer

	err := runMetricsWithCollector(
		context.Background(),
		nil,
		&stdout,
		func(context.Context) (metrics.Snapshot, error) {
			return expected, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !json.Valid(stdout.Bytes()) {
		t.Fatal("metrics output is not valid JSON")
	}

	var actual metrics.Snapshot

	if err := json.Unmarshal(
		stdout.Bytes(),
		&actual,
	); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf(
			"unexpected metrics snapshot: %s",
			stdout.String(),
		)
	}

	// Verify the public JSON field names independently
	// of the Go structure definitions.
	var document map[string]json.RawMessage

	if err := json.Unmarshal(
		stdout.Bytes(),
		&document,
	); err != nil {
		t.Fatal(err)
	}

	for _, field := range []string{
		"cpu",
		"memory",
		"storage",
		"collected_at",
	} {
		if _, exists := document[field]; !exists {
			t.Fatalf("missing JSON field %q", field)
		}
	}

	var storage map[string]json.RawMessage

	if err := json.Unmarshal(
		document["storage"],
		&storage,
	); err != nil {
		t.Fatal(err)
	}

	for _, field := range []string{
		"filesystems",
		"disks",
		"disk_rates",
	} {
		raw, exists := storage[field]
		if !exists {
			t.Fatalf("missing storage field %q", field)
		}

		var entries []json.RawMessage

		if err := json.Unmarshal(raw, &entries); err != nil {
			t.Fatal(err)
		}

		if len(entries) != 1 {
			t.Fatalf(
				"expected one entry in %q, got %d",
				field,
				len(entries),
			)
		}
	}
}

func TestRunMetricsEmptyStorageJSON(t *testing.T) {
	snapshot := metrics.Snapshot{
		CPU: metrics.CPUUsage{
			UsagePercent:         10,
			SampleIntervalMillis: 1000,
		},
		Memory: metrics.MemoryUsage{
			TotalBytes:     1000,
			AvailableBytes: 500,
			UsedBytes:      500,
			UsagePercent:   50,
		},
		Storage: &metrics.StorageUsage{
			Filesystems: []metrics.FilesystemUsage{},
			Disks:       []metrics.DiskUsage{},
			DiskRates:   []metrics.DiskRate{},
		},
		CollectedAt: time.Now().UTC(),
	}

	var stdout bytes.Buffer

	err := runMetricsWithCollector(
		context.Background(),
		nil,
		&stdout,
		func(context.Context) (metrics.Snapshot, error) {
			return snapshot, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	var document struct {
		Storage map[string]json.RawMessage `json:"storage"`
	}

	if err := json.Unmarshal(
		stdout.Bytes(),
		&document,
	); err != nil {
		t.Fatal(err)
	}

	if document.Storage == nil {
		t.Fatal("storage object is missing")
	}

	for _, field := range []string{
		"filesystems",
		"disks",
		"disk_rates",
	} {
		value, exists := document.Storage[field]
		if !exists {
			t.Fatalf("missing storage field %q", field)
		}

		if string(value) != "[]" {
			t.Fatalf(
				"expected empty array for %q, got %s",
				field,
				value,
			)
		}
	}
}

func TestRunMetricsStorageFailure(t *testing.T) {
	expectedError := metrics.ErrInvalidFilesystem

	var stdout bytes.Buffer

	err := runMetricsWithCollector(
		context.Background(),
		nil,
		&stdout,
		func(context.Context) (metrics.Snapshot, error) {
			return metrics.Snapshot{}, expectedError
		},
	)

	if !errors.Is(err, expectedError) {
		t.Fatalf(
			"expected storage collection error, got %v",
			err,
		)
	}

	if stdout.Len() != 0 {
		t.Fatal("failed collection produced JSON output")
	}
}
