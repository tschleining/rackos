package performance

import (
	"context"
	"strings"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// testQueryPerformanceRequest constructs a valid
// performance report with a controlled device,
// sample identity and collection timestamp.
func testQueryPerformanceRequest(
	deviceID string,
	sampleID string,
	collectedAt time.Time,
) *rackosv1.ReportPerformanceRequest {
	request := testPerformanceRequest(
		collectedAt.Add(time.Second),
	)

	request.DeviceId = deviceID
	request.SampleId = sampleID

	request.Snapshot.CollectedAt = timestamppb.New(
		collectedAt,
	)

	return request
}

// requireQueryPerformanceRecord verifies that a
// retrieved record matches its original report.
func requireQueryPerformanceRecord(
	t *testing.T,
	actual Performance,
	expected *rackosv1.ReportPerformanceRequest,
	receivedAt time.Time,
) {
	t.Helper()

	if actual.DeviceID != expected.DeviceId {
		t.Fatal("incorrect device identity")
	}

	if actual.SampleID != expected.SampleId {
		t.Fatalf(
			"expected sample %s, got %s",
			expected.SampleId,
			actual.SampleID,
		)
	}

	if actual.PayloadVersion != CurrentPayloadVersion {
		t.Fatal("incorrect payload version")
	}

	if !actual.CollectedAt.Equal(
		expected.Snapshot.CollectedAt.AsTime(),
	) {
		t.Fatal("incorrect collection timestamp")
	}

	expectedReceipt := time.UnixMilli(
		receivedAt.UnixMilli(),
	).UTC()

	if !actual.ReceivedAt.Equal(expectedReceipt) {
		t.Fatal("incorrect receipt timestamp")
	}

	if actual.CollectedAt.Location() != time.UTC ||
		actual.ReceivedAt.Location() != time.UTC {
		t.Fatal("timestamps are not UTC")
	}

	if !proto.Equal(
		actual.Snapshot,
		expected.Snapshot,
	) {
		t.Fatal("retrieved snapshot differs from original")
	}
}

func TestPerformanceRepositoryLatestSingleSample(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	request := testPerformanceRequest(now)

	receipt := requirePerformanceInsert(
		t,
		ctx,
		repository,
		request,
		now,
	)

	actual, err := repository.Latest(
		ctx,
		request.DeviceId,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireQueryPerformanceRecord(
		t,
		actual,
		request,
		receipt,
	)

	requirePerformanceRowCount(t, ctx, db, 1)
}

func TestPerformanceRepositoryLatestCollectionOrder(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	first := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("a", 32),
		now.Add(-3*time.Minute),
	)

	newest := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("b", 32),
		now.Add(-time.Minute),
	)

	delayed := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("c", 32),
		now.Add(-2*time.Minute),
	)

	requirePerformanceInsert(
		t, ctx, repository, first, now,
	)

	newestReceipt := requirePerformanceInsert(
		t,
		ctx,
		repository,
		newest,
		now.Add(time.Second),
	)

	// The delayed report arrives last but has
	// an older collection timestamp.
	requirePerformanceInsert(
		t,
		ctx,
		repository,
		delayed,
		now.Add(2*time.Second),
	)

	actual, err := repository.Latest(
		ctx,
		repositoryDeviceID,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireQueryPerformanceRecord(
		t,
		actual,
		newest,
		newestReceipt,
	)

	requirePerformanceRowCount(t, ctx, db, 3)
}

func TestPerformanceRepositoryLatestSameTimestamp(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	collectedAt := now.Add(-time.Second)

	higher := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("f", 32),
		collectedAt,
	)

	lower := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("a", 32),
		collectedAt,
	)

	receipt := requirePerformanceInsert(
		t, ctx, repository, higher, now,
	)

	// Inserting the lower sample ID later must
	// not affect deterministic ordering.
	requirePerformanceInsert(
		t,
		ctx,
		repository,
		lower,
		now.Add(time.Second),
	)

	actual, err := repository.Latest(
		ctx,
		repositoryDeviceID,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireQueryPerformanceRecord(
		t,
		actual,
		higher,
		receipt,
	)
}

