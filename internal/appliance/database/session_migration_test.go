package database

import (
	"bytes"
	"context"
	"database/sql"
	"testing"
)

const (
	sessionMigrationTestID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	sessionMigrationOtherID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	sessionMigrationCredentialRevision int64 = 1800000000000

	sessionMigrationCreatedAt int64 = sessionMigrationCredentialRevision + 1000

	sessionMigrationExpiresAt int64 = sessionMigrationCreatedAt + 43200000
)

const sessionMigrationInsertSQL = `
	INSERT INTO administrator_sessions (
		id,
		administrator_id,
		token_hash,
		password_changed_at,
		created_at,
		last_activity_at,
		expires_at
	)
	VALUES (?, ?, ?, ?, ?, ?, ?)
`

func seedSessionMigrationAdministrator(
	t *testing.T,
	db *sql.DB,
) {
	t.Helper()

	inventoryExec(
		t,
		db,
		adminMigrationInsertSQL,
		adminMigrationTestID,
		"admin",
	)
}

func insertSessionMigrationRecord(
	t *testing.T,
	db *sql.DB,
	id string,
	tokenHash []byte,
) {
	t.Helper()

	inventoryExec(
		t,
		db,
		sessionMigrationInsertSQL,
		id,
		adminMigrationTestID,
		tokenHash,
		sessionMigrationCredentialRevision,
		sessionMigrationCreatedAt,
		sessionMigrationCreatedAt,
		sessionMigrationExpiresAt,
	)
}

func createSessionVersionSevenDatabase(
	t *testing.T,
	dir string,
) {
	t.Helper()

	createAdministratorVersionSixDatabase(
		t,
		dir,
	)

	db, err := openRawExistingTestDatabase(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)

	inventoryExec(
		t,
		db,
		"PRAGMA foreign_keys = ON",
	)

	// Apply only Migration 007.
	inventoryExec(t, db, migrations[6])

	inventoryExec(
		t,
		db,
		"PRAGMA user_version = 7",
	)

	seedSessionMigrationAdministrator(t, db)

	assertInventorySchemaVersion(t, db, 7)
}

func TestAdministratorSessionMigrationFreshInstallation(
	t *testing.T,
) {
	if len(migrations) < 8 {
		t.Fatal("migration 008 is missing")
	}

	db := openInventoryTestDB(t)

	assertInventorySchemaVersion(
		t,
		db,
		len(migrations),
	)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
			AND name = 'administrator_sessions'
	`)

	inventoryAssertCount(t, db, 7, `
		SELECT COUNT(*)
		FROM pragma_table_info(
			'administrator_sessions'
		)
	`)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM pragma_table_info(
			'administrator_sessions'
		)
		WHERE name = 'id'
			AND pk = 1
	`)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM pragma_foreign_key_list(
			'administrator_sessions'
		)
		WHERE "table" = 'administrators'
			AND "from" = 'administrator_id'
			AND "to" = 'id'
			AND on_delete = 'CASCADE'
	`)

	inventoryAssertCount(t, db, 3, `
		SELECT COUNT(*)
		FROM pragma_index_list(
			'administrator_sessions'
		)
		WHERE name IN (
			'idx_administrator_sessions_administrator',
			'idx_administrator_sessions_expires_at',
			'idx_administrator_sessions_last_activity_at'
		)
	`)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM administrator_sessions
	`)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM administrators
	`)
}

func TestAdministratorSessionMigrationUpgrade(
	t *testing.T,
) {
	dir := t.TempDir()

	createSessionVersionSevenDatabase(
		t,
		dir,
	)

	db, err := Open(
		context.Background(),
		dir,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	assertInventorySchemaVersion(
		t,
		db,
		len(migrations),
	)

	// Preserve the existing administrator and
	// all password verification material.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM administrators
		WHERE id = ?
			AND username = 'admin'
			AND role = 'administrator'
			AND password_scheme = 'argon2id-v1'
			AND password_salt = zeroblob(16)
			AND password_hash = zeroblob(32)
			AND password_changed_at = ?
			AND disabled_at IS NULL
			AND last_login_at IS NULL
	`,
		adminMigrationTestID,
		sessionMigrationCredentialRevision,
	)

	// Preserve the appliance identity.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM appliance_metadata
		WHERE key = 'installation_id'
			AND value = ?
	`, inventoryOtherDeviceID)

	// Preserve the registered device.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM devices
		WHERE id = ?
			AND hostname = 'test-device.local'
	`, inventoryTestDeviceID)

	// Preserve agent credentials.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM agent_credentials
		WHERE device_id = ?
			AND token_hash = zeroblob(32)
			AND revoked_at IS NULL
	`, inventoryTestDeviceID)

	// Preserve enrollment invitations.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM enrollment_tokens
		WHERE id = ?
			AND used_at IS NULL
			AND revoked_at IS NULL
	`, "0123456789abcdef0123456789abcdef")

	// Preserve hardware inventory.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM device_inventory
		WHERE device_id = ?
			AND logical_cpus = 14
			AND memory_bytes = 25769803776
	`, inventoryTestDeviceID)

	// Preserve historical performance.
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

	// Upgrading must not authenticate anyone
	// or create default sessions.
	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM administrator_sessions
	`)
}

