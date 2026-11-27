package performance

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// StorageStatistics describes the logical historical
// performance data currently stored by the appliance.
//
// PayloadBytes excludes database indexes, metadata,
// unused pages, and SQLite journal files.
//
// Timestamp ranges use the indexed millisecond
// timestamps stored in SQLite.
//
// Empty storage has zero counts and nil timestamps.
type StorageStatistics struct {
	TotalSamples     int64
	ReportingDevices int64
	PayloadBytes     int64

	OldestCollectedAt *time.Time
	LatestCollectedAt *time.Time

	OldestReceivedAt *time.Time
	LatestReceivedAt *time.Time
}

const storageStatisticsSQL = `
	SELECT
		COUNT(*),
		COUNT(DISTINCT device_id),
		COALESCE(SUM(length(payload)), 0),
		MIN(collected_at),
		MAX(collected_at),
		MIN(received_at),
		MAX(received_at)
	FROM device_performance
`

// Statistics retrieves aggregate historical
// performance storage information.
//
// All aggregates are calculated within a single
// SQL statement, providing a consistent view of
// the stored records for that statement.
//
// Individual performance payloads are not decoded.
func (r *Repository) Statistics(
	ctx context.Context,
) (StorageStatistics, error) {
	if err := ctx.Err(); err != nil {
		return StorageStatistics{}, err
	}

	var statistics StorageStatistics

	var (
		oldestCollectedAt sql.NullInt64
		latestCollectedAt sql.NullInt64
		oldestReceivedAt  sql.NullInt64
		latestReceivedAt  sql.NullInt64
	)

	err := r.db.QueryRowContext(
		ctx,
		storageStatisticsSQL,
	).Scan(
		&statistics.TotalSamples,
		&statistics.ReportingDevices,
		&statistics.PayloadBytes,
		&oldestCollectedAt,
		&latestCollectedAt,
		&oldestReceivedAt,
		&latestReceivedAt,
	)
	if err != nil {
		return StorageStatistics{}, fmt.Errorf(
			"query performance storage statistics: %w",
			err,
		)
	}

	if err := ctx.Err(); err != nil {
		return StorageStatistics{}, err
	}

	timestamps := []struct {
		name   string
		value  sql.NullInt64
		target **time.Time
	}{
		{
			"oldest collection timestamp",
			oldestCollectedAt,
			&statistics.OldestCollectedAt,
		},
		{
			"latest collection timestamp",
			latestCollectedAt,
			&statistics.LatestCollectedAt,
		},
		{
			"oldest receipt timestamp",
			oldestReceivedAt,
			&statistics.OldestReceivedAt,
		},
		{
			"latest receipt timestamp",
			latestReceivedAt,
			&statistics.LatestReceivedAt,
		},
	}

	for _, timestamp := range timestamps {
		if statistics.TotalSamples == 0 {
			if timestamp.value.Valid {
				return StorageStatistics{}, corrupt(
					"unexpected timestamp in empty storage",
				)
			}

			continue
		}

		value, err := storageStatisticsTimestamp(
			timestamp.value,
			timestamp.name,
		)
		if err != nil {
			return StorageStatistics{}, err
		}

		*timestamp.target = value
	}

	return statistics, nil
}

// storageStatisticsTimestamp converts an indexed
// timestamp to its UTC representation.
func storageStatisticsTimestamp(
	value sql.NullInt64,
	name string,
) (*time.Time, error) {
	if !value.Valid || value.Int64 <= 0 {
		return nil, corrupt(
			"invalid storage statistics " + name,
		)
	}

	timestamp := time.UnixMilli(
		value.Int64,
	).UTC()

	if timestamp.Year() < 1970 ||
		timestamp.Year() > 9999 {
		return nil, corrupt(
			"storage statistics " + name +
				" is outside supported range",
		)
	}

	return &timestamp, nil
}