func TestPerformanceRepositoryLatestMillisecondOrdering(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime().Truncate(
		time.Millisecond,
	)

	base := now.Add(-time.Second)

	higher := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("f", 32),
		base.Add(100*time.Microsecond),
	)

	lower := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("a", 32),
		base.Add(900*time.Microsecond),
	)

	// Both samples share the same SQLite
	// millisecond timestamp despite having
	// different original payload timestamps.
	receipt := requirePerformanceInsert(
		t, ctx, repository, higher, now,
	)

	requirePerformanceInsert(
		t, ctx, repository, lower, now,
	)

	actual, err := repository.Latest(
		ctx,
		repositoryDeviceID,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireQueryPerformanceRecord(
		t,
		actual,
		higher,
		receipt,
	)
}

func TestPerformanceRepositoryLatestDeviceIsolation(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	sampleID := strings.Repeat("a", 32)

	first := testQueryPerformanceRequest(
		repositoryDeviceID,
		sampleID,
		now.Add(-time.Hour),
	)

	second := testQueryPerformanceRequest(
		repositoryOtherDeviceID,
		sampleID,
		now.Add(-time.Second),
	)

	firstReceipt := requirePerformanceInsert(
		t, ctx, repository, first, now,
	)

	secondReceipt := requirePerformanceInsert(
		t, ctx, repository, second, now,
	)

	actual, err := repository.Latest(
		ctx,
		repositoryDeviceID,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireQueryPerformanceRecord(
		t,
		actual,
		first,
		firstReceipt,
	)

	actual, err = repository.Latest(
		ctx,
		repositoryOtherDeviceID,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireQueryPerformanceRecord(
		t,
		actual,
		second,
		secondReceipt,
	)
}

func TestPerformanceRepositoryLatestNotFound(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	for _, deviceID := range []string{
		repositoryDeviceID,
		repositoryOtherDeviceID,
		"33333333-3333-4333-8333-333333333333",
	} {
		_, err := repository.Latest(
			ctx,
			deviceID,
		)

		requirePerformanceRepositoryError(
			t,
			err,
			ErrNotFound,
		)
	}
}

func TestPerformanceRepositoryLatestInvalidDevice(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	for _, deviceID := range []string{
		"",
		" ",
		" " + repositoryDeviceID,
		repositoryDeviceID + "\n",
		strings.Repeat("a", 1025),
	} {
		_, err := repository.Latest(
			ctx,
			deviceID,
		)

		requirePerformanceRepositoryError(
			t,
			err,
			ErrInvalidQuery,
		)
	}
}

func TestPerformanceRepositoryLatestCanceledContext(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	canceled, cancel := context.WithCancel(ctx)

	cancel()

	_, err := repository.Latest(
		canceled,
		repositoryDeviceID,
	)

	requirePerformanceRepositoryError(
		t,
		err,
		context.Canceled,
	)
}

func TestPerformanceRepositoryLatestCorruptPayload(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	older := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("a", 32),
		now.Add(-time.Hour),
	)

	newer := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("b", 32),
		now.Add(-time.Second),
	)

	requirePerformanceInsert(
		t, ctx, repository, older, now,
	)

	requirePerformanceInsert(
		t, ctx, repository, newer, now,
	)

	_, err := db.ExecContext(ctx, `
		UPDATE device_performance
		SET payload = ?
		WHERE device_id = ?
			AND sample_id = ?
	`,
		[]byte{0xff},
		newer.DeviceId,
		newer.SampleId,
	)
	if err != nil {
		t.Fatal(err)
	}

	// A corrupt latest sample must not cause
	// silent fallback to an older snapshot.
	_, err = repository.Latest(
		ctx,
		repositoryDeviceID,
	)

	requirePerformanceRepositoryError(
		t,
		err,
		ErrCorruptPerformance,
	)
}

