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
