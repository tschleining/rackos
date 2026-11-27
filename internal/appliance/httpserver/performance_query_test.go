package httpserver

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/performance"
)

const (
	testPerformanceHTTPDeviceID = "e2a104d8-9c72-4b31-8fa2-63001b24c9e8"

	testPerformanceHTTPOtherDeviceID = "22222222-2222-4222-8222-222222222222"

	testPerformanceHTTPSampleID = "0123456789abcdef0123456789abcdef"
)

func performanceHTTPMillisPtr(value int64) *int64 {
	return &value
}

func requireInvalidPerformanceHTTPQuery(
	t *testing.T,
	err error,
) {
	t.Helper()

	if !errors.Is(
		err,
		performance.ErrInvalidQuery,
	) {
		t.Fatalf(
			"expected invalid performance query, got %v",
			err,
		)
	}
}

func TestPerformanceHTTPDeviceID(t *testing.T) {
	if err := validatePerformanceDeviceID(
		testPerformanceHTTPDeviceID,
	); err != nil {
		t.Fatal(err)
	}

	for _, invalid := range []string{
		"",
		"invalid",
		" " + testPerformanceHTTPDeviceID,
		testPerformanceHTTPDeviceID + " ",
		strings.ToUpper(testPerformanceHTTPDeviceID),
		"e2a104d8-9c72-1b31-8fa2-63001b24c9e8",
		"e2a104d8-9c72-4b31-0fa2-63001b24c9e8",
	} {
		requireInvalidPerformanceHTTPQuery(
			t,
			validatePerformanceDeviceID(invalid),
		)
	}
}

func TestPerformanceHTTPLatestQuery(t *testing.T) {
	if err := validateLatestPerformanceQuery(
		"",
	); err != nil {
		t.Fatal(err)
	}

	for _, invalid := range []string{
		"limit=1",
		"from=2026-01-01T00:00:00Z",
		"cursor=example",
		"unknown=value",
		"%zz",
	} {
		requireInvalidPerformanceHTTPQuery(
			t,
			validateLatestPerformanceQuery(invalid),
		)
	}
}

func TestPerformanceHTTPTimestamp(t *testing.T) {
	expected := time.Date(
		2026, 1, 1,
		12, 0, 0,
		123000000,
		time.UTC,
	).UnixMilli()

	for _, value := range []string{
		"2026-01-01T12:00:00.123Z",
		"2026-01-01T12:00:00.123000000Z",
		"2026-01-01T07:00:00.123-05:00",
		"2026-01-01T14:00:00.123+02:00",
	} {
		actual, err := parsePerformanceTimestamp(
			value,
		)
		if err != nil {
			t.Fatal(err)
		}

		if actual == nil ||
			*actual != expected {
			t.Fatalf(
				"incorrect timestamp for %q",
				value,
			)
		}
	}
}

func TestPerformanceHTTPTimestampBoundaries(
	t *testing.T,
) {
	tests := []struct {
		value    string
		expected int64
	}{
		{
			"0001-01-01T00:00:00Z",
			-62135596800000,
		},
		{
			"1969-12-31T23:59:59.999Z",
			-1,
		},
		{
			"1970-01-01T00:00:00Z",
			0,
		},
		{
			"9999-12-31T23:59:59.999Z",
			253402300799999,
		},
	}

	for _, tt := range tests {
		actual, err := parsePerformanceTimestamp(
			tt.value,
		)
		if err != nil {
			t.Fatal(err)
		}

		if actual == nil ||
			*actual != tt.expected {
			t.Fatalf(
				"incorrect timestamp boundary for %q",
				tt.value,
			)
		}
	}
}

func TestPerformanceHTTPTimestampInvalid(
	t *testing.T,
) {
	for _, value := range []string{
		"",
		"invalid",
		"2026-01-01",
		"2026-01-01T12:00:00",
		"2026-01-01 12:00:00Z",
		"2026-01-01T12:00:00z",
		"2026-01-01T12:00:00.1234Z",
		"2026-01-01T12:00:00.000000001Z",
		"2026-01-01T12:00:00,123Z",
		"2026-01-01T12:00:00.1230000000Z",
		"2026-02-30T12:00:00Z",
		"2026-01-01T25:00:00Z",
		"2026-01-01T12:00:60Z",
		"2026-01-01T12:00:00+24:00",
		"2026-01-01T12:00:00+01:60",
		"0000-01-01T00:00:00Z",
		"10000-01-01T00:00:00Z",
		"0001-01-01T00:00:00+01:00",
		"9999-12-31T23:59:59-01:00",
	} {
		t.Run(value, func(t *testing.T) {
			actual, err := parsePerformanceTimestamp(
				value,
			)

			requireInvalidPerformanceHTTPQuery(
				t,
				err,
			)

			if actual != nil {
				t.Fatal("invalid timestamp returned a value")
			}
		})
	}
}

