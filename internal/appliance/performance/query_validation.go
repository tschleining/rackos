package performance

import "fmt"

const (
	// Supported timestamp bounds correspond to
	// the Protobuf timestamp range: years 1-9999.
	minQueryTimestampMillis int64 = -62135596800000
	maxQueryTimestampMillis int64 = 253402300799999
)

// validateQueryDeviceID applies the existing
// repository device identity validation rules.
func validateQueryDeviceID(deviceID string) error {
	if err := validateText(
		"device_id",
		deviceID,
		true,
		false,
	); err != nil {
		return invalidQuery("invalid device ID")
	}

	return nil
}

// Validate checks the historical query contract.
//
// Validation is performed independently of HTTP
// request parsing so repository callers cannot
// bypass query constraints.
func (q HistoryQuery) Validate() error {
	if q.Limit < 1 || q.Limit > MaxHistoryLimit {
		return invalidQuery(
			"limit must be between 1 and %d",
			MaxHistoryLimit,
		)
	}

	if q.FromMillis != nil &&
		!validQueryTimestamp(*q.FromMillis) {
		return invalidQuery(
			"invalid lower time bound",
		)
	}

	if q.ToMillis != nil &&
		!validQueryTimestamp(*q.ToMillis) {
		return invalidQuery(
			"invalid upper time bound",
		)
	}

	if q.FromMillis != nil &&
		q.ToMillis != nil &&
		*q.FromMillis >= *q.ToMillis {
		return invalidQuery(
			"lower time bound must precede upper time bound",
		)
	}

	if q.After == nil {
		return nil
	}

	return q.validateCursor()
}

// validateCursor ensures that the continuation
// position is valid and belongs to the requested
// time range.
func (q HistoryQuery) validateCursor() error {
	cursor := q.After

	if cursor.CollectedAtMillis <= 0 ||
		!validQueryTimestamp(cursor.CollectedAtMillis) {
		return invalidQuery(
			"invalid cursor timestamp",
		)
	}

	if err := ValidateSampleID(
		cursor.SampleID,
	); err != nil {
		return invalidQuery(
			"invalid cursor sample ID",
		)
	}

	if q.FromMillis != nil &&
		cursor.CollectedAtMillis < *q.FromMillis {
		return invalidQuery(
			"cursor precedes lower time bound",
		)
	}

	if q.ToMillis != nil &&
		cursor.CollectedAtMillis >= *q.ToMillis {
		return invalidQuery(
			"cursor exceeds requested time range",
		)
	}

	return nil
}

func validQueryTimestamp(value int64) bool {
	return value >= minQueryTimestampMillis &&
		value <= maxQueryTimestampMillis
}

func invalidQuery(format string, args ...any) error {
	return fmt.Errorf(
		"%w: %s",
		ErrInvalidQuery,
		fmt.Sprintf(format, args...),
	)
}
