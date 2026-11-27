package database

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

const (
	performanceTestSampleID  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	performanceOtherSampleID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	performanceTestTimestamp int64 = 1700000000000
)

const insertPerformanceTestSQL = `
	INSERT INTO device_performance (
		device_id,
		sample_id,
		collected_at,
		received_at,
		payload_version,
		payload
	)
	VALUES (?, ?, ?, ?, ?, ?)
`

// insertPerformanceTestSample inserts an opaque binary
// payload for database schema testing.
//
// Application-level Protobuf validation is tested
// separately in the appliance performance package.
func insertPerformanceTestSample(
	t *testing.T,
	db *sql.DB,
	deviceID string,
	sampleID string,
	collectedAt int64,
) {
	t.Helper()

	inventoryExec(
		t,
		db,
		insertPerformanceTestSQL,
		deviceID,
		sampleID,
		collectedAt,
		performanceTestTimestamp+1000,
		1,
		[]byte{0x0a, 0x02, 0x08, 0x01},
	)
}

func TestPerformanceMigrationFreshInstallation(t *testing.T) {
	db := openInventoryTestDB(t)

	if len(migrations) < 6 {
		t.Fatal("migration 006 is missing")
	}

	assertInventorySchemaVersion(t, db, len(migrations))

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
			AND name = 'device_performance'
	`)

	inventoryAssertCount(t, db, 6, `
		SELECT COUNT(*)
		FROM pragma_table_info('device_performance')
	`)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM pragma_table_info('device_performance')
		WHERE name = 'device_id'
			AND pk = 1
	`)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM pragma_table_info('device_performance')
		WHERE name = 'sample_id'
			AND pk = 2
	`)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM pragma_table_info('device_performance')
		WHERE name = 'payload'
			AND type = 'BLOB'
			AND "notnull" = 1
	`)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM pragma_foreign_key_list('device_performance')
		WHERE "table" = 'devices'
			AND "from" = 'device_id'
			AND "to" = 'id'
			AND on_delete = 'CASCADE'
	`)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*) FROM device_performance
	`)
}

func TestPerformanceMigrationIndexes(t *testing.T) {
	db := openInventoryTestDB(t)

	inventoryAssertCount(t, db, 2, `
		SELECT COUNT(*)
		FROM pragma_index_list('device_performance')
		WHERE name IN (
			'idx_device_performance_history',
			'idx_device_performance_received_at'
		)
	`)

	inventoryAssertCount(t, db, 3, `
		SELECT COUNT(*)
		FROM pragma_index_info(
			'idx_device_performance_history'
		)
		WHERE
			(seqno = 0 AND name = 'device_id')
			OR
			(seqno = 1 AND name = 'collected_at')
			OR
			(seqno = 2 AND name = 'sample_id')
	`)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM pragma_index_info(
			'idx_device_performance_received_at'
		)
		WHERE seqno = 0
			AND name = 'received_at'
	`)
}

// createPerformanceVersionFiveDatabase constructs a
// database containing only migrations 001 through 005.
func createPerformanceVersionFiveDatabase(
	t *testing.T,
	dir string,
) {
	t.Helper()

	createInventoryVersionFourDatabase(t, dir)

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

	inventoryExec(t, db, migrations[4])
	inventoryExec(t, db, "PRAGMA user_version = 5")

	assertInventorySchemaVersion(t, db, 5)

	insertInventoryTestSnapshot(
		t,
		db,
		inventoryTestDeviceID,
	)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE name = 'device_performance'
	`)
}

