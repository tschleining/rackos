package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/config"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/retention"
)

const retentionTestDeviceID = "e2a104d8-9c72-4b31-8fa2-63001b24c9e8"

const retentionTestExpiredSampleID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

const retentionTestRecentSampleID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

// retentionTestLogWriter captures daemon logs and
// allows tests to cancel the appliance when specific
// lifecycle events have occurred.
//
// The slog text handler serializes calls to Write.
type retentionTestLogWriter struct {
	bytes.Buffer

	onWrite func(string)
}

func (w *retentionTestLogWriter) Write(
	data []byte,
) (int, error) {
	n, err := w.Buffer.Write(data)

	if w.onWrite != nil {
		w.onWrite(string(data))
	}

	return n, err
}

func testDaemonRetentionConfig(
	t *testing.T,
) config.Config {
	t.Helper()

	cfg := config.Default()

	cfg.DataDir = t.TempDir()

	// Request operating-system-assigned ports
	// to avoid conflicts with existing services.
	cfg.HTTPAddr = "127.0.0.1:0"
	cfg.GRPCAddr = "127.0.0.1:0"

	t.Setenv(
		"RACKOS_GRPC_TLS_NAMES",
		"127.0.0.1",
	)

	return cfg
}

// seedDaemonRetentionSamples creates isolated
// historical records on either side of a
// 24-hour retention cutoff.
//
// Payloads are deliberately opaque test bytes.
// Retention does not require protobuf decoding.
func seedDaemonRetentionSamples(
	t *testing.T,
	dataDir string,
) {
	t.Helper()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repository := devices.NewRepository(db)

	_, err = repository.Create(
		ctx,
		retentionTestDeviceID,
		devices.DeviceInfo{
			Hostname:     "retention-test.local",
			OS:           "linux",
			Architecture: "arm64",
			AgentVersion: "0.1.0-test",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().
		Truncate(time.Millisecond)

	samples := []struct {
		id      string
		receipt time.Time
	}{
		{
			retentionTestExpiredSampleID,
			now.Add(-48 * time.Hour),
		},
		{
			retentionTestRecentSampleID,
			now.Add(-time.Hour),
		},
	}

	for _, sample := range samples {
		_, err := db.ExecContext(ctx, `
			INSERT INTO device_performance (
				device_id,
				sample_id,
				collected_at,
				received_at,
				payload_version,
				payload
			) VALUES (?, ?, ?, ?, 1, X'FF')
		`,
			retentionTestDeviceID,
			sample.id,
			sample.receipt.Add(-time.Second).UnixMilli(),
			sample.receipt.UnixMilli(),
		)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func requireDaemonRetentionSamples(
	t *testing.T,
	dataDir string,
	expectedExpired int,
	expectedRecent int,
) {
	t.Helper()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tests := []struct {
		sampleID string
		expected int
	}{
		{
			retentionTestExpiredSampleID,
			expectedExpired,
		},
		{
			retentionTestRecentSampleID,
			expectedRecent,
		},
	}

	for _, tt := range tests {
		var count int

		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM device_performance
			WHERE device_id = ?
				AND sample_id = ?
		`,
			retentionTestDeviceID,
			tt.sampleID,
		).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}

		if count != tt.expected {
			t.Fatalf(
				"sample %s: expected %d records, got %d",
				tt.sampleID,
				tt.expected,
				count,
			)
		}
	}

	var total int

	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM device_performance
	`).Scan(&total)
	if err != nil {
		t.Fatal(err)
	}

	if total != expectedExpired+expectedRecent {
		t.Fatal("unexpected historical record count")
	}

	// Verify that cleanup did not remove
	// the registered device.
	repository := devices.NewRepository(db)

	if _, err := repository.Get(
		ctx,
		retentionTestDeviceID,
	); err != nil {
		t.Fatal(err)
	}

	var integrity string

	err = db.QueryRowContext(
		ctx,
		"PRAGMA integrity_check",
	).Scan(&integrity)
	if err != nil {
		t.Fatal(err)
	}

	if integrity != "ok" {
		t.Fatalf("database integrity failure: %s", integrity)
	}
}

func TestDaemonRetentionDisabled(t *testing.T) {
	cfg := testDaemonRetentionConfig(t)

	seedDaemonRetentionSamples(
		t,
		cfg.DataDir,
	)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	var output retentionTestLogWriter

	started := false

	output.onWrite = func(line string) {
		if strings.Contains(
			line,
			"RackOS appliance is running",
		) {
			started = true
			cancel()
		}
	}

	logger := slog.New(
		slog.NewTextHandler(
			&output,
			&slog.HandlerOptions{
				Level: slog.LevelDebug,
			},
		),
	)

	if err := run(ctx, cfg, logger); err != nil {
		t.Fatal(err)
	}

	if !started {
		t.Fatal("appliance did not complete startup")
	}

	logs := output.String()

	if !strings.Contains(
		logs,
		"Performance retention disabled",
	) {
		t.Fatal("missing disabled retention log")
	}

	if strings.Contains(
		logs,
		"Performance retention worker started",
	) {
		t.Fatal("disabled retention worker started")
	}

	if !strings.Contains(
		logs,
		"Database closed",
	) {
		t.Fatal("database did not close")
	}

	requireDaemonRetentionSamples(
		t,
		cfg.DataDir,
		1,
		1,
	)
}

func TestDaemonRetentionEnabled(t *testing.T) {
	cfg := testDaemonRetentionConfig(t)

	cfg.PerformanceRetention.RetentionDuration =
		retention.MinRetentionDuration

	cfg.PerformanceRetention.BatchSize = 10

	seedDaemonRetentionSamples(
		t,
		cfg.DataDir,
	)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	var output retentionTestLogWriter

	started := false
	cleanupCompleted := false

	output.onWrite = func(line string) {
		if strings.Contains(
			line,
			"RackOS appliance is running",
		) {
			started = true
		}

		if strings.Contains(
			line,
			"Performance retention cleanup completed",
		) {
			cleanupCompleted = true
		}

		// Wait for both service startup and
		// successful initial retention cleanup.
		if started && cleanupCompleted {
			cancel()
		}
	}

	logger := slog.New(
		slog.NewTextHandler(
			&output,
			&slog.HandlerOptions{
				Level: slog.LevelDebug,
			},
		),
	)

	if err := run(ctx, cfg, logger); err != nil {
		t.Fatal(err)
	}

	if !started || !cleanupCompleted {
		t.Fatalf(
			"appliance did not complete startup cleanup:\n%s",
			output.String(),
		)
	}

	logs := output.String()

	for _, message := range []string{
		"Performance retention worker started",
		"Performance retention cleanup completed",
		"deleted_samples=1",
		"Performance retention worker stopped",
		"Database closed",
	} {
		if !strings.Contains(logs, message) {
			t.Fatalf("missing log message: %s", message)
		}
	}

	workerStopped := strings.Index(
		logs,
		"Performance retention worker stopped",
	)

	databaseClosed := strings.Index(
		logs,
		"Database closed",
	)

	if workerStopped >= databaseClosed {
		t.Fatal("database closed before retention worker stopped")
	}

	requireDaemonRetentionSamples(
		t,
		cfg.DataDir,
		0,
		1,
	)
}

func TestDaemonRetentionInvalidPolicy(t *testing.T) {
	cfg := testDaemonRetentionConfig(t)

	cfg.PerformanceRetention.RetentionDuration =
		-time.Hour

	logger := slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)

	err := run(
		context.Background(),
		cfg,
		logger,
	)

	if !errors.Is(err, retention.ErrInvalidPolicy) {
		t.Fatalf("expected invalid policy error, got %v", err)
	}

	// Policy validation must occur before
	// the appliance database is initialized.
	path := filepath.Join(
		cfg.DataDir,
		database.Filename,
	)

	_, err = os.Stat(path)

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(
			"invalid policy initialized database: %v",
			err,
		)
	}
}

func TestDaemonRetentionListenerFailure(t *testing.T) {
	cfg := testDaemonRetentionConfig(t)

	cfg.PerformanceRetention.RetentionDuration =
		retention.MinRetentionDuration

	seedDaemonRetentionSamples(
		t,
		cfg.DataDir,
	)

	// Occupy the requested gRPC listening port
	// so daemon startup must fail.
	listener, err := net.Listen(
		"tcp",
		"127.0.0.1:0",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	cfg.GRPCAddr = listener.Addr().String()

	var output bytes.Buffer

	logger := slog.New(
		slog.NewTextHandler(
			&output,
			&slog.HandlerOptions{
				Level: slog.LevelDebug,
			},
		),
	)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	err = run(ctx, cfg, logger)

	if err == nil {
		t.Fatal("expected listener initialization failure")
	}

	if !strings.Contains(
		err.Error(),
		"listen on gRPC address",
	) {
		t.Fatalf("unexpected startup error: %v", err)
	}

	if strings.Contains(
		output.String(),
		"Performance retention worker started",
	) {
		t.Fatal("retention started during failed appliance startup")
	}

	if !strings.Contains(
		output.String(),
		"Database closed",
	) {
		t.Fatal("failed startup did not close database")
	}

	requireDaemonRetentionSamples(
		t,
		cfg.DataDir,
		1,
		1,
	)
}
