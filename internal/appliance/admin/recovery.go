package admin

import (
	"context"
	"fmt"
)

// Current retrieves the appliance's administrator
// account without selecting password material.
//
// Disabled administrators are included.
func (r *Repository) Current(
	ctx context.Context,
) (Administrator, error) {
	row := r.db.QueryRowContext(
		ctx,
		"SELECT "+administratorColumns+
			" FROM administrators",
	)

	return readAdministrator(row)
}

// ReplacePassword replaces an administrator's
// password verification material.
//
// The account identity, role, creation timestamp,
// last login timestamp and disabled state are
// preserved.
//
// Future session invalidation must be performed
// within the same transaction as this operation.
func (r *Repository) ReplacePassword(
	ctx context.Context,
	id string,
	digest PasswordDigest,
) error {
	if !IsValidID(id) {
		return ErrInvalidID
	}

	if err := validatePasswordDigest(digest); err != nil {
		return err
	}

	now := r.now().UnixMilli()

	result, err := r.db.ExecContext(
		ctx,
		`UPDATE administrators
		 SET password_scheme = ?,
		     password_salt = ?,
		     password_hash = ?,
		     password_changed_at = MAX(
		         password_changed_at + 1,
		         ?
		     ),
		     updated_at = MAX(
		         updated_at,
		         password_changed_at + 1,
		         ?
		     )
		 WHERE id = ?`,
		digest.Scheme,
		digest.Salt[:],
		digest.Hash[:],
		now,
		now,
		id,
	)

	if err != nil {
		return fmt.Errorf(
			"replace administrator password: %w",
			err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf(
			"read password replacement result: %w",
			err,
		)
	}

	if affected == 0 {
		return ErrNotFound
	}

	return nil
}