func TestPerformanceMigrationUpgradeFromVersionFive(
	t *testing.T,
) {
	dir := t.TempDir()

	createPerformanceVersionFiveDatabase(t, dir)

	db, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	assertInventorySchemaVersion(t, db, len(migrations))

	// Existing appliance identity must survive.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM appliance_metadata
		WHERE key = 'installation_id'
			AND value = ?
	`, inventoryOtherDeviceID)

	// Existing device identity must survive.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM devices
		WHERE id = ?
			AND hostname = 'test-device.local'
			AND os = 'darwin'
			AND architecture = 'arm64'
	`, inventoryTestDeviceID)

	// Existing credentials must survive.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM agent_credentials
		WHERE device_id = ?
			AND token_hash = zeroblob(32)
			AND revoked_at IS NULL
	`, inventoryTestDeviceID)

	// Existing enrollment tokens must survive.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM enrollment_tokens
		WHERE id = ?
			AND token_hash = zeroblob(32)
			AND used_at IS NULL
			AND revoked_at IS NULL
	`, "0123456789abcdef0123456789abcdef")

	// Existing hardware inventory must survive.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM device_inventory
		WHERE device_id = ?
			AND logical_cpus = 14
			AND memory_bytes = 25769803776
	`, inventoryTestDeviceID)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*) FROM device_performance
	`)

	// Existing devices can receive performance records
	// immediately after upgrading.
	insertPerformanceTestSample(
		t,
		db,
		inventoryTestDeviceID,
		performanceTestSampleID,
		performanceTestTimestamp,
	)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*) FROM device_performance
		WHERE device_id = ?
	`, inventoryTestDeviceID)
}

func TestPerformanceMigrationCompositePrimaryKey(t *testing.T) {
	db := openInventoryTestDB(t)

	insertInventoryTestDevice(
		t, db, inventoryTestDeviceID,
	)

	insertInventoryTestDevice(
		t, db, inventoryOtherDeviceID,
	)

	// First sample for the first device.
	insertPerformanceTestSample(
		t,
		db,
		inventoryTestDeviceID,
		performanceTestSampleID,
		performanceTestTimestamp,
	)

	// A different sample with the same collection time
	// must be allowed.
	insertPerformanceTestSample(
		t,
		db,
		inventoryTestDeviceID,
		performanceOtherSampleID,
		performanceTestTimestamp,
	)

	// A different device may reuse the same sample ID.
	insertPerformanceTestSample(
		t,
		db,
		inventoryOtherDeviceID,
		performanceTestSampleID,
		performanceTestTimestamp,
	)

	// The same device and sample ID cannot be inserted
	// twice, even when the collection time changes.
	inventoryAssertConstraint(
		t,
		db,
		insertPerformanceTestSQL,
		inventoryTestDeviceID,
		performanceTestSampleID,
		performanceTestTimestamp+5000,
		performanceTestTimestamp+6000,
		1,
		[]byte{0x01},
	)

	inventoryAssertCount(t, db, 3, `
		SELECT COUNT(*) FROM device_performance
	`)

	inventoryAssertCount(t, db, 2, `
		SELECT COUNT(*) FROM device_performance
		WHERE device_id = ?
	`, inventoryTestDeviceID)
}

func TestPerformanceMigrationHistoricalOrdering(t *testing.T) {
	db := openInventoryTestDB(t)

	insertInventoryTestDevice(
		t, db, inventoryTestDeviceID,
	)

	// Insert the newer sample first.
	insertPerformanceTestSample(
		t,
		db,
		inventoryTestDeviceID,
		performanceTestSampleID,
		performanceTestTimestamp,
	)

	// Older historical samples remain valid.
	insertPerformanceTestSample(
		t,
		db,
		inventoryTestDeviceID,
		performanceOtherSampleID,
		performanceTestTimestamp-60000,
	)

	rows, err := db.Query(`
		SELECT sample_id
		FROM device_performance
		WHERE device_id = ?
		ORDER BY collected_at ASC, sample_id ASC
	`, inventoryTestDeviceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var actual []string

	for rows.Next() {
		var sampleID string

		if err := rows.Scan(&sampleID); err != nil {
			t.Fatal(err)
		}

		actual = append(actual, sampleID)
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if len(actual) != 2 {
		t.Fatal("incorrect historical sample count")
	}

	if actual[0] != performanceOtherSampleID ||
		actual[1] != performanceTestSampleID {
		t.Fatal("incorrect historical ordering")
	}
}

func TestPerformanceMigrationForeignKey(t *testing.T) {
	db := openInventoryTestDB(t)

	inventoryAssertConstraint(
		t,
		db,
		insertPerformanceTestSQL,
		inventoryTestDeviceID,
		performanceTestSampleID,
		performanceTestTimestamp,
		performanceTestTimestamp,
		1,
		[]byte{0x01},
	)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*) FROM device_performance
	`)
}

