package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/performance"
)

const storageTestDeviceID = "e2a104d8-9c72-4b31-8fa2-63001b24c9e8"

func setupStorageCLIDatabase(
	t *testing.T,
	withSamples bool,
) string {
	t.Helper()

	ctx := context.Background()
	dir := t.TempDir()

	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if !withSamples {
		return dir
	}

	repository := devices.NewRepository(db)

	_, err = repository.Create(
		ctx,
		storageTestDeviceID,
		devices.DeviceInfo{
			Hostname:     "storage-test.local",
			OS:           "linux",
			Architecture: "arm64",
			AgentVersion: "0.1.0-test",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	base := time.Date(
		2026, 11, 27,
		12, 0, 0, 0,
		time.UTC,
	)

	for i := 0; i < 2; i++ {
		collectedAt := base.Add(
			time.Duration(i) * time.Minute,
		)

		receivedAt := collectedAt.Add(time.Second)

		_, err := db.ExecContext(ctx, `
			INSERT INTO device_performance (
				device_id,
				sample_id,
				collected_at,
				received_at,
				payload_version,
				payload
			) VALUES (?, ?, ?, ?, 1, zeroblob(1024))
		`,
			storageTestDeviceID,
			fmt.Sprintf("%032x", i),
			collectedAt.UnixMilli(),
			receivedAt.UnixMilli(),
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func requireStorageOutput(
	t *testing.T,
	output string,
	expected ...string,
) {
	t.Helper()

	for _, value := range expected {
		if !strings.Contains(output, value) {
			t.Fatalf(
				"missing output %q:\n%s",
				value,
				output,
			)
		}
	}
}

func TestStorageCLIHelp(t *testing.T) {
	for _, args := range [][]string{
		{"storage"},
		{"storage", "help"},
		{"storage", "--help"},
		{"storage", "-h"},
	} {
		var stdout, stderr bytes.Buffer

		if err := run(
			args,
			&stdout,
			&stderr,
		); err != nil {
			t.Fatal(err)
		}

		requireStorageOutput(
			t,
			stdout.String(),
			"RackOS storage administration",
			"rackosctl storage stats",
			"read-only",
		)

		if stderr.Len() != 0 {
			t.Fatal("unexpected help error output")
		}
	}
}

func TestStorageCLIMainHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer

	err := run(
		[]string{"help"},
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireStorageOutput(
		t,
		stdout.String(),
		"rackosctl storage stats",
		"rackosctl storage help",
	)
}

func TestStorageCLIStatsHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer

	err := run(
		[]string{
			"storage", "stats", "--help",
		},
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireStorageOutput(
		t,
		stderr.String(),
		"data-dir",
	)
}

func TestStorageCLIEmptyStatistics(t *testing.T) {
	dir := setupStorageCLIDatabase(t, false)

	var stdout, stderr bytes.Buffer

	err := run(
		[]string{
			"storage", "stats",
			"--data-dir", dir,
		},
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireStorageOutput(
		t,
		stdout.String(),
		"Historical samples:    0",
		"Reporting devices:     0",
		"Payload size:          0 B",
		"Oldest collection:     none",
		"Latest collection:     none",
		"Oldest receipt:        none",
		"Latest receipt:        none",
		"SQLite Files",
		"SQLite Page Usage",
		"Reusable capacity:",
	)

	if stderr.Len() != 0 {
		t.Fatal("unexpected error output")
	}
}

func TestStorageCLIPopulatedStatistics(t *testing.T) {
	dir := setupStorageCLIDatabase(t, true)

	var stdout, stderr bytes.Buffer

	err := run(
		[]string{
			"storage", "stats",
			"--data-dir", dir,
		},
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireStorageOutput(
		t,
		stdout.String(),
		"Historical samples:    2",
		"Reporting devices:     1",
		"Payload size:          2.00 KiB (2048 bytes)",
		"Oldest collection:     2026-11-27T12:00:00Z",
		"Latest collection:     2026-11-27T12:01:00Z",
		"Oldest receipt:        2026-11-27T12:00:01Z",
		"Latest receipt:        2026-11-27T12:01:01Z",
		"Database size:",
		"WAL size:",
		"SHM size:",
		"Allocated capacity:",
		"Reusable capacity:",
		"Non-freelist capacity:",
	)

	if stderr.Len() != 0 {
		t.Fatal("unexpected error output")
	}

	// The command must not modify performance data.
	ctx := context.Background()

	db, err := database.OpenReadOnly(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	statistics, err :=
		performance.NewRepository(db).Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if statistics.TotalSamples != 2 ||
		statistics.PayloadBytes != 2048 {
		t.Fatal("storage inspection modified performance data")
	}
}

func TestStorageCLIEnvironmentDirectory(t *testing.T) {
	dir := setupStorageCLIDatabase(t, true)

	t.Setenv("RACKOS_DATA_DIR", dir)

	// Storage inspection should not depend on
	// unrelated daemon retention configuration.
	t.Setenv(
		"RACKOS_PERFORMANCE_RETENTION",
		"invalid",
	)

	var stdout, stderr bytes.Buffer

	err := run(
		[]string{"storage", "stats"},
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireStorageOutput(
		t,
		stdout.String(),
		"Historical samples:    2",
	)
}

func TestStorageCLIExplicitDirectoryPrecedence(t *testing.T) {
	emptyDir := setupStorageCLIDatabase(t, false)

	populatedDir := setupStorageCLIDatabase(t, true)

	t.Setenv("RACKOS_DATA_DIR", emptyDir)

	var stdout, stderr bytes.Buffer

	err := run(
		[]string{
			"storage", "stats",
			"--data-dir", populatedDir,
		},
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireStorageOutput(
		t,
		stdout.String(),
		"Historical samples:    2",
	)
}

func TestStorageCLIMissingDatabase(t *testing.T) {
	dir := filepath.Join(
		t.TempDir(),
		"missing",
	)

	var stdout, stderr bytes.Buffer

	err := run(
		[]string{
			"storage", "stats",
			"--data-dir", dir,
		},
		&stdout,
		&stderr,
	)

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected error: %v", err)
	}

	if stdout.Len() != 0 {
		t.Fatal("failed command returned partial report")
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("storage command created a data directory")
	}
}

func TestStorageCLIInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{"storage", "unknown"},
		{"storage", "stats", "--unknown"},
		{"storage", "stats", "--data-dir"},
		{"storage", "stats", "--data-dir", ""},
		{"storage", "stats", "--data-dir", " "},
		{"storage", "stats", "unexpected"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			err := run(
				args,
				&stdout,
				&stderr,
			)

			if err == nil {
				t.Fatal("invalid arguments accepted")
			}

			if stdout.Len() != 0 {
				t.Fatal("invalid command returned storage report")
			}
		})
	}
}

func TestStorageCLICanceledContext(t *testing.T) {
	dir := setupStorageCLIDatabase(t, true)

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	var stdout, stderr bytes.Buffer

	err := runStorage(
		ctx,
		[]string{
			"stats",
			"--data-dir", dir,
		},
		&stdout,
		&stderr,
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}

	if stdout.Len() != 0 {
		t.Fatal("canceled command returned storage report")
	}
}

func TestFormatStorageBytes(t *testing.T) {
	tests := []struct {
		value int64
		want  string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.00 KiB (1024 bytes)"},
		{1536, "1.50 KiB (1536 bytes)"},
		{1 << 20, "1.00 MiB (1048576 bytes)"},
		{1 << 30, "1.00 GiB (1073741824 bytes)"},
		{1 << 40, "1.00 TiB (1099511627776 bytes)"},
		{1 << 50, "1.00 PiB (1125899906842624 bytes)"},
		{1 << 60, "1.00 EiB (1152921504606846976 bytes)"},
		{
			math.MaxInt64,
			"8.00 EiB (9223372036854775807 bytes)",
		},
	}

	for _, tt := range tests {
		actual := formatStorageBytes(tt.value)

		if actual != tt.want {
			t.Errorf(
				"%d: expected %q, got %q",
				tt.value,
				tt.want,
				actual,
			)
		}
	}
}

func TestFormatStorageTimestamp(t *testing.T) {
	if formatStorageTimestamp(nil) != "none" {
		t.Fatal("incorrect missing timestamp representation")
	}

	value := time.Date(
		2026, 11, 27,
		7, 0, 0, 123000000,
		time.FixedZone("test", -5*60*60),
	)

	actual := formatStorageTimestamp(&value)

	if actual != "2026-11-27T12:00:00.123Z" {
		t.Fatalf("incorrect timestamp: %s", actual)
	}
}

type storageTestFailingWriter struct {
	err error
}

func (w storageTestFailingWriter) Write(
	_ []byte,
) (int, error) {
	return 0, w.err
}

func TestStorageCLIOutputFailure(t *testing.T) {
	injectedError := errors.New(
		"injected output failure",
	)

	err := writeStorageStatistics(
		storageTestFailingWriter{
			err: injectedError,
		},
		performance.StorageStatistics{},
		database.StorageUsage{},
	)

	if !errors.Is(err, injectedError) {
		t.Fatalf("unexpected output error: %v", err)
	}
}