func TestPerformanceRepositoryLatestUnsupportedVersion(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	request := testPerformanceRequest(now)

	requirePerformanceInsert(
		t, ctx, repository, request, now,
	)

	_, err := db.ExecContext(ctx, `
		UPDATE device_performance
		SET payload_version = ?
		WHERE device_id = ?
			AND sample_id = ?
	`,
		CurrentPayloadVersion+1,
		request.DeviceId,
		request.SampleId,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = repository.Latest(
		ctx,
		request.DeviceId,
	)

	requirePerformanceRepositoryError(
		t,
		err,
		ErrUnsupportedPayloadVersion,
	)
}

func TestPerformanceRepositoryLatestIndependentSnapshots(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	request := testPerformanceRequest(now)

	receipt := requirePerformanceInsert(
		t, ctx, repository, request, now,
	)

	first, err := repository.Latest(
		ctx,
		request.DeviceId,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Mutating a retrieved snapshot must not
	// alter subsequent database reads.
	first.Snapshot.Cpu.UsagePercent = 99
	first.Snapshot.Memory.TotalBytes = 1

	second, err := repository.Latest(
		ctx,
		request.DeviceId,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireQueryPerformanceRecord(
		t,
		second,
		request,
		receipt,
	)
}

// setupHistoryQuerySamples creates six historical
// records at one-minute collection intervals.
func setupHistoryQuerySamples(
	t *testing.T,
	ctx context.Context,
	repository *Repository,
) (
	time.Time,
	[]*rackosv1.ReportPerformanceRequest,
) {
	t.Helper()

	now := testPerformanceTime().Truncate(
		time.Millisecond,
	)

	base := now.Add(-10 * time.Minute)

	sampleCharacters := []string{
		"a", "b", "c", "d", "e", "f",
	}

	requests := make(
		[]*rackosv1.ReportPerformanceRequest,
		0,
		len(sampleCharacters),
	)

	for i, character := range sampleCharacters {
		request := testQueryPerformanceRequest(
			repositoryDeviceID,
			strings.Repeat(character, 32),
			base.Add(
				time.Duration(i)*time.Minute,
			),
		)

		requests = append(
			requests,
			request,
		)
	}

	// Insert records out of chronological order.
	for _, i := range []int{3, 0, 5, 1, 4, 2} {
		requirePerformanceInsert(
			t,
			ctx,
			repository,
			requests[i],
			now,
		)
	}

	return base, requests
}

func TestPerformanceRepositoryHistoryTimeRanges(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	base, requests := setupHistoryQuerySamples(
		t,
		ctx,
		repository,
	)

	bound := func(minutes int) *int64 {
		return historyMillisPtr(
			base.Add(
				time.Duration(minutes) * time.Minute,
			).UnixMilli(),
		)
	}

	tests := []struct {
		name     string
		query    HistoryQuery
		expected []int
	}{
		{
			name: "unbounded",
			query: HistoryQuery{
				Limit: DefaultHistoryLimit,
			},
			expected: []int{5, 4, 3, 2, 1, 0},
		},
		{
			name: "inclusive lower bound",
			query: HistoryQuery{
				FromMillis: bound(3),
				Limit:      DefaultHistoryLimit,
			},
			expected: []int{5, 4, 3},
		},
		{
			name: "exclusive upper bound",
			query: HistoryQuery{
				ToMillis: bound(3),
				Limit:    DefaultHistoryLimit,
			},
			expected: []int{2, 1, 0},
		},
		{
			name: "bounded range",
			query: HistoryQuery{
				FromMillis: bound(1),
				ToMillis:   bound(4),
				Limit:      DefaultHistoryLimit,
			},
			expected: []int{3, 2, 1},
		},
		{
			name: "single sample range",
			query: HistoryQuery{
				FromMillis: bound(2),
				ToMillis:   bound(3),
				Limit:      DefaultHistoryLimit,
			},
			expected: []int{2},
		},
		{
			name: "range before history",
			query: HistoryQuery{
				ToMillis: bound(0),
				Limit:    DefaultHistoryLimit,
			},
			expected: []int{},
		},
		{
			name: "range after history",
			query: HistoryQuery{
				FromMillis: bound(6),
				Limit:      DefaultHistoryLimit,
			},
			expected: []int{},
		},
	}

	receipt := testPerformanceTime().Truncate(
		time.Millisecond,
	)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			samples, hasMore, err :=
				repository.readHistoryRows(
					ctx,
					repositoryDeviceID,
					tt.query,
				)
			if err != nil {
				t.Fatal(err)
			}

			if samples == nil {
				t.Fatal("history returned a nil slice")
			}

			if hasMore {
				t.Fatal("unexpected additional page")
			}

			if len(samples) != len(tt.expected) {
				t.Fatalf(
					"expected %d samples, got %d",
					len(tt.expected),
					len(samples),
				)
			}

			for i, index := range tt.expected {
				requireQueryPerformanceRecord(
					t,
					samples[i],
					requests[index],
					receipt,
				)
			}
		})
	}

	requirePerformanceRowCount(
		t,
		ctx,
		db,
		6,
	)
}

