package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const sessionColumns = `
	id,
	administrator_id,
	password_changed_at,
	created_at,
	last_activity_at,
	expires_at
`

// Shared validity rules for validation, renewal,
// revocation and expired-session cleanup.
//
// Argument order:
//
//  1. Current timestamp
//  2. Current timestamp
//  3. Idle expiration cutoff
//  4. Current timestamp
const validSessionStatePredicate = `
	created_at <= ?
	AND last_activity_at <= ?
	AND last_activity_at > ?
	AND expires_at > ?
	AND EXISTS (
		SELECT 1
		FROM administrators AS a
		WHERE a.id =
			administrator_sessions.administrator_id
		AND a.password_changed_at =
			administrator_sessions.password_changed_at
		AND a.disabled_at IS NULL
		AND a.role = 'administrator'
	)
`

const validSessionPredicate = `
	token_hash = ?
	AND
` + validSessionStatePredicate

// Validate authenticates a session without modifying
// its activity timestamp or extending its lifetime.
func (r *Repository) Validate(
	ctx context.Context,
	token string,
) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}

	digest, err := hashToken(token)
	if err != nil {
		return Session{}, ErrInvalidSession
	}
	defer clear(digest[:])

	conn, err := r.db.Conn(ctx)
	if err != nil {
		return Session{}, fmt.Errorf(
			"acquire session validation connection: %w",
			err,
		)
	}
	defer conn.Close()

	now, err := r.currentTimestamp()
	if errors.Is(err, ErrClockRollback) {
		return Session{}, ErrInvalidSession
	}
	if err != nil {
		return Session{}, err
	}

	row := conn.QueryRowContext(
		ctx,
		"SELECT "+sessionColumns+
			" FROM administrator_sessions WHERE "+
			validSessionPredicate,
		digest[:],
		now,
		now,
		now-IdleTimeout.Milliseconds(),
		now,
	)

	return readValidatedSession(row)
}

// RecordActivity authenticates a session and renews
// its inactivity window.
//
// It cannot revive an expired session or extend
// its original absolute expiration.
func (r *Repository) RecordActivity(
	ctx context.Context,
	token string,
) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}

	digest, err := hashToken(token)
	if err != nil {
		return Session{}, ErrInvalidSession
	}
	defer clear(digest[:])

	tx, err := beginWriteTransaction(ctx, r.db)
	if err != nil {
		return Session{}, fmt.Errorf(
			"begin session activity transaction: %w",
			err,
		)
	}
	defer tx.Rollback()

	now, err := r.currentTimestamp()
	if errors.Is(err, ErrClockRollback) {
		return Session{}, ErrInvalidSession
	}
	if err != nil {
		return Session{}, err
	}

	row := tx.QueryRowContext(
		ctx,
		`UPDATE administrator_sessions
		 SET last_activity_at = ?
		 WHERE `+validSessionPredicate+
			" RETURNING "+sessionColumns,
		now,
		digest[:],
		now,
		now,
		now-IdleTimeout.Milliseconds(),
		now,
	)

	current, err := readValidatedSession(row)
	if err != nil {
		return Session{}, err
	}

	if err := tx.Commit(); err != nil {
		return Session{}, fmt.Errorf(
			"commit session activity transaction: %w",
			err,
		)
	}

	return current, nil
}

func readValidatedSession(
	row *sql.Row,
) (Session, error) {
	var current Session

	var revision int64
	var createdAt int64
	var lastActivityAt int64
	var expiresAt int64

	err := row.Scan(
		&current.ID,
		&current.AdministratorID,
		&revision,
		&createdAt,
		&lastActivityAt,
		&expiresAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrInvalidSession
	}

	if err != nil {
		return Session{}, fmt.Errorf(
			"read validated administrator session: %w",
			err,
		)
	}

	current.PasswordChangedAt =
		time.UnixMilli(revision).UTC()

	current.CreatedAt =
		time.UnixMilli(createdAt).UTC()

	current.LastActivityAt =
		time.UnixMilli(lastActivityAt).UTC()

	current.ExpiresAt =
		time.UnixMilli(expiresAt).UTC()

	return current, nil
}
