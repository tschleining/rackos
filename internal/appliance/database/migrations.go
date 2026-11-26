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
