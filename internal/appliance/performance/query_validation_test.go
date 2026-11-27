package performance

import (
	"errors"
	"strings"
	"testing"
)

const testHistoryCursorSampleID = "0123456789abcdef0123456789abcdef"

func historyMillisPtr(value int64) *int64 {
	return &value
}

func testHistoryCursor(value int64) *HistoryCursor {
	return &HistoryCursor{
		CollectedAtMillis: value,
		SampleID:          testHistoryCursorSampleID,
	}
}

func TestHistoryQueryValid(t *testing.T) {
	tests := []struct {
		name  string
		query HistoryQuery
	}{
		{
			name: "unbounded",
			query: HistoryQuery{
				Limit: DefaultHistoryLimit,
			},
		},
		{
			name: "minimum limit",
			query: HistoryQuery{
				Limit: 1,
			},
		},
		{
			name: "maximum limit",
			query: HistoryQuery{
				Limit: MaxHistoryLimit,
			},
		},
		{
			name: "lower bound only",
			query: HistoryQuery{
				FromMillis: historyMillisPtr(1000),
				Limit:      10,
			},
		},
		{
			name: "upper bound only",
			query: HistoryQuery{
				ToMillis: historyMillisPtr(2000),
				Limit:    10,
			},
		},
		{
			name: "bounded range",
			query: HistoryQuery{
				FromMillis: historyMillisPtr(1000),
				ToMillis:   historyMillisPtr(2000),
				Limit:      10,
			},
		},
		{
			name: "cursor without bounds",
			query: HistoryQuery{
				Limit: 10,
				After: testHistoryCursor(1500),
			},
		},
		{
			name: "cursor inside range",
			query: HistoryQuery{
				FromMillis: historyMillisPtr(1000),
				ToMillis:   historyMillisPtr(2000),
				Limit:      10,
				After:      testHistoryCursor(1500),
			},
		},
		{
			name: "cursor at inclusive lower bound",
			query: HistoryQuery{
				FromMillis: historyMillisPtr(1000),
				ToMillis:   historyMillisPtr(2000),
				Limit:      10,
				After:      testHistoryCursor(1000),
			},
		},
		{
			name: "cursor before exclusive upper bound",
			query: HistoryQuery{
				FromMillis: historyMillisPtr(1000),
				ToMillis:   historyMillisPtr(2000),
				Limit:      10,
				After:      testHistoryCursor(1999),
			},
		},
		{
			name: "epoch lower bound",
			query: HistoryQuery{
				FromMillis: historyMillisPtr(0),
				Limit:      10,
			},
		},
		{
			name: "pre epoch lower bound",
			query: HistoryQuery{
				FromMillis: historyMillisPtr(-1000),
				Limit:      10,
			},
		},
		{
			name: "supported timestamp boundaries",
			query: HistoryQuery{
				FromMillis: historyMillisPtr(
					minQueryTimestampMillis,
				),
				ToMillis: historyMillisPtr(
					maxQueryTimestampMillis,
				),
				Limit: 10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.query.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHistoryQueryInvalid(t *testing.T) {
	tests := []struct {
		name  string
		query HistoryQuery
	}{
		{
			name:  "zero limit",
			query: HistoryQuery{},
		},
		{
			name: "negative limit",
			query: HistoryQuery{
				Limit: -1,
			},
		},
		{
			name: "oversized limit",
			query: HistoryQuery{
				Limit: MaxHistoryLimit + 1,
			},
		},
		{
			name: "equal time bounds",
			query: HistoryQuery{
				FromMillis: historyMillisPtr(1000),
				ToMillis:   historyMillisPtr(1000),
				Limit:      10,
			},
		},
		{
			name: "reversed time bounds",
			query: HistoryQuery{
				FromMillis: historyMillisPtr(2000),
				ToMillis:   historyMillisPtr(1000),
				Limit:      10,
			},
		},
		{
			name: "invalid lower timestamp",
			query: HistoryQuery{
				FromMillis: historyMillisPtr(
					minQueryTimestampMillis - 1,
				),
				Limit: 10,
			},
		},
		{
			name: "invalid upper timestamp",
			query: HistoryQuery{
				ToMillis: historyMillisPtr(
					maxQueryTimestampMillis + 1,
				),
				Limit: 10,
			},
		},
		{
			name: "zero cursor timestamp",
			query: HistoryQuery{
				Limit: 10,
				After: testHistoryCursor(0),
			},
		},
		{
			name: "negative cursor timestamp",
			query: HistoryQuery{
				Limit: 10,
				After: testHistoryCursor(-1),
			},
		},
		{
			name: "cursor timestamp overflow",
			query: HistoryQuery{
				Limit: 10,
				After: testHistoryCursor(
					maxQueryTimestampMillis + 1,
				),
			},
		},
		{
			name: "missing cursor sample ID",
			query: HistoryQuery{
				Limit: 10,
				After: &HistoryCursor{
					CollectedAtMillis: 1500,
				},
			},
		},
		{
			name: "short cursor sample ID",
			query: HistoryQuery{
				Limit: 10,
				After: &HistoryCursor{
					CollectedAtMillis: 1500,
					SampleID:          "abc",
				},
			},
		},
		{
			name: "uppercase cursor sample ID",
			query: HistoryQuery{
				Limit: 10,
				After: &HistoryCursor{
					CollectedAtMillis: 1500,
					SampleID: strings.ToUpper(
						testHistoryCursorSampleID,
					),
				},
			},
		},
		{
			name: "nonhexadecimal cursor sample ID",
			query: HistoryQuery{
				Limit: 10,
				After: &HistoryCursor{
					CollectedAtMillis: 1500,
					SampleID: strings.Repeat(
						"g",
						32,
					),
				},
			},
		},
		{
			name: "cursor before lower bound",
			query: HistoryQuery{
				FromMillis: historyMillisPtr(1000),
				Limit:      10,
				After:      testHistoryCursor(999),
			},
		},
		{
			name: "cursor at exclusive upper bound",
			query: HistoryQuery{
				ToMillis: historyMillisPtr(2000),
				Limit:    10,
				After:    testHistoryCursor(2000),
			},
		},
		{
			name: "cursor after upper bound",
			query: HistoryQuery{
				ToMillis: historyMillisPtr(2000),
				Limit:    10,
				After:    testHistoryCursor(2001),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.query.Validate()

			if !errors.Is(err, ErrInvalidQuery) {
				t.Fatalf(
					"expected invalid query error, got %v",
					err,
				)
			}
		})
	}
}

func TestHistoryQueryDeviceID(t *testing.T) {
	const validID = "e2a104d8-9c72-4b31-8fa2-63001b24c9e8"

	if err := validateQueryDeviceID(validID); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		id   string
	}{
		{
			name: "empty identity",
			id:   "",
		},
		{
			name: "leading whitespace",
			id:   " " + validID,
		},
		{
			name: "trailing whitespace",
			id:   validID + " ",
		},
		{
			name: "control character",
			id:   validID + "\ninvalid",
		},
		{
			name: "invalid UTF-8",
			id:   string([]byte{0xff}),
		},
		{
			name: "oversized identity",
			id:   strings.Repeat("a", 1025),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateQueryDeviceID(tt.id)

			if !errors.Is(err, ErrInvalidQuery) {
				t.Fatalf(
					"expected invalid query error, got %v",
					err,
				)
			}
		})
	}
}

func TestHistoryPageHasMore(t *testing.T) {
	page := HistoryPage{
		Samples: make([]Performance, 0),
	}

	if page.HasMore() {
		t.Fatal("empty page has continuation")
	}

	page.NextCursor = testHistoryCursor(1000)

	if !page.HasMore() {
		t.Fatal("continuation was not recognized")
	}

	page.NextCursor = nil

	if page.HasMore() {
		t.Fatal("final page has continuation")
	}
}

func TestHistoryQueryLimits(t *testing.T) {
	if DefaultHistoryLimit != 10 {
		t.Fatal("unexpected default history limit")
	}

	if MaxHistoryLimit != 25 {
		t.Fatal("unexpected maximum history limit")
	}

	if DefaultHistoryLimit > MaxHistoryLimit {
		t.Fatal("default limit exceeds maximum limit")
	}
}