func TestPerformanceMigrationCascadeDelete(t *testing.T) {
	db := openInventoryTestDB(t)

	insertInventoryTestDevice(
		t, db, inventoryTestDeviceID,
	)

	insertInventoryTestDevice(
		t, db, inventoryOtherDeviceID,
	)

	insertPerformanceTestSample(
		t,
		db,
		inventoryTestDeviceID,
		performanceTestSampleID,
		performanceTestTimestamp,
	)

	insertPerformanceTestSample(
		t,
		db,
		inventoryTestDeviceID,
		performanceOtherSampleID,
		performanceTestTimestamp+1000,
	)

	insertPerformanceTestSample(
		t,
		db,
		inventoryOtherDeviceID,
		performanceTestSampleID,
		performanceTestTimestamp,
	)

	inventoryExec(t, db, `
		DELETE FROM devices
		WHERE id = ?
	`, inventoryTestDeviceID)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*) FROM device_performance
		WHERE device_id = ?
	`, inventoryTestDeviceID)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*) FROM device_performance
		WHERE device_id = ?
	`, inventoryOtherDeviceID)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*) FROM device_performance
	`)
}

func TestPerformanceMigrationConstraints(t *testing.T) {
	db := openInventoryTestDB(t)

	insertInventoryTestDevice(
		t, db, inventoryTestDeviceID,
	)

	insertPerformanceTestSample(
		t,
		db,
		inventoryTestDeviceID,
		performanceTestSampleID,
		performanceTestTimestamp,
	)

	tests := []struct {
		name   string
		column string
		value  any
	}{
		{"null device ID", "device_id", nil},
		{"empty device ID", "device_id", ""},
		{"null sample ID", "sample_id", nil},
		{"empty sample ID", "sample_id", ""},
		{"short sample ID", "sample_id", strings.Repeat("a", 31)},
		{"long sample ID", "sample_id", strings.Repeat("a", 33)},
		{"uppercase sample ID", "sample_id", strings.Repeat("A", 32)},
		{"nonhexadecimal sample ID", "sample_id", strings.Repeat("g", 32)},
		{"embedded NUL", "sample_id", strings.Repeat("a", 31) + "\x00"},

		{"null collection time", "collected_at", nil},
		{"zero collection time", "collected_at", 0},
		{"negative collection time", "collected_at", -1},
		{"fractional collection time", "collected_at", 1.5},
		{"text collection time", "collected_at", "invalid"},

		{"null receipt time", "received_at", nil},
		{"zero receipt time", "received_at", 0},
		{"negative receipt time", "received_at", -1},
		{"fractional receipt time", "received_at", 1.5},
		{"text receipt time", "received_at", "invalid"},

		{"null payload version", "payload_version", nil},
		{"zero payload version", "payload_version", 0},
		{"negative payload version", "payload_version", -1},
		{"fractional payload version", "payload_version", 1.5},
		{"text payload version", "payload_version", "invalid"},

		{"null payload", "payload", nil},
		{"empty payload", "payload", []byte{}},
		{"text payload", "payload", "not binary"},
		{"integer payload", "payload", 123},
		{"oversized payload", "payload", make([]byte, 524289)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Column names originate exclusively from
			// fixed test constants.
			query := `
				UPDATE device_performance
				SET ` + tt.column + ` = ?
				WHERE device_id = ?
					AND sample_id = ?
			`

			inventoryAssertConstraint(
				t,
				db,
				query,
				tt.value,
				inventoryTestDeviceID,
				performanceTestSampleID,
			)
		})
	}

	// Failed updates must preserve the original sample.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM device_performance
		WHERE device_id = ?
			AND sample_id = ?
			AND collected_at = ?
			AND received_at = ?
			AND payload_version = 1
			AND payload = X'0A020801'
	`,
		inventoryTestDeviceID,
		performanceTestSampleID,
		performanceTestTimestamp,
		performanceTestTimestamp+1000,
	)
}

