package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

const (
	inventoryTestDeviceID  = "11111111-1111-4111-8111-111111111111"
	inventoryOtherDeviceID = "22222222-2222-4222-8222-222222222222"

	inventoryTestTimestamp int64 = 1700000000000
)

func openInventoryTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := Open(
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

func inventoryExec(
	t *testing.T,
	db *sql.DB,
	query string,
	args ...any,
) {
	t.Helper()

	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func inventoryAssertCount(
	t *testing.T,
	db *sql.DB,
	expected int,
	query string,
	args ...any,
) {
	t.Helper()

	var count int

	if err := db.QueryRow(
		query,
		args...,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != expected {
		t.Fatalf(
			"expected count %d, got %d",
			expected,
			count,
		)
	}
}

func inventoryAssertConstraint(
	t *testing.T,
	db *sql.DB,
	query string,
	args ...any,
) {
	t.Helper()

	_, err := db.Exec(query, args...)

	if err == nil {
		t.Fatal("expected database constraint violation")
	}

	if !strings.Contains(
		strings.ToLower(err.Error()),
		"constraint",
	) {
		t.Fatalf(
			"expected constraint violation, got %v",
			err,
		)
	}
}

func insertInventoryTestDevice(
	t *testing.T,
	db *sql.DB,
	deviceID string,
) {
	t.Helper()

	inventoryExec(t, db, `
		INSERT INTO devices (
			id,
			hostname,
			os,
			architecture,
			agent_version,
			created_at,
			updated_at,
			last_seen_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		deviceID,
		"test-device.local",
		"darwin",
		"arm64",
		"0.1.0-test",
		inventoryTestTimestamp,
		inventoryTestTimestamp,
		inventoryTestTimestamp,
	)
}

func insertInventoryTestSnapshot(
	t *testing.T,
	db *sql.DB,
	deviceID string,
) {
	t.Helper()

	inventoryExec(t, db, `
		INSERT INTO device_inventory (
			device_id,
			hostname,
			os,
			architecture,
			logical_cpus,
			host_logical_cpus,
			physical_cores,
			cpu_model,
			cpu_manufacturer,
			memory_bytes,
			hardware_model,
			os_version,
			kernel_version,
			boot_time,
			collected_at,
			received_at
		)
		VALUES (
			?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?
		)
	`,
		deviceID,
		"test-device.local",
		"darwin",
		"arm64",
		14,
		14,
		14,
		"Apple M4 Pro",
		"Apple",
		int64(25769803776),
		"Mac16,8",
		"26.6.2",
		"25.6.0",
		inventoryTestTimestamp-86400000,
		inventoryTestTimestamp,
		inventoryTestTimestamp+1000,
	)
}

func assertInventorySchemaVersion(
	t *testing.T,
	db *sql.DB,
	expected int,
) {
	t.Helper()

	var version int

	if err := db.QueryRow(
		"PRAGMA user_version",
	).Scan(&version); err != nil {
		t.Fatal(err)
	}

	if version != expected {
		t.Fatalf(
			"expected schema version %d, got %d",
			expected,
			version,
		)
	}
}

func TestInventoryMigrationFreshInstallation(t *testing.T) {
	db := openInventoryTestDB(t)

	assertInventorySchemaVersion(t, db, 5)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
			AND name = 'device_inventory'
	`)

	inventoryAssertCount(t, db, 16, `
		SELECT COUNT(*)
		FROM pragma_table_info('device_inventory')
	`)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM pragma_table_info('device_inventory')
		WHERE name = 'device_id'
			AND pk = 1
	`)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM pragma_foreign_key_list('device_inventory')
		WHERE "table" = 'devices'
			AND "from" = 'device_id'
			AND "to" = 'id'
			AND on_delete = 'CASCADE'
	`)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM device_inventory
	`)
}

func createInventoryVersionFourDatabase(
	t *testing.T,
	dir string,
) {
	t.Helper()

	if len(migrations) < 5 {
		t.Fatal("migration 005 is missing")
	}

	db, err := sql.Open(
		"sqlite",
		filepath.Join(dir, Filename),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)

	inventoryExec(t, db, "PRAGMA foreign_keys = ON")

	// Apply only the original four migrations.
	for _, migration := range migrations[:4] {
		inventoryExec(t, db, migration)
	}

	inventoryExec(t, db, "PRAGMA user_version = 4")

	assertInventorySchemaVersion(t, db, 4)

	insertInventoryTestDevice(
		t, db, inventoryTestDeviceID,
	)

	inventoryExec(t, db, `
		INSERT INTO appliance_metadata (key, value)
		VALUES ('installation_id', ?)
	`, inventoryOtherDeviceID)

	inventoryExec(t, db, `
		INSERT INTO agent_credentials (
			device_id,
			token_hash,
			created_at
		)
		VALUES (?, zeroblob(32), ?)
	`,
		inventoryTestDeviceID,
		inventoryTestTimestamp,
	)

	inventoryExec(t, db, `
		INSERT INTO enrollment_tokens (
			id,
			token_hash,
			created_at,
			expires_at
		)
		VALUES (?, zeroblob(32), ?, ?)
	`,
		"0123456789abcdef0123456789abcdef",
		inventoryTestTimestamp,
		inventoryTestTimestamp+900000,
	)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE name = 'device_inventory'
	`)
}

func TestInventoryMigrationUpgradeFromVersionFour(
	t *testing.T,
) {
	dir := t.TempDir()

	createInventoryVersionFourDatabase(t, dir)

	db, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	assertInventorySchemaVersion(t, db, 5)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM devices
		WHERE id = ?
			AND hostname = 'test-device.local'
			AND os = 'darwin'
			AND architecture = 'arm64'
			AND agent_version = '0.1.0-test'
			AND created_at = ?
			AND updated_at = ?
			AND last_seen_at = ?
	`,
		inventoryTestDeviceID,
		inventoryTestTimestamp,
		inventoryTestTimestamp,
		inventoryTestTimestamp,
	)

	inventoryAssertCount(t, db, 1, `
			SELECT COUNT(*)
			FROM appliance_metadata
			WHERE key = 'installation_id'
				AND value = ?
		`, inventoryOtherDeviceID)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM agent_credentials
		WHERE device_id = ?
			AND token_hash = zeroblob(32)
			AND created_at = ?
			AND revoked_at IS NULL
	`,
		inventoryTestDeviceID,
		inventoryTestTimestamp,
	)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM enrollment_tokens
		WHERE id = ?
			AND token_hash = zeroblob(32)
			AND created_at = ?
			AND expires_at = ?
			AND used_at IS NULL
			AND revoked_at IS NULL
	`,
		"0123456789abcdef0123456789abcdef",
		inventoryTestTimestamp,
		inventoryTestTimestamp+900000,
	)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*) FROM device_inventory
	`)

	// Existing devices can receive inventory after upgrading.
	insertInventoryTestSnapshot(
		t, db, inventoryTestDeviceID,
	)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM device_inventory
		WHERE device_id = ?
	`, inventoryTestDeviceID)
}

func TestInventoryMigrationPrimaryKey(t *testing.T) {
	db := openInventoryTestDB(t)

	insertInventoryTestDevice(
		t, db, inventoryTestDeviceID,
	)

	insertInventoryTestSnapshot(
		t, db, inventoryTestDeviceID,
	)

	inventoryAssertConstraint(t, db, `
		INSERT INTO device_inventory
		SELECT *
		FROM device_inventory
		WHERE device_id = ?
	`, inventoryTestDeviceID)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*) FROM device_inventory
	`)
}