func TestPerformanceRepositoryHistoryMillisecondBounds(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime().Truncate(
		time.Millisecond,
	)

	base := now.Add(-time.Second)

	request := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("a", 32),
		base.Add(900*time.Microsecond),
	)

	requirePerformanceInsert(
		t, ctx, repository, request, now,
	)

	// The stored timestamp index truncates
	// the original fractional millisecond.
	options := HistoryQuery{
		FromMillis: historyMillisPtr(
			base.UnixMilli(),
		),
		ToMillis: historyMillisPtr(
			base.Add(time.Millisecond).UnixMilli(),
		),
		Limit: DefaultHistoryLimit,
	}

	samples, hasMore, err :=
		repository.readHistoryRows(
			ctx,
			repositoryDeviceID,
			options,
		)
	if err != nil {
		t.Fatal(err)
	}

	if len(samples) != 1 || hasMore {
		t.Fatal("incorrect millisecond range result")
	}

	requireQueryPerformanceRecord(
		t,
		samples[0],
		request,
		now,
	)

	// An exclusive upper bound equal to the
	// indexed timestamp excludes this record.
	options.FromMillis = nil

	options.ToMillis = historyMillisPtr(
		base.UnixMilli(),
	)

	samples, hasMore, err =
		repository.readHistoryRows(
			ctx,
			repositoryDeviceID,
			options,
		)
	if err != nil {
		t.Fatal(err)
	}

	if len(samples) != 0 || hasMore {
		t.Fatal("exclusive upper bound included a sample")
	}
}

func TestPerformanceRepositoryHistoryDeviceIsolation(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	first := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("a", 32),
		now.Add(-time.Minute),
	)

	second := testQueryPerformanceRequest(
		repositoryOtherDeviceID,
		first.SampleId,
		now.Add(-time.Second),
	)

	second.Snapshot.Cpu.UsagePercent = 75

	requirePerformanceInsert(
		t, ctx, repository, first, now,
	)

	requirePerformanceInsert(
		t, ctx, repository, second, now,
	)

	options := HistoryQuery{
		Limit: DefaultHistoryLimit,
	}

	for _, expected := range []*rackosv1.ReportPerformanceRequest{
		first,
		second,
	} {
		samples, hasMore, err :=
			repository.readHistoryRows(
				ctx,
				expected.DeviceId,
				options,
			)
		if err != nil {
			t.Fatal(err)
		}

		if len(samples) != 1 || hasMore {
			t.Fatal("incorrect device history count")
		}

		requireQueryPerformanceRecord(
			t,
			samples[0],
			expected,
			now,
		)
	}

	requirePerformanceRowCount(
		t,
		ctx,
		db,
		2,
	)
}

