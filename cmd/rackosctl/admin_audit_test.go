package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/audit"
	"github.com/tschleining/rackos/internal/appliance/auth"
)

func assertAdminAuditEventCount(
	t *testing.T,
	db *sql.DB,
	expected int,
) {
	t.Helper()

	var count int

	err := db.QueryRow(`
		SELECT COUNT(*) FROM audit_events
	`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != expected {
		t.Fatalf(
			"expected %d audit events, got %d",
			expected,
			count,
		)
	}
}

func assertAdminAuditEvent(
	t *testing.T,
	db *sql.DB,
	event audit.EventType,
	administratorID string,
) {
	t.Helper()

	var count int

	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM audit_events
		WHERE event_type = ?
			AND actor_kind = 'local_operator'
			AND actor_id IS NULL
			AND target_kind = 'administrator'
			AND target_id = ?
			AND outcome = 'success'
			AND occurred_at > 0
	`,
		string(event),
		administratorID,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf(
			"expected exactly one %s audit event",
			event,
		)
	}
}

func TestAdminAuditSuccessfulOperations(
	t *testing.T,
) {
	ctx := context.Background()
	dir, db := setupDeviceCommandDB(t)

	_, _, err := executeAdminCLI(
		adminTestPrompt(
			testInitialAdminPassword,
			testInitialAdminPassword,
		),
		"init",
		"--username", "admin",
		"--confirm-offline",
		"--data-dir", dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	repository := admin.NewRepository(db)

	account, err := repository.GetByUsername(
		ctx,
		"admin",
	)
	if err != nil {
		t.Fatal(err)
	}

	assertAdminAuditEventCount(t, db, 1)

	assertAdminAuditEvent(
		t,
		db,
		audit.EventAdminCreated,
		account.ID,
	)

	// Duplicate initialization must not produce
	// a successful creation event.
	_, _, err = executeAdminCLI(
		adminTestPrompt(),
		"init",
		"--username", "another-admin",
		"--confirm-offline",
		"--data-dir", dir,
	)

	if !errors.Is(err, admin.ErrAlreadyInitialized) {
		t.Fatalf("unexpected error: %v", err)
	}

	assertAdminAuditEventCount(t, db, 1)

	_, _, err = executeAdminCLI(
		adminTestPrompt(
			testReplacementAdminPassword,
			testReplacementAdminPassword,
		),
		"reset-password",
		"--username", "admin",
		"--confirm-offline",
		"--data-dir", dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	assertAdminAuditEventCount(t, db, 2)

	assertAdminAuditEvent(
		t,
		db,
		audit.EventAdminPasswordChanged,
		account.ID,
	)

	// Read-only status commands must not generate
	// administrative mutation events.
	_, _, err = executeAdminCLI(
		adminTestPrompt(),
		"status",
		"--data-dir", dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	assertAdminAuditEventCount(t, db, 2)
}

func installAuditFailureTrigger(
	t *testing.T,
	db *sql.DB,
) {
	t.Helper()

	_, err := db.Exec(`
		CREATE TRIGGER test_reject_audit_event
		BEFORE INSERT ON audit_events
		BEGIN
			SELECT RAISE(
				ABORT,
				'simulated audit storage failure'
			);
		END
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAdminAuditInitializationRollback(
	t *testing.T,
) {
	ctx := context.Background()
	dir, db := setupDeviceCommandDB(t)

	installAuditFailureTrigger(t, db)

	stdout, _, err := executeAdminCLI(
		adminTestPrompt(
			testInitialAdminPassword,
			testInitialAdminPassword,
		),
		"init",
		"--username", "admin",
		"--confirm-offline",
		"--data-dir", dir,
	)

	if err == nil {
		t.Fatal("initialization ignored audit failure")
	}

	if !strings.Contains(
		err.Error(),
		"record administrative audit event",
	) {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(stdout, "initialized:") {
		t.Fatal("failed initialization reported success")
	}

	exists, err := admin.NewRepository(db).Exists(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if exists {
		t.Fatal("administrator creation was not rolled back")
	}

	assertAdminAuditEventCount(t, db, 0)
}

func TestAdminAuditPasswordRecoveryRollback(
	t *testing.T,
) {
	ctx := context.Background()
	dir, db := setupDeviceCommandDB(t)

	_, _, err := executeAdminCLI(
		adminTestPrompt(
			testInitialAdminPassword,
			testInitialAdminPassword,
		),
		"init",
		"--username", "admin",
		"--confirm-offline",
		"--data-dir", dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	repository := admin.NewRepository(db)

	before, err := repository.GetAuthenticationRecord(
		ctx,
		"admin",
	)
	if err != nil {
		t.Fatal(err)
	}

	installAuditFailureTrigger(t, db)

	stdout, _, err := executeAdminCLI(
		adminTestPrompt(
			testReplacementAdminPassword,
			testReplacementAdminPassword,
		),
		"reset-password",
		"--username", "admin",
		"--confirm-offline",
		"--data-dir", dir,
	)

	if err == nil {
		t.Fatal("password recovery ignored audit failure")
	}

	if !strings.Contains(
		err.Error(),
		"record administrative audit event",
	) {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(stdout, "password replaced:") {
		t.Fatal("failed recovery reported success")
	}

	after, err := repository.GetAuthenticationRecord(
		ctx,
		"admin",
	)
	if err != nil {
		t.Fatal(err)
	}

	if before.Password != after.Password {
		t.Fatal("password replacement was not rolled back")
	}

	if !before.Administrator.PasswordChangedAt.Equal(
		after.Administrator.PasswordChangedAt,
	) {
		t.Fatal("password revision changed after rollback")
	}

	if !before.Administrator.UpdatedAt.Equal(
		after.Administrator.UpdatedAt,
	) {
		t.Fatal("update timestamp changed after rollback")
	}

	valid, err := auth.VerifyPassword(
		ctx,
		[]byte(testInitialAdminPassword),
		after.Password,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !valid {
		t.Fatal("original password no longer authenticates")
	}

	valid, err = auth.VerifyPassword(
		ctx,
		[]byte(testReplacementAdminPassword),
		after.Password,
	)
	if err != nil {
		t.Fatal(err)
	}

	if valid {
		t.Fatal("failed password replacement authenticates")
	}

	// Only the original creation event should remain.
	assertAdminAuditEventCount(t, db, 1)

	assertAdminAuditEvent(
		t,
		db,
		audit.EventAdminCreated,
		before.Administrator.ID,
	)
}
