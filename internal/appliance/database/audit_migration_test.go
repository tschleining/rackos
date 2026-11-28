package database

import (
	"bytes"
	"context"
	"database/sql"
	"testing"
)

const auditMigrationTestID = "cccccccccccccccccccccccccccccccc"

const auditMigrationInsertSQL = `
	INSERT INTO audit_events (
		id,
		occurred_at,
		event_type,
		actor_kind,
		actor_id,
		target_kind,
		target_id,
		outcome
	)
	VALUES (
		?, 1800000000000,
		'admin.created',
		'local_operator',
		NULL,
		'administrator',
		?,
		'success'
	)
`

func createAuditVersionEightDatabase(
	t *testing.T,
	dir string,
) {
	t.Helper()

	createSessionVersionSevenDatabase(t, dir)

	db, err := openRawExistingTestDatabase(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)

	inventoryExec(t, db, "PRAGMA foreign_keys = ON")

	// Apply only Migration 008.
	inventoryExec(t, db, migrations[7])

	inventoryExec(t, db, "PRAGMA user_version = 8")

	insertSessionMigrationRecord(
		t,
		db,
		sessionMigrationTestID,
		bytes.Repeat([]byte{0x11}, 32),
	)

	assertInventorySchemaVersion(t, db, 8)
}

func TestAuditMigrationFreshInstallation(t *testing.T) {
	if len(migrations) < 9 {
		t.Fatal("migration 009 is missing")
	}

	db := openInventoryTestDB(t)

	assertInventorySchemaVersion(t, db, len(migrations))

	inventoryAssertCount(t, db, 8, `
		SELECT COUNT(*)
		FROM pragma_table_info('audit_events')
	`)

	inventoryAssertCount(t, db, 2, `
		SELECT COUNT(*)
		FROM pragma_index_list('audit_events')
		WHERE name IN (
			'idx_audit_events_occurred_at',
			'idx_audit_events_target'
		)
	`)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM pragma_foreign_key_list('audit_events')
	`)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*) FROM audit_events
	`)
}

func TestAuditMigrationUpgrade(t *testing.T) {
	dir := t.TempDir()

	createAuditVersionEightDatabase(t, dir)

	db, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	assertInventorySchemaVersion(t, db, len(migrations))

	assertAuditMigrationPreservesRecords(t, db)

	// Upgrading must not fabricate historical events.
	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*) FROM audit_events
	`)
}

func assertAuditMigrationPreservesRecords(
	t *testing.T,
	db *sql.DB,
) {
	t.Helper()

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM administrators
		WHERE id = ?
			AND username = 'admin'
			AND password_scheme = 'argon2id-v1'
			AND password_salt = zeroblob(16)
			AND password_hash = zeroblob(32)
	`, adminMigrationTestID)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM administrator_sessions
		WHERE id = ?
			AND administrator_id = ?
			AND token_hash = ?
			AND password_changed_at = ?
			AND created_at = ?
			AND last_activity_at = ?
			AND expires_at = ?
	`,
		sessionMigrationTestID,
		adminMigrationTestID,
		bytes.Repeat([]byte{0x11}, 32),
		sessionMigrationCredentialRevision,
		sessionMigrationCreatedAt,
		sessionMigrationCreatedAt,
		sessionMigrationExpiresAt,
	)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM appliance_metadata
		WHERE key = 'installation_id'
			AND value = ?
	`, inventoryOtherDeviceID)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM devices
		WHERE id = ?
	`, inventoryTestDeviceID)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM device_performance
		WHERE device_id = ?
			AND sample_id = ?
			AND payload = X'0A020801'
	`,
		inventoryTestDeviceID,
		performanceTestSampleID,
	)
}

func TestAuditMigrationConstraints(t *testing.T) {
	db := openInventoryTestDB(t)

	seedSessionMigrationAdministrator(t, db)

	inventoryExec(
		t,
		db,
		auditMigrationInsertSQL,
		auditMigrationTestID,
		adminMigrationTestID,
	)

	tests := []struct {
		name   string
		column string
		value  any
	}{
		{"invalid event ID", "id", "invalid"},
		{"binary event ID", "id", []byte(auditMigrationTestID)},
		{"null event ID", "id", nil},
		{"zero timestamp", "occurred_at", 0},
		{"negative timestamp", "occurred_at", -1},
		{"noninteger timestamp", "occurred_at", 1.5},
		{"empty event type", "event_type", ""},
		{"invalid event type", "event_type", "admin created"},
		{"uppercase event type", "event_type", "ADMIN.CREATED"},
		{"embedded NUL", "event_type", "admin.created\x00"},
		{"invalid actor kind", "actor_kind", "unknown"},
		{"actor identity required", "actor_kind", "administrator"},
		{"local actor identity forbidden", "actor_id", adminMigrationTestID},
		{"invalid target kind", "target_kind", "unknown"},
		{"invalid target identity", "target_id", "invalid"},
		{"null target identity", "target_id", nil},
		{"unexpected target identity", "target_kind", "none"},
		{"invalid outcome", "outcome", "unknown"},
		{"null outcome", "outcome", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			query := `
				UPDATE audit_events
				SET ` + tc.column + ` = ?
				WHERE id = ?
			`

			inventoryAssertConstraint(
				t,
				db,
				query,
				tc.value,
				auditMigrationTestID,
			)
		})
	}

	inventoryAssertConstraint(
		t,
		db,
		auditMigrationInsertSQL,
		auditMigrationTestID,
		adminMigrationTestID,
	)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM audit_events
		WHERE id = ?
			AND event_type = 'admin.created'
			AND actor_kind = 'local_operator'
			AND actor_id IS NULL
			AND target_kind = 'administrator'
			AND target_id = ?
			AND outcome = 'success'
	`,
		auditMigrationTestID,
		adminMigrationTestID,
	)
}

func TestAuditMigrationPreservesHistoryAfterDeletion(
	t *testing.T,
) {
	db := openInventoryTestDB(t)

	seedSessionMigrationAdministrator(t, db)

	inventoryExec(
		t,
		db,
		auditMigrationInsertSQL,
		auditMigrationTestID,
		adminMigrationTestID,
	)

	inventoryExec(t, db, `
		DELETE FROM administrators
		WHERE id = ?
	`, adminMigrationTestID)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*) FROM administrators
	`)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM audit_events
		WHERE target_id = ?
	`, adminMigrationTestID)
}

func TestAuditMigrationRollback(t *testing.T) {
	dir := t.TempDir()

	createAuditVersionEightDatabase(t, dir)

	db, err := openRawExistingTestDatabase(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)

	inventoryExec(t, db, "PRAGMA foreign_keys = ON")

	// Force migration 009 to fail.
	inventoryExec(t, db, `
		CREATE INDEX idx_audit_events_occurred_at
		ON administrators(created_at)
	`)

	err = migrate(context.Background(), db)

	if err == nil {
		t.Fatal("expected audit migration failure")
	}

	assertInventorySchemaVersion(t, db, 8)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
			AND name = 'audit_events'
	`)

	assertAuditMigrationPreservesRecords(t, db)

	inventoryExec(t, db, `
		DROP INDEX idx_audit_events_occurred_at
	`)

	if err := migrate(
		context.Background(),
		db,
	); err != nil {
		t.Fatal(err)
	}

	assertInventorySchemaVersion(t, db, len(migrations))

	assertAuditMigrationPreservesRecords(t, db)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*) FROM audit_events
	`)
}