func TestPerformanceRepositoryHistoryEmpty(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	for _, deviceID := range []string{
		repositoryDeviceID,
		repositoryOtherDeviceID,
		"33333333-3333-4333-8333-333333333333",
	} {
		samples, hasMore, err :=
			repository.readHistoryRows(
				ctx,
				deviceID,
				HistoryQuery{
					Limit: DefaultHistoryLimit,
				},
			)
		if err != nil {
			t.Fatal(err)
		}

		if samples == nil {
			t.Fatal("empty history returned a nil slice")
		}

		if len(samples) != 0 {
			t.Fatal("unexpected historical records")
		}

		if hasMore {
			t.Fatal("empty history has additional records")
		}
	}
}

func TestPerformanceRepositoryHistoryInvalidQuery(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	tests := []struct {
		name     string
		deviceID string
		options  HistoryQuery
	}{
		{
			name:     "invalid device identity",
			deviceID: "",
			options: HistoryQuery{
				Limit: DefaultHistoryLimit,
			},
		},
		{
			name:     "missing limit",
			deviceID: repositoryDeviceID,
			options:  HistoryQuery{},
		},
		{
			name:     "oversized limit",
			deviceID: repositoryDeviceID,
			options: HistoryQuery{
				Limit: MaxHistoryLimit + 1,
			},
		},
		{
			name:     "reversed range",
			deviceID: repositoryDeviceID,
			options: HistoryQuery{
				FromMillis: historyMillisPtr(2000),
				ToMillis:   historyMillisPtr(1000),
				Limit:      DefaultHistoryLimit,
			},
		},
		{
			name:     "invalid cursor",
			deviceID: repositoryDeviceID,
			options: HistoryQuery{
				Limit: DefaultHistoryLimit,
				After: &HistoryCursor{
					CollectedAtMillis: 1000,
					SampleID:          "invalid",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			samples, hasMore, err :=
				repository.readHistoryRows(
					ctx,
					tt.deviceID,
					tt.options,
				)

			requirePerformanceRepositoryError(
				t,
				err,
				ErrInvalidQuery,
			)

			if samples != nil || hasMore {
				t.Fatal("invalid query returned results")
			}
		})
	}
}

func TestPerformanceRepositoryHistoryCanceledContext(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	canceled, cancel := context.WithCancel(ctx)

	cancel()

	samples, hasMore, err :=
		repository.readHistoryRows(
			canceled,
			repositoryDeviceID,
			HistoryQuery{
				Limit: DefaultHistoryLimit,
			},
		)

	requirePerformanceRepositoryError(
		t,
		err,
		context.Canceled,
	)

	if samples != nil || hasMore {
		t.Fatal("canceled query returned results")
	}
}

func TestPerformanceRepositoryHistoryLimit(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	_, requests := setupHistoryQuerySamples(
		t,
		ctx,
		repository,
	)

	tests := []struct {
		limit   int
		count   int
		hasMore bool
	}{
		{1, 1, true},
		{3, 3, true},
		{5, 5, true},
		{6, 6, false},
		{7, 6, false},
		{MaxHistoryLimit, 6, false},
	}

	for _, tt := range tests {
		samples, hasMore, err :=
			repository.readHistoryRows(
				ctx,
				repositoryDeviceID,
				HistoryQuery{
					Limit: tt.limit,
				},
			)
		if err != nil {
			t.Fatal(err)
		}

		if len(samples) != tt.count {
			t.Fatalf(
				"limit %d: expected %d samples, got %d",
				tt.limit,
				tt.count,
				len(samples),
			)
		}

		if hasMore != tt.hasMore {
			t.Fatalf(
				"limit %d: incorrect continuation state",
				tt.limit,
			)
		}

		for i, sample := range samples {
			expected := requests[len(requests)-1-i]

			if sample.SampleID != expected.SampleId {
				t.Fatal("incorrect historical ordering")
			}
		}
	}
}

func TestPerformanceRepositoryHistoryPagination(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	_, requests := setupHistoryQuerySamples(
		t,
		ctx,
		repository,
	)

	for _, limit := range []int{1, 2, 4, 6, 25} {
		options := HistoryQuery{
			Limit: limit,
		}

		seen := make(map[string]bool)
		position := len(requests) - 1
		pageCount := 0

		for {
			page, err := repository.History(
				ctx,
				repositoryDeviceID,
				options,
			)
			if err != nil {
				t.Fatal(err)
			}

			pageCount++

			if pageCount > len(requests) {
				t.Fatal("pagination did not terminate")
			}

			if len(page.Samples) == 0 ||
				len(page.Samples) > limit {
				t.Fatal("incorrect page size")
			}

			for _, sample := range page.Samples {
				if seen[sample.SampleID] {
					t.Fatal("duplicate sample across pages")
				}

				seen[sample.SampleID] = true

				if position < 0 {
					t.Fatal("unexpected additional sample")
				}

				requireQueryPerformanceRecord(
					t,
					sample,
					requests[position],
					testPerformanceTime().Truncate(
						time.Millisecond,
					),
				)

				position--
			}

			if !page.HasMore() {
				if page.NextCursor != nil {
					t.Fatal("final page has a cursor")
				}

				break
			}

			last := page.Samples[len(page.Samples)-1]

			if page.NextCursor.CollectedAtMillis !=
				last.CollectedAt.UnixMilli() {
				t.Fatal("incorrect continuation timestamp")
			}

			if page.NextCursor.SampleID != last.SampleID {
				t.Fatal("cursor does not identify last sample")
			}

			options.After = page.NextCursor
		}

		if len(seen) != len(requests) ||
			position != -1 {
			t.Fatal("pagination skipped historical records")
		}
	}
}

func TestPerformanceRepositoryHistoryPaginationTimestampTies(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime().Truncate(
		time.Millisecond,
	)

	collectedAt := now.Add(-time.Second)

	// Every record shares the same indexed
	// collection timestamp.
	for _, character := range []string{
		"c", "a", "e", "b", "d",
	} {
		request := testQueryPerformanceRequest(
			repositoryDeviceID,
			strings.Repeat(character, 32),
			collectedAt,
		)

		requirePerformanceInsert(
			t,
			ctx,
			repository,
			request,
			now,
		)
	}

	expectedPages := [][]string{
		{"e", "d"},
		{"c", "b"},
		{"a"},
	}

	options := HistoryQuery{
		Limit: 2,
	}

	for i, expected := range expectedPages {
		page, err := repository.History(
			ctx,
			repositoryDeviceID,
			options,
		)
		if err != nil {
			t.Fatal(err)
		}

		if len(page.Samples) != len(expected) {
			t.Fatal("incorrect timestamp tie page size")
		}

		for j, character := range expected {
			expectedID := strings.Repeat(
				character,
				32,
			)

			if page.Samples[j].SampleID != expectedID {
				t.Fatal("incorrect sample identity ordering")
			}
		}

		hasMore := i < len(expectedPages)-1

		if page.HasMore() != hasMore {
			t.Fatal("incorrect continuation state")
		}

		options.After = page.NextCursor
	}
}

func TestPerformanceRepositoryHistoryPaginationTimeBounds(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	base, requests := setupHistoryQuerySamples(
		t,
		ctx,
		repository,
	)

	options := HistoryQuery{
		FromMillis: historyMillisPtr(
			base.Add(time.Minute).UnixMilli(),
		),

		ToMillis: historyMillisPtr(
			base.Add(5 * time.Minute).UnixMilli(),
		),

		Limit: 2,
	}

	first, err := repository.History(
		ctx,
		repositoryDeviceID,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(first.Samples) != 2 ||
		!first.HasMore() {
		t.Fatal("incorrect first bounded page")
	}

	if first.Samples[0].SampleID != requests[4].SampleId ||
		first.Samples[1].SampleID != requests[3].SampleId {
		t.Fatal("incorrect first page ordering")
	}

	options.After = first.NextCursor

	second, err := repository.History(
		ctx,
		repositoryDeviceID,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(second.Samples) != 2 ||
		second.HasMore() {
		t.Fatal("incorrect final bounded page")
	}

	if second.Samples[0].SampleID != requests[2].SampleId ||
		second.Samples[1].SampleID != requests[1].SampleId {
		t.Fatal("incorrect final page ordering")
	}
}

func TestPerformanceRepositoryHistoryPaginationNewSamples(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	base, requests := setupHistoryQuerySamples(
		t,
		ctx,
		repository,
	)

	options := HistoryQuery{
		Limit: 2,
	}

	first, err := repository.History(
		ctx,
		repositoryDeviceID,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(first.Samples) != 2 ||
		!first.HasMore() {
		t.Fatal("incorrect initial page")
	}

	now := testPerformanceTime()

	// Insert a newer record before the cursor.
	newer := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("1", 32),
		base.Add(6*time.Minute),
	)

	requirePerformanceInsert(
		t,
		ctx,
		repository,
		newer,
		now,
	)

	// Insert a delayed historical record after
	// the cursor, between two existing samples.
	delayed := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("2", 32),
		base.Add(3*time.Minute+30*time.Second),
	)

	requirePerformanceInsert(
		t,
		ctx,
		repository,
		delayed,
		now,
	)

	options.After = first.NextCursor

	second, err := repository.History(
		ctx,
		repositoryDeviceID,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(second.Samples) != 2 ||
		!second.HasMore() {
		t.Fatal("incorrect continuation page")
	}

	if second.Samples[0].SampleID != delayed.SampleId {
		t.Fatal("delayed historical sample was omitted")
	}

	if second.Samples[1].SampleID != requests[3].SampleId {
		t.Fatal("existing historical sample was skipped")
	}

	// A fresh query must return the newly
	// inserted latest sample.
	fresh, err := repository.History(
		ctx,
		repositoryDeviceID,
		HistoryQuery{
			Limit: 1,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(fresh.Samples) != 1 ||
		fresh.Samples[0].SampleID != newer.SampleId {
		t.Fatal("fresh query omitted latest sample")
	}

	requirePerformanceRowCount(
		t,
		ctx,
		db,
		8,
	)
}

func TestPerformanceRepositoryHistoryPaginationEmptyPage(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	page, err := repository.History(
		ctx,
		repositoryDeviceID,
		HistoryQuery{
			Limit: DefaultHistoryLimit,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if page.Samples == nil {
		t.Fatal("empty page returned a nil sample slice")
	}

	if len(page.Samples) != 0 {
		t.Fatal("empty page contains samples")
	}

	if page.HasMore() ||
		page.NextCursor != nil {
		t.Fatal("empty page contains continuation")
	}
}

func TestPerformanceRepositoryHistoryPaginationExhaustedCursor(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	_, requests := setupHistoryQuerySamples(
		t,
		ctx,
		repository,
	)

	oldest := requests[0]

	page, err := repository.History(
		ctx,
		repositoryDeviceID,
		HistoryQuery{
			Limit: DefaultHistoryLimit,

			After: &HistoryCursor{
				CollectedAtMillis: oldest.Snapshot.
					CollectedAt.AsTime().UnixMilli(),

				SampleID: oldest.SampleId,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if page.Samples == nil ||
		len(page.Samples) != 0 ||
		page.HasMore() {
		t.Fatal("exhausted cursor returned incorrect page")
	}
}

func TestPerformanceRepositoryHistoryPaginationInvalidQuery(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	page, err := repository.History(
		ctx,
		repositoryDeviceID,
		HistoryQuery{
			Limit: MaxHistoryLimit + 1,
		},
	)

	requirePerformanceRepositoryError(
		t,
		err,
		ErrInvalidQuery,
	)

	if page.Samples != nil ||
		page.NextCursor != nil {
		t.Fatal("invalid query returned a populated page")
	}
}

func TestPerformanceRepositoryHistoryPaginationCanceledContext(
	t *testing.T,
) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	page, err := repository.History(
		canceled,
		repositoryDeviceID,
		HistoryQuery{
			Limit: DefaultHistoryLimit,
		},
	)

	requirePerformanceRepositoryError(
		t,
		err,
		context.Canceled,
	)

	if page.Samples != nil ||
		page.NextCursor != nil {
		t.Fatal("canceled query returned a populated page")
	}
}
