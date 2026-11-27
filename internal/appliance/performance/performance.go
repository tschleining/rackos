package performance

import (
	"errors"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
)

const CurrentPayloadVersion = 1

const (
	DefaultHistoryLimit = 10
	MaxHistoryLimit     = 25
)

var (
	ErrNotFound = errors.New(
		"device performance sample not found",
	)

	ErrPerformanceConflict = errors.New(
		"performance sample conflicts with stored sample",
	)

	ErrUnsupportedPayloadVersion = errors.New(
		"unsupported performance payload version",
	)

	ErrCorruptPerformance = errors.New(
		"invalid stored performance record",
	)

	ErrInvalidQuery = errors.New(
		"invalid performance query",
	)
)

// Performance represents a historical performance
// snapshot stored for a registered device.
//
// CollectedAt preserves the original snapshot timestamp.
// ReceivedAt uses the appliance's millisecond precision.
// Snapshot contains the decoded Protobuf payload.
type Performance struct {
	DeviceID string
	SampleID string

	CollectedAt time.Time
	ReceivedAt  time.Time

	PayloadVersion int

	Snapshot *rackosv1.PerformanceSnapshot
}

// HistoryCursor identifies a position in the historical
// performance ordering.
//
// CollectedAtMillis uses the indexed SQLite timestamp
// rather than the higher-precision payload timestamp.
//
// SampleID provides deterministic ordering when multiple
// samples share the same indexed collection timestamp.
type HistoryCursor struct {
	CollectedAtMillis int64
	SampleID          string
}

// HistoryQuery describes a bounded historical
// performance retrieval operation.
//
// FromMillis is inclusive.
// ToMillis is exclusive.
//
// Nil time bounds represent unbounded time ranges.
//
// Limit must be between 1 and MaxHistoryLimit.
//
// After excludes the cursor position and returns
// records appearing after it in descending order.
type HistoryQuery struct {
	FromMillis *int64
	ToMillis   *int64

	Limit int
	After *HistoryCursor
}

// HistoryPage contains historical performance records
// ordered by collection timestamp and sample identity,
// both descending.
//
// Samples is an empty, non-nil slice when no records
// match the query.
//
// NextCursor is nil when no additional page exists.
type HistoryPage struct {
	Samples    []Performance
	NextCursor *HistoryCursor
}

// HasMore reports whether another historical page
// is available.
func (p HistoryPage) HasMore() bool {
	return p.NextCursor != nil
}
