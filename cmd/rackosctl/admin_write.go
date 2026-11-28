package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/audit"
	"github.com/tschleining/rackos/internal/appliance/session"
)

// commitAdministratorChange performs a credential
// operation and records its audit event atomically.
//
// Password recovery also revokes every existing
// administrator session within the same transaction.
//
// Password hashing must already have completed
// before this function is called.
func commitAdministratorChange(
	ctx context.Context,
	db *sql.DB,
	command string,
	username string,
	existingAccount admin.Administrator,
	digest admin.PasswordDigest,
) (admin.Administrator, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return admin.Administrator{}, fmt.Errorf(
			"begin administrator transaction: %w",
			err,
		)
	}
	defer tx.Rollback()

	repository := admin.NewRepository(db).WithTx(tx)

	auditRepository := audit.NewTxRepository(tx)

	var account admin.Administrator
	var event audit.EventType

	switch command {
	case "init":
		account, err = repository.CreateInitial(
			ctx,
			username,
			digest,
		)
		if err != nil {
			return admin.Administrator{}, err
		}

		event = audit.EventAdminCreated

	case "reset-password":
		// Replace password verification material
		// and advance the credential revision.
		err = repository.ReplacePassword(
			ctx,
			existingAccount.ID,
			digest,
		)
		if err != nil {
			return admin.Administrator{}, err
		}

		// Remove every session belonging to this
		// administrator, including expired sessions.
		//
		// Revocation must use the same transaction
		// as password replacement and audit recording.
		_, err = session.RevokeAllForAdministrator(
			ctx,
			tx,
			existingAccount.ID,
		)
		if err != nil {
			return admin.Administrator{}, err
		}

		account, err = repository.Get(
			ctx,
			existingAccount.ID,
		)
		if err != nil {
			return admin.Administrator{}, err
		}

		event = audit.EventAdminPasswordChanged

	default:
		return admin.Administrator{}, errors.New(
			"unsupported administrator write operation",
		)
	}

	if err := auditRepository.RecordAdministratorChange(
		ctx,
		event,
		account.ID,
	); err != nil {
		return admin.Administrator{}, err
	}

	if err := tx.Commit(); err != nil {
		return admin.Administrator{}, fmt.Errorf(
			"commit administrator transaction: %w",
			err,
		)
	}

	return account, nil
}
