package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/audit"
)

// Revoke deletes an authenticated session and records
// its logout event within the same transaction.
//
// Unknown, malformed, expired or already revoked
// tokens return false without creating an audit event.
func (r *Repository) Revoke(
	ctx context.Context,
	token string,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	digest, err := hashToken(token)
	if err != nil {
		return false, nil
	}
	defer clear(digest[:])

	tx, err := beginWriteTransaction(ctx, r.db)
	if err != nil {
		return false, fmt.Errorf(
			"begin session revocation transaction: %w",
			err,
		)
	}
	defer tx.Rollback()

	now, err := r.currentTimestamp()
	if err != nil {
		return false, err
	}

	var sessionID string
	var administratorID string

	err = tx.QueryRowContext(
		ctx,
		`DELETE FROM administrator_sessions
		 WHERE `+validSessionPredicate+
			" RETURNING id, administrator_id",
		digest[:],
		now,
		now,
		now-IdleTimeout.Milliseconds(),
		now,
	).Scan(
		&sessionID,
		&administratorID,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf(
			"revoke administrator session: %w",
			err,
		)
	}

	if err := audit.NewTxRepository(tx).
		RecordLogout(
			ctx,
			administratorID,
			sessionID,
		); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf(
			"commit session revocation: %w",
			err,
		)
	}

	return true, nil
}

// RevokeAllForAdministrator removes every session
// belonging to the specified administrator.
//
// The caller owns the transaction and must record
// the corresponding administrative audit event.
//
// This function will be used during password recovery.
func RevokeAllForAdministrator(
	ctx context.Context,
	tx *sql.Tx,
	administratorID string,
) (int64, error) {
	if tx == nil {
		return 0, ErrTransactionRequired
	}

	if !admin.IsValidID(administratorID) {
		return 0, ErrInvalidAdministrator
	}

	result, err := tx.ExecContext(
		ctx,
		`DELETE FROM administrator_sessions
		 WHERE administrator_id = ?`,
		administratorID,
	)
	if err != nil {
		return 0, fmt.Errorf(
			"revoke administrator sessions: %w",
			err,
		)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(
			"read administrator revocation count: %w",
			err,
		)
	}

	return count, nil
}

// Cleanup permanently removes expired, inactive,
// future-dated and otherwise invalid sessions.
func (r *Repository) Cleanup(
	ctx context.Context,
) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	tx, err := beginWriteTransaction(ctx, r.db)
	if err != nil {
		return 0, fmt.Errorf(
			"begin session cleanup transaction: %w",
			err,
		)
	}
	defer tx.Rollback()

	now, err := r.currentTimestamp()
	if err != nil {
		return 0, err
	}

	count, err := pruneInvalidSessions(
		ctx,
		tx,
		now,
	)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf(
			"commit session cleanup: %w",
			err,
		)
	}

	return count, nil
}

// pruneInvalidSessions reuses the exact same session
// state rules as authentication.
//
// The caller owns the transaction.
func pruneInvalidSessions(
	ctx context.Context,
	tx *sql.Tx,
	now int64,
) (int64, error) {
	result, err := tx.ExecContext(
		ctx,
		`DELETE FROM administrator_sessions
		 WHERE NOT (`+validSessionStatePredicate+
			")",
		now,
		now,
		now-IdleTimeout.Milliseconds(),
		now,
	)
	if err != nil {
		return 0, fmt.Errorf(
			"delete invalid administrator sessions: %w",
			err,
		)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(
			"read session cleanup count: %w",
			err,
		)
	}

	return count, nil
}

// InvalidateOnStartup removes all persisted sessions.
//
// This must only be called during daemon startup,
// before management requests are accepted.
//
// Historical audit records are preserved.
func (r *Repository) InvalidateOnStartup(
	ctx context.Context,
) (int64, error) {
	result, err := r.db.ExecContext(
		ctx,
		"DELETE FROM administrator_sessions",
	)
	if err != nil {
		return 0, fmt.Errorf(
			"invalidate persisted sessions at startup: %w",
			err,
		)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(
			"read startup session invalidation count: %w",
			err,
		)
	}

	return count, nil
}
