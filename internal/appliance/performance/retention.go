package performance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tschleining/rackos/internal/appliance/retention"
)

var ErrInvalidRetentionQuery = errors.New(
	"invalid performance retention query",
)

// deleteExpiredBatchSQL deletes a bounded number
// of expired performance records.
//
// The receipt timestamp index supports oldest-first
// selection without decoding stored payloads.
//
// SQLite rowid provides a deterministic tie-breaker
// when receipt timestamps are identical.
const deleteExpiredBatchSQL = `
	DELETE FROM device_performance
	WHERE rowid IN (
		SELECT rowid
		FROM device_performance
		WHERE received_at < ?
		ORDER BY received_at ASC, rowid ASC
		LIMIT ?
	)
`

// DeleteExpiredBatch deletes at most limit historical
// performance records received before cutoff.
//
// Records exactly at the cutoff are retained.
//
// When operating on a database connection, the SQL
// statement commits atomically.
//
// When operating within an existing transaction,
// deletion remains provisional until the caller
// commits that transaction.
//
// The caller is responsible for calculating the
// cutoff according to the configured retention policy.
func (r *Repository) DeleteExpiredBatch(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	if err := validateRetentionQuery(
		cutoff,
		limit,
	); err != nil {
		return 0, err
	}

	result, err := r.db.ExecContext(
		ctx,
		deleteExpiredBatchSQL,
		cutoff.UnixMilli(),
		limit,
	)
	if err != nil {
		return 0, fmt.Errorf(
			"delete expired performance batch: %w",
			err,
		)
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(
			"read performance deletion count: %w",
			err,
		)
	}

	return deleted, nil
}

// validateRetentionQuery prevents invalid timestamp
// boundaries and unbounded deletion requests.
func validateRetentionQuery(
	cutoff time.Time,
	limit int,
) error {
	cutoff = cutoff.UTC()

	if cutoff.IsZero() ||
		cutoff.Year() < 1970 ||
		cutoff.Year() > 9999 ||
		cutoff.UnixMilli() <= 0 {
		return fmt.Errorf(
			"%w: cutoff outside supported range",
			ErrInvalidRetentionQuery,
		)
	}

	if cutoff.Nanosecond()%int(time.Millisecond) != 0 {
		return fmt.Errorf(
			"%w: cutoff must be millisecond aligned",
			ErrInvalidRetentionQuery,
		)
	}

	if limit < retention.MinBatchSize ||
		limit > retention.MaxBatchSize {
		return fmt.Errorf(
			"%w: batch size must be between %d and %d",
			ErrInvalidRetentionQuery,
			retention.MinBatchSize,
			retention.MaxBatchSize,
		)
	}

	return nil
}
