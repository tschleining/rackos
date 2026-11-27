package performance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// executor allows the repository to operate using
// either a database connection or an existing
// transaction.
type executor interface {
	ExecContext(
		ctx context.Context,
		query string,
		args ...any,
	) (sql.Result, error)

	QueryRowContext(
		ctx context.Context,
		query string,
		args ...any,
	) *sql.Row
}

// Repository manages historical performance snapshots.
type Repository struct {
	db executor
}

// NewRepository creates a database-backed performance
// repository.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db: db,
	}
}

// WithTx creates a repository bound to an existing
// database transaction.
//
// The caller remains responsible for committing or
// rolling back the transaction.
func (r *Repository) WithTx(tx *sql.Tx) *Repository {
	return &Repository{
		db: tx,
	}
}

const performanceColumns = `
	device_id,
	sample_id,
	collected_at,
	received_at,
	payload_version,
	payload
`

// Get retrieves an individual historical performance
// snapshot using its device and sample identities.
//
// ErrNotFound is returned when the record does not
// exist.
func (r *Repository) Get(
	ctx context.Context,
	deviceID string,
	sampleID string,
) (Performance, error) {
	if err := ctx.Err(); err != nil {
		return Performance{}, err
	}

	if err := validateText(
		"device_id",
		deviceID,
		true,
		false,
	); err != nil {
		return Performance{}, err
	}

	if err := ValidateSampleID(sampleID); err != nil {
		return Performance{}, err
	}

	query := `
		SELECT ` + performanceColumns + `
		FROM device_performance
		WHERE device_id = ?
			AND sample_id = ?
	`

	record, err := scanPerformance(
		r.db.QueryRowContext(
			ctx,
			query,
			deviceID,
			sampleID,
		),
	)

	if errors.Is(err, sql.ErrNoRows) {
		return Performance{}, ErrNotFound
	}

	if err != nil {
		return Performance{}, fmt.Errorf(
			"get device performance: %w",
			err,
		)
	}

	return record, nil
}

// scanner allows stored records to be decoded from
// individual SQL rows and future result sets.
type scanner interface {
	Scan(dest ...any) error
}

// scanPerformance decodes and checks a stored
// historical performance record.
func scanPerformance(row scanner) (Performance, error) {
	var record Performance

	var (
		collectedAt int64
		receivedAt  int64
		payload     []byte
	)

	err := row.Scan(
		&record.DeviceID,
		&record.SampleID,
		&collectedAt,
		&receivedAt,
		&record.PayloadVersion,
		&payload,
	)
	if err != nil {
		return Performance{}, err
	}

	if record.PayloadVersion != CurrentPayloadVersion {
		return Performance{}, fmt.Errorf(
			"%w: %d",
			ErrUnsupportedPayloadVersion,
			record.PayloadVersion,
		)
	}

	if collectedAt <= 0 || receivedAt <= 0 {
		return Performance{}, corrupt(
			"invalid stored timestamps",
		)
	}

	if len(payload) == 0 || len(payload) > MaxRequestBytes {
		return Performance{}, corrupt(
			"invalid stored payload size",
		)
	}

	record.ReceivedAt = time.UnixMilli(
		receivedAt,
	).UTC()

	if err := timestamppb.New(
		record.ReceivedAt,
	).CheckValid(); err != nil {
		return Performance{}, corrupt(
			"receipt timestamp is outside supported range",
		)
	}

	snapshot := &rackosv1.PerformanceSnapshot{}

	if err := proto.Unmarshal(
		payload,
		snapshot,
	); err != nil {
		return Performance{}, corrupt(
			"cannot decode performance payload",
		)
	}

	// Receipt timestamps are stored at millisecond
	// precision. Account for the discarded fractional
	// millisecond when checking future clock skew.
	validationTime := record.ReceivedAt.Add(
		time.Millisecond - time.Nanosecond,
	)

	if err := Validate(
		snapshot,
		validationTime,
	); err != nil {
		return Performance{}, corrupt(
			"stored snapshot failed validation",
		)
	}

	record.CollectedAt = snapshot.CollectedAt.AsTime()

	// The indexed timestamp must agree with the
	// timestamp contained in the original payload.
	if record.CollectedAt.UnixMilli() != collectedAt {
		return Performance{}, corrupt(
			"collection timestamp does not match payload",
		)
	}

	record.Snapshot = snapshot

	return record, nil
}

func corrupt(message string) error {
	return fmt.Errorf(
		"%w: %s",
		ErrCorruptPerformance,
		message,
	)
}
