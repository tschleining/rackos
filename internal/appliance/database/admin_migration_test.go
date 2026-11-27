package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

const adminMigrationTestID = "11111111-1111-4111-8111-111111111111"

const adminMigrationInsertSQL = `
	INSERT INTO administrators (
		id,
		username,
		password_scheme,
		password_salt,
		password_hash,
		role,
		created_at,
		updated_at,
		password_changed_at
	)
	VALUES (
		?, ?,
		'argon2id-v1',
		zeroblob(16),
		zeroblob(32),
		'administrator',
		1800000000000,
		1800000000000,
		1800000000000
	)
`

func TestAdministratorMigrationFreshInstallation(
	t *testing.T,
) {
	db := openInventoryTestDB(t)

	if len(migrations) < 7 {
		t.Fatal("migration 007 is missing")
	}

	assertInventorySchemaVersion(
		t,
		db,
		len(migrations),
	)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
			AND name = 'administrators'
	`)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM pragma_index_list('administrators')
		WHERE name = 'idx_administrators_singleton'
			AND "unique" = 1
	`)

	inventoryAssertCount(t, db, 11, `
		SELECT COUNT(*)
		FROM pragma_table_info('administrators')
	`)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*) FROM administrators
	`)
}

func createAdministratorVersionSixDatabase(
	t *testing.T,
	dir string,
) {
	t.Helper()

	createPerformanceVersionFiveDatabase(t, dir)

	db, err := sql.Open(
		"sqlite",
		filepath.Join(dir, Filename),
	)
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

	inventoryExec(t, db, migrations[5])

	inventoryExec(
		t,
		db,
		"PRAGMA user_version = 6",
	)

	insertPerformanceTestSample(
		t,
		db,
		inventoryTestDeviceID,
		performanceTestSampleID,
		performanceTestTimestamp,
	)

	assertInventorySchemaVersion(t, db, 6)
}

func TestAdministratorMigrationUpgrade(
	t *testing.T,
) {
	dir := t.TempDir()

	createAdministratorVersionSixDatabase(
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

	// Existing appliance identity survives.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM appliance_metadata
		WHERE key = 'installation_id'
			AND value = ?
	`, inventoryOtherDeviceID)

	// Existing device registration survives.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM devices
		WHERE id = ?
			AND hostname = 'test-device.local'
	`, inventoryTestDeviceID)

	// Existing agent credential survives.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM agent_credentials
		WHERE device_id = ?
			AND token_hash = zeroblob(32)
			AND revoked_at IS NULL
	`, inventoryTestDeviceID)

	// Existing enrollment invitation survives.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM enrollment_tokens
		WHERE id = ?
			AND used_at IS NULL
			AND revoked_at IS NULL
	`, "0123456789abcdef0123456789abcdef")

	// Existing hardware inventory survives.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM device_inventory
		WHERE device_id = ?
			AND logical_cpus = 14
			AND memory_bytes = 25769803776
	`, inventoryTestDeviceID)

	// Existing historical performance survives.
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

	// Upgrades do not create default accounts.
	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*) FROM administrators
	`)
}

func TestAdministratorMigrationConstraints(
	t *testing.T,
) {
	db := openInventoryTestDB(t)

	inventoryExec(
		t,
		db,
		adminMigrationInsertSQL,
		adminMigrationTestID,
		"admin",
	)

	// A different username and identity must still
	// be rejected by the singleton index.
	inventoryAssertConstraint(
		t,
		db,
		adminMigrationInsertSQL,
		"22222222-2222-4222-8222-222222222222",
		"another-admin",
	)

	tests := []struct {
		name   string
		column string
		value  any
	}{
		{"invalid ID", "id", "invalid"},
		{"uppercase username", "username", "ADMIN"},
		{"empty username", "username", ""},
		{"short username", "username", "ab"},
		{"username whitespace", "username", "admin user"},
		{"invalid username character", "username", "admin@"},
		{"embedded NUL", "username", "admin\x00"},
		{"unsupported scheme", "password_scheme", "plaintext"},
		{"empty salt", "password_salt", []byte{}},
		{"short salt", "password_salt", make([]byte, 15)},
		{"long salt", "password_salt", make([]byte, 17)},
		{"text salt", "password_salt", "1234567890123456"},
		{"null salt", "password_salt", nil},
		{"short hash", "password_hash", make([]byte, 31)},
		{"long hash", "password_hash", make([]byte, 33)},
		{"null hash", "password_hash", nil},
		{"invalid role", "role", "unknown"},
		{"zero created timestamp", "created_at", 0},
		{"negative update timestamp", "updated_at", -1},
		{"invalid password timestamp", "password_changed_at", 0},
		{"invalid disabled timestamp", "disabled_at", -1},
		{"invalid login timestamp", "last_login_at", -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Column names are fixed test constants.
			query := `
				UPDATE administrators
				SET ` + tc.column + ` = ?
				WHERE id = ?
			`

			inventoryAssertConstraint(
				t,
				db,
				query,
				tc.value,
				adminMigrationTestID,
			)
		})
	}

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM administrators
		WHERE id = ?
			AND username = 'admin'
			AND password_scheme = 'argon2id-v1'
			AND password_salt = zeroblob(16)
			AND password_hash = zeroblob(32)
			AND role = 'administrator'
			AND disabled_at IS NULL
			AND last_login_at IS NULL
	`, adminMigrationTestID)
}

func TestAdministratorMigrationRollback(
	t *testing.T,
) {
	dir := t.TempDir()

	createAdministratorVersionSixDatabase(
		t,
		dir,
	)

	db, err := sql.Open(
		"sqlite",
		filepath.Join(dir, Filename),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)

	// Introduce a deliberate index naming conflict
	// to force migration 007 to fail.
	inventoryExec(t, db, `
		CREATE INDEX idx_administrators_singleton
		ON devices(created_at)
	`)

	err = migrate(context.Background(), db)

	if err == nil {
		t.Fatal("expected migration failure")
	}

	// A failed migration must not advance the schema
	// version or leave the administrators table behind.
	assertInventorySchemaVersion(t, db, 6)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
			AND name = 'administrators'
	`)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM device_performance
		WHERE device_id = ?
	`, inventoryTestDeviceID)

	// Remove the conflict and verify migration recovery.
	inventoryExec(t, db, `
		DROP INDEX idx_administrators_singleton
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
		SELECT COUNT(*) FROM administrators
	`)
}
