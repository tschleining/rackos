package audit

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/database"
)

const testAdministratorID = "11111111-1111-4111-8111-111111111111"

func openAuditTestDatabase(t *testing.T) *sql.DB {
	t.Helper()

	db, err := database.Open(
		context.Background(),
		t.TempDir(),
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func assertAuditCount(
	t *testing.T,
	db *sql.DB,
	expected int,
) {
	t.Helper()

	var count int

	err := db.QueryRow(
		"SELECT COUNT(*) FROM audit_events",
	).Scan(&count)
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

func TestAuditEventIdentity(t *testing.T) {
	seen := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id, err := newEventID()
		if err != nil {
			t.Fatal(err)
		}

		decoded, err := hex.DecodeString(id)
		if err != nil || len(decoded) != 16 {
			t.Fatal("invalid audit event identity")
		}

		if hex.EncodeToString(decoded) != id {
			t.Fatal("noncanonical audit event identity")
		}

		if seen[id] {
			t.Fatal("duplicate audit event identity")
		}

		seen[id] = true
	}
}

func TestAuditRepositoryCommit(t *testing.T) {
	ctx := context.Background()
	db := openAuditTestDatabase(t)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	repository := NewTxRepository(tx)

	now := time.UnixMilli(1800000000000).UTC()

	repository.now = func() time.Time {
		return now
	}

	for _, event := range []EventType{
		EventAdminCreated,
		EventAdminPasswordChanged,
	} {
		err := repository.RecordAdministratorChange(
			ctx,
			event,
			testAdministratorID,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertAuditCount(t, db, 2)

	var count int

	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM audit_events
		WHERE occurred_at = ?
			AND actor_kind = 'local_operator'
			AND actor_id IS NULL
			AND target_kind = 'administrator'
			AND target_id = ?
			AND outcome = 'success'
	`,
		now.UnixMilli(),
		testAdministratorID,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Fatal("incorrect audit event metadata")
	}

	err = db.QueryRow(`
		SELECT COUNT(DISTINCT id)
		FROM audit_events
	`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Fatal("audit event identity was reused")
	}
}

func TestAuditRepositoryRollback(t *testing.T) {
	ctx := context.Background()
	db := openAuditTestDatabase(t)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	repository := NewTxRepository(tx)

	err = repository.RecordAdministratorChange(
		ctx,
		EventAdminCreated,
		testAdministratorID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	assertAuditCount(t, db, 0)
}

func TestAuditRepositoryRejectsInvalidEvents(
	t *testing.T,
) {
	ctx := context.Background()
	db := openAuditTestDatabase(t)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	repository := NewTxRepository(tx)

	tests := []struct {
		event EventType
		id    string
	}{
		{"unknown.event", testAdministratorID},
		{"", testAdministratorID},
		{"admin.created\ninvalid", testAdministratorID},
		{EventAdminCreated, ""},
		{EventAdminCreated, "invalid"},
		{EventAdminPasswordChanged, "invalid"},
	}

	for _, tc := range tests {
		err := repository.RecordAdministratorChange(
			ctx,
			tc.event,
			tc.id,
		)

		if !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	repository.now = func() time.Time {
		return time.UnixMilli(0)
	}

	err = repository.RecordAdministratorChange(
		ctx,
		EventAdminCreated,
		testAdministratorID,
	)

	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("unexpected clock error: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertAuditCount(t, db, 0)
}

func TestAuditRepositoryRequiresTransaction(
	t *testing.T,
) {
	repository := NewTxRepository(nil)

	err := repository.RecordAdministratorChange(
		context.Background(),
		EventAdminCreated,
		testAdministratorID,
	)

	if !errors.Is(err, ErrTransactionRequired) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuditRepositoryCanceledContext(
	t *testing.T,
) {
	db := openAuditTestDatabase(t)

	tx, err := db.BeginTx(
		context.Background(),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	repository := NewTxRepository(tx)

	err = repository.RecordAdministratorChange(
		ctx,
		EventAdminCreated,
		testAdministratorID,
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertAuditCount(t, db, 0)
}

func TestAuditRepositoryPersistence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	err = NewTxRepository(tx).
		RecordAdministratorChange(
			ctx,
			EventAdminCreated,
			testAdministratorID,
		)
	if err != nil {
		t.Fatal(err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	assertAuditCount(t, reopened, 1)

	var eventType string
	var targetID string

	err = reopened.QueryRow(`
		SELECT event_type, target_id
		FROM audit_events
	`).Scan(&eventType, &targetID)
	if err != nil {
		t.Fatal(err)
	}

	if eventType != string(EventAdminCreated) ||
		targetID != testAdministratorID {
		t.Fatal("audit event did not survive reopening")
	}
}
