package httpserver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/tschleining/rackos/internal/appliance/performance"
)

const (
	performanceCursorVersion = 1

	maxPerformanceQueryLength  = 4096
	maxPerformanceCursorLength = 1024
)

var (
	performanceDeviceIDPattern = regexp.MustCompile(
		`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
	)

	performanceTimestampPattern = regexp.MustCompile(
		`^[0-9]{4}-[0-9]{2}-[0-9]{2}T` +
			`[0-9]{2}:[0-9]{2}:[0-9]{2}` +
			`(\.[0-9]{1,9})?` +
			`(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`,
	)

	performanceLimitPattern = regexp.MustCompile(
		`^[0-9]+$`,
	)
)

// performanceCursorPayload is the versioned wire
// representation of an HTTP continuation cursor.
//
// Cursors are continuation positions, not credentials.
// The device and time bounds prevent accidental reuse
// with a different historical query.
type performanceCursorPayload struct {
	Version  int    `json:"version"`
	DeviceID string `json:"device_id"`

	FromMillis *int64 `json:"from_millis"`
	ToMillis   *int64 `json:"to_millis"`

	CollectedAtMillis int64  `json:"collected_at_millis"`
	SampleID          string `json:"sample_id"`
}

func invalidPerformanceQuery(message string) error {
	return fmt.Errorf(
		"%w: %s",
		performance.ErrInvalidQuery,
		message,
	)
}

func validatePerformanceDeviceID(deviceID string) error {
	if !performanceDeviceIDPattern.MatchString(
		deviceID,
	) {
		return invalidPerformanceQuery(
			"invalid device ID",
		)
	}

	return nil
}

// validateLatestPerformanceQuery rejects parameters
// because the latest endpoint does not support
// filtering or pagination.
func validateLatestPerformanceQuery(
	rawQuery string,
) error {
	if rawQuery != "" {
		return invalidPerformanceQuery(
			"latest performance does not accept query parameters",
		)
	}

	return nil
}

// parsePerformanceTimestamp converts an RFC 3339
// timestamp into an indexed Unix millisecond value.
//
// Sub-millisecond precision is rejected rather
// than silently changing the requested time range.
func parsePerformanceTimestamp(
	value string,
) (*int64, error) {
	if !performanceTimestampPattern.MatchString(
		value,
	) {
		return nil, invalidPerformanceQuery(
			"invalid performance timestamp",
		)
	}

	parsed, err := time.Parse(
		time.RFC3339Nano,
		value,
	)
	if err != nil {
		return nil, invalidPerformanceQuery(
			"invalid performance timestamp",
		)
	}

	if parsed.Year() < 1 ||
		parsed.Year() > 9999 {
		return nil, invalidPerformanceQuery(
			"timestamp outside supported range",
		)
	}

	parsed = parsed.UTC()

	if parsed.Year() < 1 ||
		parsed.Year() > 9999 {
		return nil, invalidPerformanceQuery(
			"timestamp outside supported range",
		)
	}

	if parsed.Nanosecond()%int(time.Millisecond) != 0 {
		return nil, invalidPerformanceQuery(
			"timestamp must use millisecond precision",
		)
	}

	millis := parsed.UnixMilli()

	return &millis, nil
}

func parsePerformanceLimit(
	value string,
) (int, error) {
	if !performanceLimitPattern.MatchString(value) {
		return 0, invalidPerformanceQuery(
			"invalid performance limit",
		)
	}

	limit, err := strconv.Atoi(value)
	if err != nil ||
		limit < 1 ||
		limit > performance.MaxHistoryLimit {
		return 0, invalidPerformanceQuery(
			"invalid performance limit",
		)
	}

	return limit, nil
}

// parsePerformanceHistoryQuery validates the HTTP
// query and converts it into a repository query.
//
// ParseQuery is used directly so malformed URL
// encoding cannot be silently ignored.
func parsePerformanceHistoryQuery(
	deviceID string,
	rawQuery string,
) (performance.HistoryQuery, error) {
	if err := validatePerformanceDeviceID(
		deviceID,
	); err != nil {
		return performance.HistoryQuery{}, err
	}

	if len(rawQuery) > maxPerformanceQueryLength {
		return performance.HistoryQuery{},
			invalidPerformanceQuery(
				"performance query is too large",
			)
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return performance.HistoryQuery{},
			invalidPerformanceQuery(
				"malformed query parameters",
			)
	}

	for key, entries := range values {
		switch key {
		case "from", "to", "limit", "cursor":
		default:
			return performance.HistoryQuery{},
				invalidPerformanceQuery(
					"unknown performance query parameter",
				)
		}

		if len(entries) != 1 {
			return performance.HistoryQuery{},
				invalidPerformanceQuery(
					"duplicate performance query parameter",
				)
		}

		if entries[0] == "" {
			return performance.HistoryQuery{},
				invalidPerformanceQuery(
					"empty performance query parameter",
				)
		}
	}

	options := performance.HistoryQuery{
		Limit: performance.DefaultHistoryLimit,
	}

	if values.Has("from") {
		options.FromMillis, err =
			parsePerformanceTimestamp(
				values.Get("from"),
			)
		if err != nil {
			return performance.HistoryQuery{}, err
		}
	}

	if values.Has("to") {
		options.ToMillis, err =
			parsePerformanceTimestamp(
				values.Get("to"),
			)
		if err != nil {
			return performance.HistoryQuery{}, err
		}
	}

	if values.Has("limit") {
		options.Limit, err = parsePerformanceLimit(
			values.Get("limit"),
		)
		if err != nil {
			return performance.HistoryQuery{}, err
		}
	}

	// Validate time bounds before processing a
	// continuation cursor.
	if err := options.Validate(); err != nil {
		return performance.HistoryQuery{}, err
	}

	if values.Has("cursor") {
		options.After, err = decodePerformanceCursor(
			deviceID,
			options,
			values.Get("cursor"),
		)
		if err != nil {
			return performance.HistoryQuery{}, err
		}
	}

	if err := options.Validate(); err != nil {
		return performance.HistoryQuery{}, err
	}

	return options, nil
}

// encodePerformanceCursor converts a repository
// continuation position into a versioned Base64URL
// token bound to the original device and time range.
func encodePerformanceCursor(
	deviceID string,
	options performance.HistoryQuery,
	cursor *performance.HistoryCursor,
) (string, error) {
	if err := validatePerformanceDeviceID(
		deviceID,
	); err != nil {
		return "", err
	}

	if cursor == nil {
		return "", invalidPerformanceQuery(
			"missing performance cursor",
		)
	}

	validation := options
	validation.After = cursor

	if err := validation.Validate(); err != nil {
		return "", err
	}

	payload := performanceCursorPayload{
		Version:  performanceCursorVersion,
		DeviceID: deviceID,

		FromMillis: options.FromMillis,
		ToMillis:   options.ToMillis,

		CollectedAtMillis: cursor.CollectedAtMillis,
		SampleID:          cursor.SampleID,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf(
			"encode performance cursor: %w",
			err,
		)
	}

	encoded := base64.RawURLEncoding.EncodeToString(
		data,
	)

	if len(encoded) > maxPerformanceCursorLength {
		return "", invalidPerformanceQuery(
			"performance cursor is too large",
		)
	}

	return encoded, nil
}

// decodePerformanceCursor validates the cursor
// encoding, version, device identity, time bounds,
// and repository continuation position.
func decodePerformanceCursor(
	deviceID string,
	options performance.HistoryQuery,
	encoded string,
) (*performance.HistoryCursor, error) {
	if err := validatePerformanceDeviceID(
		deviceID,
	); err != nil {
		return nil, err
	}

	if len(encoded) == 0 ||
		len(encoded) > maxPerformanceCursorLength {
		return nil, invalidPerformanceQuery(
			"invalid performance cursor size",
		)
	}

	data, err := base64.RawURLEncoding.
		Strict().DecodeString(encoded)

	if err != nil {
		return nil, invalidPerformanceQuery(
			"invalid performance cursor encoding",
		)
	}

	// Reject alternative encodings, including
	// embedded newline characters.
	if base64.RawURLEncoding.EncodeToString(data) !=
		encoded {
		return nil, invalidPerformanceQuery(
			"noncanonical performance cursor encoding",
		)
	}

	var payload performanceCursorPayload

	if err := json.Unmarshal(
		data,
		&payload,
	); err != nil {
		return nil, invalidPerformanceQuery(
			"invalid performance cursor payload",
		)
	}

	// Require the exact canonical representation
	// generated by the cursor encoder.
	//
	// This rejects unknown fields, duplicate fields,
	// missing fields, and alternate representations.
	canonical, err := json.Marshal(payload)
	if err != nil ||
		!bytes.Equal(canonical, data) {
		return nil, invalidPerformanceQuery(
			"noncanonical performance cursor payload",
		)
	}

	if payload.Version != performanceCursorVersion {
		return nil, invalidPerformanceQuery(
			"unsupported performance cursor version",
		)
	}

	if payload.DeviceID != deviceID {
		return nil, invalidPerformanceQuery(
			"performance cursor device mismatch",
		)
	}

	if !equalPerformanceTimeBound(
		payload.FromMillis,
		options.FromMillis,
	) ||
		!equalPerformanceTimeBound(
			payload.ToMillis,
			options.ToMillis,
		) {
		return nil, invalidPerformanceQuery(
			"performance cursor time range mismatch",
		)
	}

	cursor := &performance.HistoryCursor{
		CollectedAtMillis: payload.CollectedAtMillis,
		SampleID:          payload.SampleID,
	}

	validation := options
	validation.After = cursor

	if err := validation.Validate(); err != nil {
		return nil, err
	}

	return cursor, nil
}

func equalPerformanceTimeBound(
	first *int64,
	second *int64,
) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}

	return *first == *second
}