func TestPerformanceMigrationPayloadSizeBoundary(t *testing.T) {
	db := openInventoryTestDB(t)

	insertInventoryTestDevice(
		t, db, inventoryTestDeviceID,
	)

	const maxPayloadBytes = 512 * 1024

	inventoryExec(
		t,
		db,
		insertPerformanceTestSQL,
		inventoryTestDeviceID,
		performanceTestSampleID,
		performanceTestTimestamp,
		performanceTestTimestamp,
		1,
		make([]byte, maxPayloadBytes),
	)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*) FROM device_performance
		WHERE length(payload) = ?
			AND typeof(payload) = 'blob'
	`, maxPayloadBytes)
}

func TestPerformanceMigrationClockSkew(t *testing.T) {
	db := openInventoryTestDB(t)

	insertInventoryTestDevice(
		t, db, inventoryTestDeviceID,
	)

	// The collection timestamp can be slightly ahead
	// of the appliance receipt timestamp.
	inventoryExec(
		t,
		db,
		insertPerformanceTestSQL,
		inventoryTestDeviceID,
		performanceTestSampleID,
		performanceTestTimestamp+60000,
		performanceTestTimestamp,
		1,
		[]byte{0x01},
	)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*) FROM device_performance
		WHERE collected_at > received_at
	`)
}

func TestPerformanceMigrationDatabaseReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	insertInventoryTestDevice(
		t, db, inventoryTestDeviceID,
	)

	expected := []byte{
		0x00, 0x01, 0x02, 0x7f, 0x80, 0xff,
	}

	inventoryExec(
		t,
		db,
		insertPerformanceTestSQL,
		inventoryTestDeviceID,
		performanceTestSampleID,
		performanceTestTimestamp,
		performanceTestTimestamp+1000,
		1,
		expected,
	)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	assertInventorySchemaVersion(
		t,
		reopened,
		len(migrations),
	)

	var (
		collectedAt int64
		receivedAt  int64
		version     int
		payload     []byte
	)

	err = reopened.QueryRow(`
		SELECT
			collected_at,
			received_at,
			payload_version,
			payload
		FROM device_performance
		WHERE device_id = ?
			AND sample_id = ?
	`,
		inventoryTestDeviceID,
		performanceTestSampleID,
	).Scan(
		&collectedAt,
		&receivedAt,
		&version,
		&payload,
	)
	if err != nil {
		t.Fatal(err)
	}

	if collectedAt != performanceTestTimestamp {
		t.Fatal("collection timestamp changed")
	}

	if receivedAt != performanceTestTimestamp+1000 {
		t.Fatal("receipt timestamp changed")
	}

	if version != 1 {
		t.Fatal("payload version changed")
	}

	if !bytes.Equal(payload, expected) {
		t.Fatal("binary payload changed")
	}
}

func TestPerformanceMigrationRollback(t *testing.T) {
	dir := t.TempDir()

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

	// Intentionally create an index naming conflict.
	// Migration 006 should fail after attempting to
	// create the table and its historical query index.
	inventoryExec(t, db, `
		CREATE INDEX idx_device_performance_received_at
		ON devices(created_at)
	`)

	err = migrate(context.Background(), db)

	if err == nil {
		t.Fatal("expected migration failure")
	}

	if !strings.Contains(
		err.Error(),
		"apply migration 6",
	) {
		t.Fatalf("unexpected migration error: %v", err)
	}

	// The entire failed migration must roll back.
	assertInventorySchemaVersion(t, db, 5)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE name = 'device_performance'
	`)

	inventoryAssertCount(t, db, 0, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE name = 'idx_device_performance_history'
	`)

	// Pre-existing records must remain unchanged.
	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*) FROM device_inventory
		WHERE device_id = ?
	`, inventoryTestDeviceID)

	// Remove the intentional conflict and retry.
	inventoryExec(t, db, `
		DROP INDEX idx_device_performance_received_at
	`)

	if err := migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}

	assertInventorySchemaVersion(
		t,
		db,
		len(migrations),
	)

	inventoryAssertCount(t, db, 1, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE name = 'device_performance'
			AND type = 'table'
	`)
}