func TestAdministratorSessionMigrationConstraints(
	t *testing.T,
) {
	db := openInventoryTestDB(t)

	seedSessionMigrationAdministrator(t, db)

	tokenHash := bytes.Repeat(
		[]byte{0x11},
		32,
	)

	insertSessionMigrationRecord(
		t,
		db,
		sessionMigrationTestID,
		tokenHash,
	)

	tests := []struct {
		name   string
		column string
		value  any
	}{
		{
			"invalid session ID",
			"id",
			"invalid",
		},
		{
			"uppercase session ID",
			"id",
			"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		},
		{
			"nonhexadecimal session ID",
			"id",
			"gggggggggggggggggggggggggggggggg",
		},
		{
			"session ID containing NUL",
			"id",
			sessionMigrationTestID + "\x00",
		},
		{
			"binary session ID",
			"id",
			[]byte(sessionMigrationTestID),
		},
		{
			"null session ID",
			"id",
			nil,
		},
		{
			"unknown administrator",
			"administrator_id",
			"22222222-2222-4222-8222-222222222222",
		},
		{
			"null administrator",
			"administrator_id",
			nil,
		},
		{
			"short token digest",
			"token_hash",
			make([]byte, 31),
		},
		{
			"long token digest",
			"token_hash",
			make([]byte, 33),
		},
		{
			"text token digest",
			"token_hash",
			sessionMigrationTestID,
		},
		{
			"null token digest",
			"token_hash",
			nil,
		},
		{
			"invalid credential revision",
			"password_changed_at",
			0,
		},
		{
			"future credential revision",
			"password_changed_at",
			sessionMigrationCreatedAt + 1,
		},
		{
			"noninteger credential revision",
			"password_changed_at",
			1.5,
		},
		{
			"zero creation timestamp",
			"created_at",
			0,
		},
		{
			"negative creation timestamp",
			"created_at",
			-1,
		},
		{
			"noninteger creation timestamp",
			"created_at",
			1.5,
		},
		{
			"activity before creation",
			"last_activity_at",
			sessionMigrationCreatedAt - 1,
		},
		{
			"activity at expiration",
			"last_activity_at",
			sessionMigrationExpiresAt,
		},
		{
			"activity after expiration",
			"last_activity_at",
			sessionMigrationExpiresAt + 1,
		},
		{
			"expiration before creation",
			"expires_at",
			sessionMigrationCreatedAt - 1,
		},
		{
			"expiration at creation",
			"expires_at",
			sessionMigrationCreatedAt,
		},
		{
			"expiration beyond maximum lifetime",
			"expires_at",
			sessionMigrationExpiresAt + 1,
		},
		{
			"null expiration",
			"expires_at",
			nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Column names originate exclusively
			// from the fixed test cases above.
			query := `
				UPDATE administrator_sessions
				SET ` + tc.column + ` = ?
				WHERE id = ?
			`

			inventoryAssertConstraint(
				t,
				db,
				query,
				tc.value,
				sessionMigrationTestID,
			)
		})
	}

	// Rejected writes must preserve the session.
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
		tokenHash,
		sessionMigrationCredentialRevision,
		sessionMigrationCreatedAt,
		sessionMigrationCreatedAt,
		sessionMigrationExpiresAt,
	)
}

