package audit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
)

type EventType string

const (
	EventAdminCreated EventType = "admin.created"

	EventAdminPasswordChanged EventType = "admin.password.changed"

	EventLoginSucceeded EventType = "auth.login.succeeded"
)

var (
	ErrInvalidEvent = errors.New(
		"invalid audit event",
	)

	ErrTransactionRequired = errors.New(
		"audit recording requires a transaction",
	)
)

// Repository records security events within an existing
// database transaction.
//
// It does not own the transaction and cannot commit it.
type Repository struct {
	tx  *sql.Tx
	now func() time.Time
}

func NewTxRepository(tx *sql.Tx) *Repository {
	return &Repository{
		tx:  tx,
		now: time.Now,
	}
}

// RecordAdministratorChange records a successful local
// administrator creation or password replacement.
func (r *Repository) RecordAdministratorChange(
	ctx context.Context,
	event EventType,
	administratorID string,
) error {
	switch event {
	case EventAdminCreated,
		EventAdminPasswordChanged:
	default:
		return ErrInvalidEvent
	}

	if !admin.IsValidID(administratorID) {
		return ErrInvalidEvent
	}

	return r.record(
		ctx,
		event,
		"local_operator",
		nil,
		"administrator",
		administratorID,
	)
}

// RecordLoginSucceeded records successful issuance of
// an authenticated administrator session.
//
// The supplied session ID is a nonsecret identifier,
// not the authentication token or its digest.
func (r *Repository) RecordLoginSucceeded(
	ctx context.Context,
	administratorID string,
	sessionID string,
) error {
	if !admin.IsValidID(administratorID) ||
		!isSessionID(sessionID) {
		return ErrInvalidEvent
	}

	return r.record(
		ctx,
		EventLoginSucceeded,
		"administrator",
		administratorID,
		"session",
		sessionID,
	)
}

// record is private so callers cannot supply arbitrary
// event types, actor kinds or diagnostic messages.
func (r *Repository) record(
	ctx context.Context,
	event EventType,
	actorKind string,
	actorID any,
	targetKind string,
	targetID string,
) error {
	if r.tx == nil {
		return ErrTransactionRequired
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	timestamp := r.now().UnixMilli()

	if timestamp <= 0 {
		return ErrInvalidEvent
	}

	id, err := newEventID()
	if err != nil {
		return fmt.Errorf(
			"generate audit event identity: %w",
			err,
		)
	}

	_, err = r.tx.ExecContext(
		ctx,
		`INSERT INTO audit_events (
			id,
			occurred_at,
			event_type,
			actor_kind,
			actor_id,
			target_kind,
			target_id,
			outcome
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'success')`,
		id,
		timestamp,
		string(event),
		actorKind,
		actorID,
		targetKind,
		targetID,
	)

	if err != nil {
		return fmt.Errorf(
			"record administrative audit event: %w",
			err,
		)
	}

	return nil
}

func isSessionID(id string) bool {
	if len(id) != 32 {
		return false
	}

	decoded, err := hex.DecodeString(id)

	return err == nil &&
		len(decoded) == 16 &&
		hex.EncodeToString(decoded) == id
}

func newEventID() (string, error) {
	var value [16]byte

	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}

	return hex.EncodeToString(value[:]), nil
}
