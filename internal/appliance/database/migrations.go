package database

import (
	"context"
	"database/sql"
	"fmt"
)

var migrations = []string{
	// Migration 001: Appliance metadata.
	`
	CREATE TABLE appliance_metadata (
		key TEXT PRIMARY KEY NOT NULL,
		value TEXT NOT NULL
	);
	`,

	// Migration 002: Managed devices.
	`
	CREATE TABLE devices (
		id TEXT PRIMARY KEY NOT NULL,
		hostname TEXT NOT NULL,
		os TEXT NOT NULL,
		architecture TEXT NOT NULL,
		agent_version TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		last_seen_at INTEGER NOT NULL
	);

	CREATE INDEX idx_devices_last_seen_at
		ON devices(last_seen_at);
	`,

	// Migration 003: Agent enrollment tokens.
	`
	CREATE TABLE enrollment_tokens (
		id TEXT PRIMARY KEY NOT NULL,
		token_hash BLOB UNIQUE NOT NULL
			CHECK(length(token_hash) = 32),
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL,
		used_at INTEGER,
		revoked_at INTEGER,
		CHECK(expires_at > created_at)
	);

	CREATE INDEX idx_enrollment_tokens_expires_at
		ON enrollment_tokens(expires_at);
	`,

	// Migration 004: Device authentication credentials.
	`
	CREATE TABLE agent_credentials (
		device_id TEXT PRIMARY KEY NOT NULL,
		token_hash BLOB UNIQUE NOT NULL
			CHECK(length(token_hash) = 32),
		created_at INTEGER NOT NULL,
		revoked_at INTEGER,

		FOREIGN KEY(device_id)
			REFERENCES devices(id)
			ON DELETE CASCADE
	);
	`,

	`
	CREATE TABLE device_inventory (
		device_id TEXT PRIMARY KEY NOT NULL,

		hostname TEXT NOT NULL
			CHECK(length(CAST(hostname AS BLOB)) BETWEEN 1 AND 255),

		os TEXT NOT NULL
			CHECK(length(CAST(os AS BLOB)) BETWEEN 1 AND 64),

		architecture TEXT NOT NULL
			CHECK(length(CAST(architecture AS BLOB)) BETWEEN 1 AND 64),

		logical_cpus INTEGER NOT NULL
			CHECK(logical_cpus BETWEEN 1 AND 1048576),

		host_logical_cpus INTEGER NOT NULL DEFAULT 0
			CHECK(host_logical_cpus BETWEEN 0 AND 1048576),

		physical_cores INTEGER NOT NULL DEFAULT 0
			CHECK(physical_cores BETWEEN 0 AND 1048576),

		cpu_model TEXT NOT NULL DEFAULT ''
			CHECK(length(CAST(cpu_model AS BLOB)) <= 256),

		cpu_manufacturer TEXT NOT NULL DEFAULT ''
			CHECK(length(CAST(cpu_manufacturer AS BLOB)) <= 256),

		memory_bytes INTEGER NOT NULL DEFAULT 0
			CHECK(memory_bytes >= 0),

		hardware_model TEXT NOT NULL DEFAULT ''
			CHECK(length(CAST(hardware_model AS BLOB)) <= 256),

		os_version TEXT NOT NULL DEFAULT ''
			CHECK(length(CAST(os_version AS BLOB)) <= 128),

		kernel_version TEXT NOT NULL DEFAULT ''
			CHECK(length(CAST(kernel_version AS BLOB)) <= 128),

		boot_time INTEGER NOT NULL DEFAULT 0
			CHECK(boot_time >= 0),

		collected_at INTEGER NOT NULL
			CHECK(collected_at > 0),

		received_at INTEGER NOT NULL
			CHECK(received_at > 0),

		CHECK(
			host_logical_cpus = 0
			OR physical_cores <= host_logical_cpus
		),

		CHECK(
			boot_time = 0
			OR boot_time <= collected_at
		),

		FOREIGN KEY(device_id)
			REFERENCES devices(id)
			ON DELETE CASCADE
	);
	`,

	// Migration 006: Historical device performance.
	`
	CREATE TABLE device_performance (
		device_id TEXT NOT NULL
			CHECK(
				length(CAST(device_id AS BLOB))
					BETWEEN 1 AND 1024
			),

		sample_id TEXT NOT NULL
			CHECK(
				length(sample_id) = 32
				AND length(CAST(sample_id AS BLOB)) = 32
				AND sample_id NOT GLOB '*[^0-9a-f]*'
			),

		collected_at INTEGER NOT NULL
			CHECK(
				typeof(collected_at) = 'integer'
				AND collected_at > 0
			),

		received_at INTEGER NOT NULL
			CHECK(
				typeof(received_at) = 'integer'
				AND received_at > 0
			),

		payload_version INTEGER NOT NULL
			CHECK(
				typeof(payload_version) = 'integer'
				AND payload_version > 0
			),

		payload BLOB NOT NULL
			CHECK(
				typeof(payload) = 'blob'
				AND length(payload) BETWEEN 1 AND 524288
			),

		PRIMARY KEY(device_id, sample_id),

		FOREIGN KEY(device_id)
			REFERENCES devices(id)
			ON DELETE CASCADE
	);

	CREATE INDEX idx_device_performance_history
		ON device_performance(
			device_id,
			collected_at,
			sample_id
		);

	CREATE INDEX idx_device_performance_received_at
		ON device_performance(received_at);
	`,
}

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer tx.Rollback()

	var currentVersion int

	err = tx.QueryRowContext(
		ctx,
		"PRAGMA user_version",
	).Scan(&currentVersion)

	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	if currentVersion < 0 || currentVersion > len(migrations) {
		return fmt.Errorf(
			"unsupported database schema version: %d (supported: 0-%d)",
			currentVersion,
			len(migrations),
		)
	}

	for i := currentVersion; i < len(migrations); i++ {
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			return fmt.Errorf("apply migration %d: %w", i+1, err)
		}

		query := fmt.Sprintf(
			"PRAGMA user_version = %d",
			i+1,
		)

		if _, err := tx.ExecContext(ctx, query); err != nil {
			return fmt.Errorf(
				"update schema version to %d: %w",
				i+1,
				err,
			)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}

	return nil
}
