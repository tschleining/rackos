package performance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Latest retrieves the most recently collected
// performance snapshot for a device.
//
// Ordering uses the indexed collection timestamp,
// with sample identity as a deterministic tie-breaker.
//
// ErrNotFound is returned when the device has no
// historical performance records.
//
// Device registration is checked separately by
// callers that need to distinguish unknown devices
// from devices without performance history.
func (r *Repository) Latest(
	ctx context.Context,
	deviceID string,
) (Performance, error) {
	if err := ctx.Err(); err != nil {
		return Performance{}, err
	}

	if err := validateQueryDeviceID(deviceID); err != nil {
		return Performance{}, err
	}

	query := `
		SELECT ` + performanceColumns + `
		FROM device_performance
		WHERE device_id = ?
		ORDER BY collected_at DESC, sample_id DESC
		LIMIT 1
	`

	record, err := scanPerformance(
		r.db.QueryRowContext(
			ctx,
			query,
			deviceID,
		),
	)

	if errors.Is(err, sql.ErrNoRows) {
		return Performance{}, ErrNotFound
	}

	if err != nil {
		return Performance{}, fmt.Errorf(
			"get latest device performance: %w",
			err,
		)
	}

	return record, nil
}

// readHistoryRows retrieves a bounded page of historical
// performance records.
//
// Time bounds use indexed Unix millisecond timestamps.
//
// The lower bound is inclusive and the upper bound
// is exclusive.
//
// Results are ordered by collection timestamp and
// sample identity, both descending.
//
// One additional row is inspected to determine whether
// more records exist without decoding its payload.
func (r *Repository) readHistoryRows(
	ctx context.Context,
	deviceID string,
	options HistoryQuery,
) ([]Performance, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	if err := validateQueryDeviceID(deviceID); err != nil {
		return nil, false, err
	}

	if err := options.Validate(); err != nil {
		return nil, false, err
	}

	query := `
		SELECT ` + performanceColumns + `
		FROM device_performance
		WHERE device_id = ?
	`

	args := []any{deviceID}

	if options.FromMillis != nil {
		query += `
			AND collected_at >= ?
		`

		args = append(
			args,
			*options.FromMillis,
		)
	}

	if options.ToMillis != nil {
		query += `
			AND collected_at < ?
		`

		args = append(
			args,
			*options.ToMillis,
		)
	}

	if options.After != nil {
		query += `
			AND (collected_at, sample_id) < (?, ?)
		`

		args = append(
			args,
			options.After.CollectedAtMillis,
			options.After.SampleID,
		)
	}

	query += `
		ORDER BY collected_at DESC, sample_id DESC
		LIMIT ?
	`

	// Request one additional row to determine
	// whether another page exists.
	args = append(
		args,
		options.Limit+1,
	)

	rows, err := r.db.QueryContext(
		ctx,
		query,
		args...,
	)
	if err != nil {
		return nil, false, fmt.Errorf(
			"query device performance history: %w",
			err,
		)
	}

	defer rows.Close()

	samples := make(
		[]Performance,
		0,
		options.Limit,
	)

	hasMore := false

	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}

		// The additional row establishes that
		// another page exists. Its payload will
		// be decoded when that page is requested.
		if len(samples) == options.Limit {
			hasMore = true
			break
		}

		record, err := scanPerformance(rows)
		if err != nil {
			return nil, false, fmt.Errorf(
				"scan device performance history: %w",
				err,
			)
		}

		samples = append(
			samples,
			record,
		)
	}

	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf(
			"iterate device performance history: %w",
			err,
		)
	}

	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	return samples, hasMore, nil
}

// History retrieves a bounded page of historical
// performance snapshots for a device.
//
// Results are ordered by indexed collection timestamp
// and sample identity, both descending.
//
// When additional records exist, NextCursor identifies
// the last returned sample and can be supplied in the
// next query.
//
// Empty history returns a non-nil empty sample slice.
//
// Device registration is checked separately by callers
// that need to distinguish unknown devices from devices
// without performance history.
func (r *Repository) History(
	ctx context.Context,
	deviceID string,
	options HistoryQuery,
) (HistoryPage, error) {
	samples, hasMore, err := r.readHistoryRows(
		ctx,
		deviceID,
		options,
	)
	if err != nil {
		return HistoryPage{}, err
	}

	page := HistoryPage{
		Samples: samples,
	}

	if hasMore {
		last := samples[len(samples)-1]

		page.NextCursor = &HistoryCursor{
			CollectedAtMillis: last.CollectedAt.UnixMilli(),
			SampleID:          last.SampleID,
		}
	}

	return page, nil
}