func TestPerformanceHTTPLimit(t *testing.T) {
	tests := []struct {
		value    string
		expected int
	}{
		{"1", 1},
		{"10", 10},
		{"25", 25},
	}

	for _, tt := range tests {
		actual, err := parsePerformanceLimit(
			tt.value,
		)
		if err != nil {
			t.Fatal(err)
		}

		if actual != tt.expected {
			t.Fatal("incorrect performance limit")
		}
	}
}

func TestPerformanceHTTPLimitInvalid(
	t *testing.T,
) {
	for _, value := range []string{
		"",
		"0",
		"-1",
		"+1",
		"26",
		"1.5",
		"1e1",
		" 10",
		"10 ",
		"999999999999999999999999999999999999",
	} {
		_, err := parsePerformanceLimit(value)

		requireInvalidPerformanceHTTPQuery(
			t,
			err,
		)
	}
}

func TestPerformanceHTTPHistoryDefaults(
	t *testing.T,
) {
	options, err := parsePerformanceHistoryQuery(
		testPerformanceHTTPDeviceID,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}

	if options.Limit != performance.DefaultHistoryLimit {
		t.Fatal("incorrect default history limit")
	}

	if options.FromMillis != nil ||
		options.ToMillis != nil ||
		options.After != nil {
		t.Fatal("default history query has unexpected bounds")
	}
}

func TestPerformanceHTTPHistoryRange(
	t *testing.T,
) {
	values := url.Values{
		"from": {
			"2026-01-01T12:00:00+02:00",
		},
		"to": {
			"2026-01-01T12:00:00Z",
		},
		"limit": {"25"},
	}

	options, err := parsePerformanceHistoryQuery(
		testPerformanceHTTPDeviceID,
		values.Encode(),
	)
	if err != nil {
		t.Fatal(err)
	}

	expectedFrom := time.Date(
		2026, 1, 1,
		10, 0, 0, 0,
		time.UTC,
	).UnixMilli()

	expectedTo := time.Date(
		2026, 1, 1,
		12, 0, 0, 0,
		time.UTC,
	).UnixMilli()

	if options.FromMillis == nil ||
		*options.FromMillis != expectedFrom {
		t.Fatal("incorrect lower bound")
	}

	if options.ToMillis == nil ||
		*options.ToMillis != expectedTo {
		t.Fatal("incorrect upper bound")
	}

	if options.Limit != 25 {
		t.Fatal("incorrect history limit")
	}
}

func TestPerformanceHTTPHistoryInvalid(
	t *testing.T,
) {
	tests := []string{
		"unknown=value",
		"Limit=10",
		"limit=0",
		"limit=26",
		"limit=",
		"limit=10&limit=20",
		"from=",
		"to=",
		"cursor=",
		"cursor=invalid",
		"cursor=a&cursor=b",
		"from=invalid",
		"to=invalid",
		"limit=%zz",
		"limit=10;unknown=value",
		"from=2026-01-01T00:00:00Z&" +
			"from=2026-01-02T00:00:00Z",
		"from=2026-01-02T00:00:00Z&" +
			"to=2026-01-01T00:00:00Z",
		"from=2026-01-01T00:00:00Z&" +
			"to=2026-01-01T00:00:00Z",
		strings.Repeat(
			"a",
			maxPerformanceQueryLength+1,
		),
	}

	for _, rawQuery := range tests {
		_, err := parsePerformanceHistoryQuery(
			testPerformanceHTTPDeviceID,
			rawQuery,
		)

		requireInvalidPerformanceHTTPQuery(
			t,
			err,
		)
	}

	_, err := parsePerformanceHistoryQuery(
		"invalid-device",
		"",
	)

	requireInvalidPerformanceHTTPQuery(
		t,
		err,
	)
}

