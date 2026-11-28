package main

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/config"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/session"
)

// Observe the daemon's actual startup message
// without requiring a fixed management port.
type sessionStartupWriter struct {
	ready chan struct{}
	once  sync.Once
}

func (w *sessionStartupWriter) Write(
	p []byte,
) (int, error) {
	if bytes.Contains(
		p,
		[]byte("RackOS appliance is running"),
	) {
		w.once.Do(func() {
			close(w.ready)
		})
	}

	return len(p), nil
}

func prepareDaemonSessionDatabase(
	t *testing.T,
	dir string,
) (
	*sql.DB,
	admin.Administrator,
	session.IssuedSession,
) {
	t.Helper()

	ctx := context.Background()

	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	// Synthetic digest: this test exercises daemon
	// startup, not password verification.
	account, err := admin.NewRepository(db).
		CreateInitial(
			ctx,
			"admin",
			admin.PasswordDigest{
				Scheme: admin.PasswordScheme,
			},
		)
	if err != nil {
		t.Fatal(err)
	}

	issued, err := session.NewRepository(db).
		Issue(ctx, account)
	if err != nil {
		t.Fatal(err)
	}

	return db, account, issued
}

func assertDaemonSessionCount(
	t *testing.T,
	db *sql.DB,
	expected int,
) {
	t.Helper()

	var count int

	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM administrator_sessions
	`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != expected {
		t.Fatalf(
			"expected %d sessions, got %d",
			expected,
			count,
		)
	}
}

func daemonSessionTestConfig(
	dir string,
) config.Config {
	cfg := config.Default()

	cfg.DataDir = dir
	cfg.HTTPAddr = "127.0.0.1:0"
	cfg.GRPCAddr = "127.0.0.1:0"

	cfg.ManagementTLS = true
	cfg.PerformanceRetention.RetentionDuration = 0

	return cfg
}

func TestDaemonSessionStartupInvalidatesPersistedSessions(
	t *testing.T,
) {
	t.Setenv("RACKOS_GRPC_TLS_NAMES", "127.0.0.1")

	dir := t.TempDir()

	db, account, issued :=
		prepareDaemonSessionDatabase(t, dir)

	assertDaemonSessionCount(t, db, 1)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := daemonSessionTestConfig(dir)

	writer := &sessionStartupWriter{
		ready: make(chan struct{}),
	}

	logger := slog.New(
		slog.NewTextHandler(writer, nil),
	)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		20*time.Second,
	)

	done := make(chan struct{})
	result := make(chan error, 1)

	go func() {
		defer close(done)

		result <- run(ctx, cfg, logger)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	select {
	case <-writer.ready:
		// The startup invalidation must already
		// have committed before this message.

	case err := <-result:
		t.Fatalf("daemon exited during startup: %v", err)

	case <-ctx.Done():
		t.Fatal("daemon startup timed out")
	}

	// Open another database handle while the
	// daemon is running.
	inspectionDB, err := database.OpenExisting(
		ctx,
		dir,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer inspectionDB.Close()

	assertDaemonSessionCount(
		t,
		inspectionDB,
		0,
	)

	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatal("daemon shutdown failed:", err)
		}

	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not shut down")
	}

	current, err := admin.NewRepository(inspectionDB).
		Get(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !current.PasswordChangedAt.Equal(
		account.PasswordChangedAt,
	) {
		t.Fatal("startup changed administrator credentials")
	}

	var count int

	err = inspectionDB.QueryRow(`
		SELECT COUNT(*)
		FROM audit_events
		WHERE event_type = 'auth.login.succeeded'
			AND actor_id = ?
			AND target_id = ?
	`,
		account.ID,
		issued.Session.ID,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatal("startup invalidation removed audit history")
	}
}

func TestDaemonSessionStartupFailsIfInvalidationFails(
	t *testing.T,
) {
	t.Setenv("RACKOS_GRPC_TLS_NAMES", "127.0.0.1")

	dir := t.TempDir()

	db, _, _ := prepareDaemonSessionDatabase(
		t,
		dir,
	)

	_, err := db.Exec(`
		CREATE TRIGGER test_reject_startup_invalidation
		BEFORE DELETE ON administrator_sessions
		BEGIN
			SELECT RAISE(
				ABORT,
				'simulated startup invalidation failure'
			);
		END
	`)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	writer := &sessionStartupWriter{
		ready: make(chan struct{}),
	}

	logger := slog.New(
		slog.NewTextHandler(writer, nil),
	)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()

	err = run(
		ctx,
		daemonSessionTestConfig(dir),
		logger,
	)

	if err == nil {
		t.Fatal("daemon ignored startup invalidation failure")
	}

	if !strings.Contains(
		err.Error(),
		"initialize administrator sessions",
	) {
		t.Fatalf("unexpected startup error: %v", err)
	}

	select {
	case <-writer.ready:
		t.Fatal("daemon reported readiness after invalidation failure")

	default:
	}

	inspectionDB, err := database.OpenExisting(
		context.Background(),
		dir,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer inspectionDB.Close()

	assertDaemonSessionCount(
		t,
		inspectionDB,
		1,
	)
}