func TestInventoryMigrationForeignKey(t *testing.T) {
	db := openInventoryTestDB(t)

	inventoryAssertConstraint(t, db, `
		INSERT INTO device_inventory (
			device_id,
			hostname,
			os,
			architecture,
			logical_cpus,
			collected_at,
			received_at
		)
		VALUES (?, 'test-device', 'linux', 'amd64', 4, ?, ?)
	`,
		inventoryTestDeviceID,
		inventoryTestTimestamp,
		inventoryTestTimestamp,
	)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*) FROM device_inventory
	`)
}

func TestInventoryMigrationCascadeDelete(t *testing.T) {
	db := openInventoryTestDB(t)

	insertInventoryTestDevice(
		t, db, inventoryTestDeviceID,
	)

	insertInventoryTestDevice(
		t, db, inventoryOtherDeviceID,
	)

	insertInventoryTestSnapshot(
		t, db, inventoryTestDeviceID,
	)

	insertInventoryTestSnapshot(
		t, db, inventoryOtherDeviceID,
	)

	inventoryExec(t, db, `
		DELETE FROM devices
		WHERE id = ?
	`, inventoryTestDeviceID)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM device_inventory
		WHERE device_id = ?
	`, inventoryTestDeviceID)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM device_inventory
		WHERE device_id = ?
	`, inventoryOtherDeviceID)
}

func TestInventoryMigrationConstraints(t *testing.T) {
	db := openInventoryTestDB(t)

	insertInventoryTestDevice(
		t, db, inventoryTestDeviceID,
	)

	insertInventoryTestSnapshot(
		t, db, inventoryTestDeviceID,
	)

	tests := []struct {
		name   string
		column string
		value  any
	}{
		{"empty hostname", "hostname", ""},
		{"null hostname", "hostname", nil},
		{"long hostname", "hostname", strings.Repeat("a", 256)},
		{"empty OS", "os", ""},
		{"long OS", "os", strings.Repeat("a", 65)},
		{"empty architecture", "architecture", ""},
		{"long architecture", "architecture", strings.Repeat("a", 65)},
		{"zero logical CPUs", "logical_cpus", 0},
		{"negative logical CPUs", "logical_cpus", -1},
		{"excessive logical CPUs", "logical_cpus", 1048577},
		{"negative host CPUs", "host_logical_cpus", -1},
		{"excessive host CPUs", "host_logical_cpus", 1048577},
		{"negative physical cores", "physical_cores", -1},
		{"excessive physical cores", "physical_cores", 1048577},
		{"invalid CPU topology", "physical_cores", 15},
		{"long CPU model", "cpu_model", strings.Repeat("a", 257)},
		{"long manufacturer", "cpu_manufacturer", strings.Repeat("a", 257)},
		{"negative memory", "memory_bytes", -1},
		{"long hardware model", "hardware_model", strings.Repeat("a", 257)},
		{"long OS version", "os_version", strings.Repeat("a", 129)},
		{"long kernel version", "kernel_version", strings.Repeat("a", 129)},
		{"negative boot time", "boot_time", -1},
		{"zero collection time", "collected_at", 0},
		{"negative collection time", "collected_at", -1},
		{"zero received time", "received_at", 0},
		{"negative received time", "received_at", -1},
		{
			"boot after collection",
			"boot_time",
			inventoryTestTimestamp + 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Column names are fixed test constants,
			// never external input.
			query := `
				UPDATE device_inventory
				SET ` + tt.column + ` = ?
				WHERE device_id = ?
			`

			inventoryAssertConstraint(
				t,
				db,
				query,
				tt.value,
				inventoryTestDeviceID,
			)
		})
	}

	// Failed updates must leave the original record intact.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM device_inventory
		WHERE device_id = ?
			AND hostname = 'test-device.local'
			AND logical_cpus = 14
			AND physical_cores = 14
			AND memory_bytes = 25769803776
			AND collected_at = ?
	`,
		inventoryTestDeviceID,
		inventoryTestTimestamp,
	)
}

