package performance

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"google.golang.org/protobuf/proto"
)

// Insert persists a historical performance snapshot.
//
// Identical retries return the original receipt
// timestamp without modifying the stored record.
//
// Conflicting samples using an existing identity
// are rejected.
//
// receivedAt must originate from the appliance clock.
//
// When bound to an existing transaction, success
// remains provisional until the caller commits.
func (r *Repository) Insert(
	ctx context.Context,
	request *rackosv1.ReportPerformanceRequest,
	receivedAt time.Time,
) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}

	receivedAt = receivedAt.UTC()

	if receivedAt.IsZero() ||
		receivedAt.Year() < 1970 ||
		receivedAt.Year() > 9999 ||
		receivedAt.UnixMilli() <= 0 {
		return time.Time{}, invalid(
			"receipt timestamp is outside supported range",
		)
	}

	// Validate before reducing receipt timestamp
	// precision to milliseconds.
	if err := ValidateRequest(
		request,
		receivedAt,
	); err != nil {
		return time.Time{}, err
	}

	payload, err := proto.Marshal(
		request.Snapshot,
	)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"serialize performance snapshot: %w",
			err,
		)
	}

	if len(payload) == 0 {
		return time.Time{}, invalid(
			"serialized performance snapshot is empty",
		)
	}

	if len(payload) > MaxRequestBytes {
		return time.Time{}, ErrPerformanceTooLarge
	}

	record := Performance{
		DeviceID: request.DeviceId,
		SampleID: request.SampleId,

		CollectedAt: request.Snapshot.
			CollectedAt.AsTime(),

		ReceivedAt: time.UnixMilli(
			receivedAt.UnixMilli(),
		).UTC(),

		PayloadVersion: CurrentPayloadVersion,
	}

	// Decode the serialized payload so conflict
	// comparison uses the content being persisted,
	// independently of the caller's snapshot pointer.
	record.Snapshot = &rackosv1.PerformanceSnapshot{}

	if err := proto.Unmarshal(
		payload,
		record.Snapshot,
	); err != nil {
		return time.Time{}, invalid(
			"cannot decode serialized performance snapshot",
		)
	}

	switch db := r.db.(type) {
	case *sql.DB:
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return time.Time{}, fmt.Errorf(
				"begin performance transaction: %w",
				err,
			)
		}
		defer tx.Rollback()

		acceptedAt, err := r.WithTx(tx).insert(
			ctx,
			record,
			payload,
		)
		if err != nil {
			return time.Time{}, err
		}

		if err := tx.Commit(); err != nil {
			return time.Time{}, fmt.Errorf(
				"commit performance transaction: %w",
				err,
			)
		}

		return acceptedAt, nil

	case *sql.Tx:
		return r.insert(
			ctx,
			record,
			payload,
		)

	default:
		return time.Time{}, fmt.Errorf(
			"unsupported performance repository executor",
		)
	}
}

// insert performs the historical write inside an
// existing database transaction.
func (r *Repository) insert(
	ctx context.Context,
	record Performance,
	payload []byte,
) (time.Time, error) {
	query := `
		INSERT INTO device_performance (
			` + performanceColumns + `
		)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id, sample_id)
		DO NOTHING
	`

	result, err := r.db.ExecContext(
		ctx,
		query,
		record.DeviceID,
		record.SampleID,
		record.CollectedAt.UnixMilli(),
		record.ReceivedAt.UnixMilli(),
		record.PayloadVersion,
		payload,
	)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"insert device performance: %w",
			err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"read performance affected rows: %w",
			err,
		)
	}

	switch affected {
	case 1:
		return record.ReceivedAt, nil

	case 0:
		return r.resolvePerformanceConflict(
			ctx,
			record,
		)

	default:
		return time.Time{}, fmt.Errorf(
			"unexpected performance affected row count: %d",
			affected,
		)
	}
}

// resolvePerformanceConflict compares an incoming
// sample with an existing historical record.
func (r *Repository) resolvePerformanceConflict(
	ctx context.Context,
	incoming Performance,
) (time.Time, error) {
	existing, err := r.Get(
		ctx,
		incoming.DeviceID,
		incoming.SampleID,
	)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"resolve performance conflict: %w",
			err,
		)
	}

	// Compare decoded messages instead of relying
	// on binary serialization order.
	if !proto.Equal(
		existing.Snapshot,
		incoming.Snapshot,
	) {
		return time.Time{}, ErrPerformanceConflict
	}

	return existing.ReceivedAt, nil
}
