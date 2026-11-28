package session

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/audit"
)

type Repository struct {
	db  *sql.DB
	now func() time.Time

	clockMu        sync.Mutex
	clockHighWater int64
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db:  db,
		now: time.Now,
	}
}

// currentTimestamp rejects a backward movement
// relative to the latest observed session clock.
//
// Repository instances must not be copied after use.
func (r *Repository) currentTimestamp() (
	int64,
	error,
) {
	r.clockMu.Lock()
	defer r.clockMu.Unlock()

	now := r.now().UnixMilli()

	if now <= 0 {
		return 0, ErrInvalidClock
	}

	if now < r.clockHighWater {
		return 0, ErrClockRollback
	}

	r.clockHighWater = now

	return now, nil
}

// Issue creates a session for credentials that have
// already been successfully verified.
//
// verifiedAccount must be the original snapshot
// associated with password verification.
func (r *Repository) Issue(
	ctx context.Context,
	verifiedAccount admin.Administrator,
) (IssuedSession, error) {
	if err := ctx.Err(); err != nil {
		return IssuedSession{}, err
	}

	revision := verifiedAccount.
		PasswordChangedAt.UnixMilli()

	if !admin.IsValidID(verifiedAccount.ID) ||
		verifiedAccount.Role != admin.RoleAdministrator ||
		revision <= 0 {
		return IssuedSession{}, ErrInvalidAdministrator
	}

	if !verifiedAccount.IsActive() {
		return IssuedSession{}, ErrCredentialsChanged
	}

	id, err := generateSessionID()
	if err != nil {
		return IssuedSession{}, err
	}

	token, digest, err := generateToken()
	if err != nil {
		return IssuedSession{}, err
	}
	defer clear(digest[:])

	tx, err := beginWriteTransaction(ctx, r.db)
	if err != nil {
		return IssuedSession{}, fmt.Errorf(
			"begin session issuance transaction: %w",
			err,
		)
	}
	defer tx.Rollback()

	createdAt, err := r.currentTimestamp()
	if err != nil {
		return IssuedSession{}, err
	}

	if createdAt < revision {
		return IssuedSession{}, ErrInvalidClock
	}

	expiresAt := createdAt +
		AbsoluteLifetime.Milliseconds()

	if expiresAt <= createdAt {
		return IssuedSession{}, ErrInvalidClock
	}

	// Remove invalid sessions before calculating
	// available session capacity.
	if _, err := pruneInvalidSessions(
		ctx,
		tx,
		createdAt,
	); err != nil {
		return IssuedSession{}, err
	}

	// Credential validation, capacity enforcement
	// and insertion occur within one SQL statement.
	result, err := tx.ExecContext(
		ctx,
		`INSERT INTO administrator_sessions (
			id,
			administrator_id,
			token_hash,
			password_changed_at,
			created_at,
			last_activity_at,
			expires_at
		)
		SELECT
			?,
			id,
			?,
			password_changed_at,
			?,
			?,
			?
		FROM administrators
		WHERE id = ?
			AND password_changed_at = ?
			AND disabled_at IS NULL
			AND role = 'administrator'
			AND (
				SELECT COUNT(*)
				FROM administrator_sessions AS s
				WHERE s.administrator_id = ?
			) < ?`,
		id,
		digest[:],
		createdAt,
		createdAt,
		expiresAt,
		verifiedAccount.ID,
		revision,
		verifiedAccount.ID,
		MaxActiveSessions,
	)
	if err != nil {
		return IssuedSession{}, fmt.Errorf(
			"insert administrator session: %w",
			err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return IssuedSession{}, fmt.Errorf(
			"read session insertion result: %w",
			err,
		)
	}

	if affected == 0 {
		var eligible bool

		err := tx.QueryRowContext(
			ctx,
			`SELECT EXISTS (
				SELECT 1
				FROM administrators
				WHERE id = ?
					AND password_changed_at = ?
					AND disabled_at IS NULL
					AND role = 'administrator'
			)`,
			verifiedAccount.ID,
			revision,
		).Scan(&eligible)
		if err != nil {
			return IssuedSession{}, fmt.Errorf(
				"check administrator session eligibility: %w",
				err,
			)
		}

		if !eligible {
			return IssuedSession{}, ErrCredentialsChanged
		}

		return IssuedSession{}, ErrSessionLimit
	}

	if affected != 1 {
		return IssuedSession{}, fmt.Errorf(
			"unexpected session insertion count: %d",
			affected,
		)
	}

	result, err = tx.ExecContext(
		ctx,
		`UPDATE administrators
		SET last_login_at = MAX(
				COALESCE(last_login_at, 0),
				?
			),
			updated_at = MAX(updated_at, ?)
		WHERE id = ?
			AND password_changed_at = ?
			AND disabled_at IS NULL`,
		createdAt,
		createdAt,
		verifiedAccount.ID,
		revision,
	)
	if err != nil {
		return IssuedSession{}, fmt.Errorf(
			"record administrator login timestamp: %w",
			err,
		)
	}

	affected, err = result.RowsAffected()
	if err != nil {
		return IssuedSession{}, fmt.Errorf(
			"read login timestamp update result: %w",
			err,
		)
	}

	if affected != 1 {
		return IssuedSession{}, ErrCredentialsChanged
	}

	auditRepository := audit.NewTxRepository(tx)

	if err := auditRepository.RecordLoginSucceeded(
		ctx,
		verifiedAccount.ID,
		id,
	); err != nil {
		return IssuedSession{}, err
	}

	if err := tx.Commit(); err != nil {
		return IssuedSession{}, fmt.Errorf(
			"commit session issuance transaction: %w",
			err,
		)
	}

	created := time.UnixMilli(createdAt).UTC()

	return IssuedSession{
		Session: Session{
			ID:              id,
			AdministratorID: verifiedAccount.ID,
			PasswordChangedAt: time.UnixMilli(
				revision,
			).UTC(),
			CreatedAt:      created,
			LastActivityAt: created,
			ExpiresAt: time.UnixMilli(
				expiresAt,
			).UTC(),
		},

		token: token,
	}, nil
}