func TestInventoryMigrationOptionalFields(t *testing.T) {
	db := openInventoryTestDB(t)

	insertInventoryTestDevice(
		t, db, inventoryTestDeviceID,
	)

	inventoryExec(t, db, `
		INSERT INTO device_inventory (
			device_id,
			hostname,
			os,
			architecture,
			logical_cpus,
			collected_at,
			received_at
		)
		VALUES (?, 'test-server', 'linux', 'amd64', 4, ?, ?)
	`,
		inventoryTestDeviceID,
		inventoryTestTimestamp,
		inventoryTestTimestamp,
	)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM device_inventory
		WHERE device_id = ?
			AND host_logical_cpus = 0
			AND physical_cores = 0
			AND cpu_model = ''
			AND cpu_manufacturer = ''
			AND memory_bytes = 0
			AND hardware_model = ''
			AND os_version = ''
			AND kernel_version = ''
			AND boot_time = 0
	`, inventoryTestDeviceID)
}

func TestInventoryMigrationDatabaseReopen(t *testing.T) {
	dir := t.TempDir()

	db, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	insertInventoryTestDevice(
		t, db, inventoryTestDeviceID,
	)

	insertInventoryTestSnapshot(
		t, db, inventoryTestDeviceID,
	)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	assertInventorySchemaVersion(t, reopened, 5)

	inventoryAssertCount(t, reopened, 1, `
		SELECT COUNT(*)
		FROM device_inventory
		WHERE device_id = ?
			AND cpu_model = 'Apple M4 Pro'
			AND logical_cpus = 14
			AND memory_bytes = 25769803776
	`, inventoryTestDeviceID)
}