func TestPerformanceHTTPCursorRoundTrip(
	t *testing.T,
) {
	options := performance.HistoryQuery{
		FromMillis: performanceHTTPMillisPtr(1000),
		ToMillis:   performanceHTTPMillisPtr(3000),
		Limit:      10,
	}

	cursor := &performance.HistoryCursor{
		CollectedAtMillis: 2000,
		SampleID:          testPerformanceHTTPSampleID,
	}

	encoded, err := encodePerformanceCursor(
		testPerformanceHTTPDeviceID,
		options,
		cursor,
	)
	if err != nil {
		t.Fatal(err)
	}

	if encoded == "" ||
		strings.ContainsAny(encoded, "=+/") {
		t.Fatal("cursor is not unpadded Base64URL")
	}

	actual, err := decodePerformanceCursor(
		testPerformanceHTTPDeviceID,
		options,
		encoded,
	)
	if err != nil {
		t.Fatal(err)
	}

	if actual == nil || *actual != *cursor {
		t.Fatal("cursor did not survive round trip")
	}

	// Changing the page size must not invalidate
	// an otherwise compatible continuation.
	options.Limit = 25

	actual, err = decodePerformanceCursor(
		testPerformanceHTTPDeviceID,
		options,
		encoded,
	)
	if err != nil {
		t.Fatal(err)
	}

	if actual == nil || *actual != *cursor {
		t.Fatal("page size change invalidated cursor")
	}
}

