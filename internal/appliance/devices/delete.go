package devices

import (
	"context"
	"fmt"
)

// Delete permanently removes a registered device.
//
// Associated agent credentials are removed automatically
// through the database's foreign key cascade.
//
// The caller is responsible for obtaining administrative
// authorization and confirmation before calling Delete.
func (r *Repository) Delete(
	ctx context.Context,
	id string,
) error {
	if id == "" {
		return ErrInvalidDevice
	}

	result, err := r.db.ExecContext(
		ctx,
		`DELETE FROM devices WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("delete device: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf(
			"determine deleted device count: %w",
			err,
		)
	}

	if affected == 0 {
		return ErrNotFound
	}

	return nil
}
