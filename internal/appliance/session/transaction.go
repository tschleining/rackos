package session

import (
	"context"
	"database/sql"
	"fmt"
)

// beginWriteTransaction acquires SQLite's write
// transaction before the caller samples the clock.
//
// BeginTx alone starts a deferred transaction.
// A write statement is necessary to acquire the
// SQLite writer lock before evaluating expiration.
func beginWriteTransaction(
	ctx context.Context,
	db *sql.DB,
) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	// This statement changes no rows but starts
	// a SQLite write transaction.
	_, err = tx.ExecContext(
		ctx,
		`UPDATE administrator_sessions
		 SET last_activity_at = last_activity_at
		 WHERE 0`,
	)
	if err != nil {
		_ = tx.Rollback()

		return nil, fmt.Errorf(
			"acquire session database write lock: %w",
			err,
		)
	}

	return tx, nil
}