func TestPerformanceHTTPCursorQueryIntegration(
	t *testing.T,
) {
	options := performance.HistoryQuery{
		FromMillis: performanceHTTPMillisPtr(1000),
		ToMillis:   performanceHTTPMillisPtr(3000),
		Limit:      10,
	}

	cursor := &performance.HistoryCursor{
		CollectedAtMillis: 2000,
		SampleID:          testPerformanceHTTPSampleID,
	}

	encoded, err := encodePerformanceCursor(
		testPerformanceHTTPDeviceID,
		options,
		cursor,
	)
	if err != nil {
		t.Fatal(err)
	}

	values := url.Values{
		"from":   {"1970-01-01T00:00:01Z"},
		"to":     {"1970-01-01T00:00:03Z"},
		"limit":  {"5"},
		"cursor": {encoded},
	}

	actual, err := parsePerformanceHistoryQuery(
		testPerformanceHTTPDeviceID,
		values.Encode(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if actual.After == nil ||
		*actual.After != *cursor {
		t.Fatal("incorrect continuation position")
	}

	if actual.Limit != 5 {
		t.Fatal("incorrect continuation page limit")
	}

	// Omitting the original time bounds must
	// invalidate the bounded continuation token.
	values.Del("from")
	values.Del("to")

	_, err = parsePerformanceHistoryQuery(
		testPerformanceHTTPDeviceID,
		values.Encode(),
	)

	requireInvalidPerformanceHTTPQuery(
		t,
		err,
	)
}

func TestPerformanceHTTPCursorMismatch(
	t *testing.T,
) {
	options := performance.HistoryQuery{
		FromMillis: performanceHTTPMillisPtr(1000),
		ToMillis:   performanceHTTPMillisPtr(3000),
		Limit:      10,
	}

	cursor := &performance.HistoryCursor{
		CollectedAtMillis: 2000,
		SampleID:          testPerformanceHTTPSampleID,
	}

	encoded, err := encodePerformanceCursor(
		testPerformanceHTTPDeviceID,
		options,
		cursor,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = decodePerformanceCursor(
		testPerformanceHTTPOtherDeviceID,
		options,
		encoded,
	)

	requireInvalidPerformanceHTTPQuery(t, err)

	changed := options
	changed.FromMillis = performanceHTTPMillisPtr(
		1500,
	)

	_, err = decodePerformanceCursor(
		testPerformanceHTTPDeviceID,
		changed,
		encoded,
	)

	requireInvalidPerformanceHTTPQuery(t, err)

	changed = options
	changed.ToMillis = nil

	_, err = decodePerformanceCursor(
		testPerformanceHTTPDeviceID,
		changed,
		encoded,
	)

	requireInvalidPerformanceHTTPQuery(t, err)
}

func TestPerformanceHTTPCursorInvalidEncoding(
	t *testing.T,
) {
	options := performance.HistoryQuery{
		Limit: 10,
	}

	cursor := &performance.HistoryCursor{
		CollectedAtMillis: 2000,
		SampleID:          testPerformanceHTTPSampleID,
	}

	valid, err := encodePerformanceCursor(
		testPerformanceHTTPDeviceID,
		options,
		cursor,
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, encoded := range []string{
		"",
		"!",
		valid + "=",
		valid + "\n",
		strings.Repeat(
			"a",
			maxPerformanceCursorLength+1,
		),
		base64.RawURLEncoding.EncodeToString(
			[]byte("not JSON"),
		),
		base64.RawURLEncoding.EncodeToString(
			[]byte("{}"),
		),
		base64.RawURLEncoding.EncodeToString(
			[]byte("null"),
		),
	} {
		actual, err := decodePerformanceCursor(
			testPerformanceHTTPDeviceID,
			options,
			encoded,
		)

		requireInvalidPerformanceHTTPQuery(
			t,
			err,
		)

		if actual != nil {
			t.Fatal("invalid cursor returned a position")
		}
	}
}

func TestPerformanceHTTPCursorInvalidPayload(
	t *testing.T,
) {
	options := performance.HistoryQuery{
		FromMillis: performanceHTTPMillisPtr(1000),
		ToMillis:   performanceHTTPMillisPtr(3000),
		Limit:      10,
	}

	original := performanceCursorPayload{
		Version:  performanceCursorVersion,
		DeviceID: testPerformanceHTTPDeviceID,

		FromMillis: options.FromMillis,
		ToMillis:   options.ToMillis,

		CollectedAtMillis: 2000,
		SampleID:          testPerformanceHTTPSampleID,
	}

	tests := []struct {
		name   string
		change func(*performanceCursorPayload)
	}{
		{
			name: "unknown version",
			change: func(p *performanceCursorPayload) {
				p.Version = 99
			},
		},
		{
			name: "missing sample ID",
			change: func(p *performanceCursorPayload) {
				p.SampleID = ""
			},
		},
		{
			name: "invalid sample ID",
			change: func(p *performanceCursorPayload) {
				p.SampleID = "invalid"
			},
		},
		{
			name: "zero timestamp",
			change: func(p *performanceCursorPayload) {
				p.CollectedAtMillis = 0
			},
		},
		{
			name: "before lower bound",
			change: func(p *performanceCursorPayload) {
				p.CollectedAtMillis = 999
			},
		},
		{
			name: "at exclusive upper bound",
			change: func(p *performanceCursorPayload) {
				p.CollectedAtMillis = 3000
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := original

			tt.change(&payload)

			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}

			encoded := base64.RawURLEncoding.
				EncodeToString(data)

			_, err = decodePerformanceCursor(
				testPerformanceHTTPDeviceID,
				options,
				encoded,
			)

			requireInvalidPerformanceHTTPQuery(
				t,
				err,
			)
		})
	}
}

func TestPerformanceHTTPCursorNoncanonicalJSON(
	t *testing.T,
) {
	options := performance.HistoryQuery{
		Limit: 10,
	}

	cursor := &performance.HistoryCursor{
		CollectedAtMillis: 2000,
		SampleID:          testPerformanceHTTPSampleID,
	}

	valid, err := encodePerformanceCursor(
		testPerformanceHTTPDeviceID,
		options,
		cursor,
	)
	if err != nil {
		t.Fatal(err)
	}

	data, err := base64.RawURLEncoding.
		DecodeString(valid)
	if err != nil {
		t.Fatal(err)
	}

	original := string(data)

	tests := []string{
		" " + original,
		original + "\n",

		// Unknown field.
		strings.TrimSuffix(original, "}") +
			`,"extra":true}`,

		// Duplicate field.
		strings.TrimSuffix(original, "}") +
			`,"version":1}`,

		// Missing field.
		strings.Replace(
			original,
			`"from_millis":null,`,
			"",
			1,
		),

		// Trailing JSON object.
		original + "{}",
	}

	for _, value := range tests {
		encoded := base64.RawURLEncoding.
			EncodeToString([]byte(value))

		_, err := decodePerformanceCursor(
			testPerformanceHTTPDeviceID,
			options,
			encoded,
		)

		requireInvalidPerformanceHTTPQuery(
			t,
			err,
		)
	}
}

func TestPerformanceHTTPCursorInvalidEncode(
	t *testing.T,
) {
	options := performance.HistoryQuery{
		Limit: 10,
	}

	for _, cursor := range []*performance.HistoryCursor{
		nil,
		{},
		{
			CollectedAtMillis: 2000,
			SampleID:          "invalid",
		},
		{
			CollectedAtMillis: 0,
			SampleID:          testPerformanceHTTPSampleID,
		},
	} {
		encoded, err := encodePerformanceCursor(
			testPerformanceHTTPDeviceID,
			options,
			cursor,
		)

		requireInvalidPerformanceHTTPQuery(
			t,
			err,
		)

		if encoded != "" {
			t.Fatal("invalid cursor was encoded")
		}
	}
}