func TestAdministratorSessionMigrationUniqueness(
	t *testing.T,
) {
	db := openInventoryTestDB(t)

	seedSessionMigrationAdministrator(t, db)

	firstHash := bytes.Repeat(
		[]byte{0x11},
		32,
	)

	secondHash := bytes.Repeat(
		[]byte{0x22},
		32,
	)

	insertSessionMigrationRecord(
		t,
		db,
		sessionMigrationTestID,
		firstHash,
	)

	// Duplicate session ID with a different digest.
	inventoryAssertConstraint(
		t,
		db,
		sessionMigrationInsertSQL,
		sessionMigrationTestID,
		adminMigrationTestID,
		secondHash,
		sessionMigrationCredentialRevision,
		sessionMigrationCreatedAt,
		sessionMigrationCreatedAt,
		sessionMigrationExpiresAt,
	)

	// Duplicate digest with a different session ID.
	inventoryAssertConstraint(
		t,
		db,
		sessionMigrationInsertSQL,
		sessionMigrationOtherID,
		adminMigrationTestID,
		firstHash,
		sessionMigrationCredentialRevision,
		sessionMigrationCreatedAt,
		sessionMigrationCreatedAt,
		sessionMigrationExpiresAt,
	)

	// One administrator may have multiple sessions.
	insertSessionMigrationRecord(
		t,
		db,
		sessionMigrationOtherID,
		secondHash,
	)

	inventoryAssertCount(t, db, 2, `
		SELECT COUNT(*)
		FROM administrator_sessions
		WHERE administrator_id = ?
	`, adminMigrationTestID)
}

func TestAdministratorSessionMigrationCascade(
	t *testing.T,
) {
	db := openInventoryTestDB(t)

	seedSessionMigrationAdministrator(t, db)

	insertSessionMigrationRecord(
		t,
		db,
		sessionMigrationTestID,
		bytes.Repeat([]byte{0x11}, 32),
	)

	insertSessionMigrationRecord(
		t,
		db,
		sessionMigrationOtherID,
		bytes.Repeat([]byte{0x22}, 32),
	)

	inventoryAssertCount(t, db, 2, `
		SELECT COUNT(*)
		FROM administrator_sessions
	`)

	inventoryExec(t, db, `
		DELETE FROM administrators
		WHERE id = ?
	`, adminMigrationTestID)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM administrator_sessions
	`)
}

func TestAdministratorSessionMigrationPersistence(
	t *testing.T,
) {
	ctx := context.Background()
	dir := t.TempDir()

	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	tokenHash := bytes.Repeat(
		[]byte{0x11},
		32,
	)

	seedSessionMigrationAdministrator(t, db)

	insertSessionMigrationRecord(
		t,
		db,
		sessionMigrationTestID,
		tokenHash,
	)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

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
		tokenHash,
		sessionMigrationCredentialRevision,
		sessionMigrationCreatedAt,
		sessionMigrationCreatedAt,
		sessionMigrationExpiresAt,
	)
}

func TestAdministratorSessionMigrationRollback(
	t *testing.T,
) {
	dir := t.TempDir()

	createSessionVersionSevenDatabase(
		t,
		dir,
	)

	db, err := openRawExistingTestDatabase(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)

	inventoryExec(
		t,
		db,
		"PRAGMA foreign_keys = ON",
	)

	// Force Migration 008 to fail after the
	// session table has been created.
	inventoryExec(t, db, `
		CREATE INDEX idx_administrator_sessions_expires_at
		ON administrators(created_at)
	`)

	err = migrate(
		context.Background(),
		db,
	)

	if err == nil {
		t.Fatal("expected migration failure")
	}

	assertInventorySchemaVersion(t, db, 7)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
			AND name = 'administrator_sessions'
	`)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'index'
			AND name = 'idx_administrator_sessions_administrator'
	`)

	// Existing account survives the failed upgrade.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM administrators
		WHERE id = ?
			AND username = 'admin'
	`, adminMigrationTestID)

	// Remove the conflict and retry.
	inventoryExec(t, db, `
		DROP INDEX idx_administrator_sessions_expires_at
	`)

	if err := migrate(
		context.Background(),
		db,
	); err != nil {
		t.Fatal(err)
	}

	assertInventorySchemaVersion(
		t,
		db,
		len(migrations),
	)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM administrator_sessions
	`)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM administrators
		WHERE id = ?
	`, adminMigrationTestID)
}
