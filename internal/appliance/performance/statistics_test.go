package performance

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
)

func requireStorageStatisticsTimestamp(
	t *testing.T,
	actual *time.Time,
	expected time.Time,
) {
	t.Helper()

	if actual == nil {
		t.Fatal("missing storage statistics timestamp")
	}

	expected = expected.UTC().
		Truncate(time.Millisecond)

	if !actual.Equal(expected) {
		t.Fatalf(
			"expected timestamp %s, got %s",
			expected,
			actual,
		)
	}

	if actual.Location() != time.UTC {
		t.Fatal("statistics timestamp is not UTC")
	}

	if actual.Nanosecond()%int(time.Millisecond) != 0 {
		t.Fatal("statistics timestamp is not millisecond aligned")
	}
}

func TestPerformanceStatisticsEmpty(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	statistics, err := repository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if statistics != (StorageStatistics{}) {
		t.Fatalf(
			"expected empty storage statistics, got %+v",
			statistics,
		)
	}

	// Registered devices without performance
	// records are not counted as reporting devices.
	requirePerformanceRowCount(t, ctx, db, 0)
}

func TestPerformanceStatisticsSingleSample(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	receipt := testPerformanceTime().
		Truncate(time.Millisecond)

	request := testPerformanceRequest(receipt)

	requirePerformanceInsert(
		t, ctx, repository, request, receipt,
	)

	statistics, err := repository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if statistics.TotalSamples != 1 {
		t.Fatal("incorrect sample count")
	}

	if statistics.ReportingDevices != 1 {
		t.Fatal("incorrect reporting device count")
	}

	expectedBytes := int64(
		proto.Size(request.Snapshot),
	)

	if statistics.PayloadBytes != expectedBytes {
		t.Fatalf(
			"expected %d payload bytes, got %d",
			expectedBytes,
			statistics.PayloadBytes,
		)
	}

	collectedAt := request.Snapshot.
		CollectedAt.AsTime()

	requireStorageStatisticsTimestamp(
		t,
		statistics.OldestCollectedAt,
		collectedAt,
	)

	requireStorageStatisticsTimestamp(
		t,
		statistics.LatestCollectedAt,
		collectedAt,
	)

	requireStorageStatisticsTimestamp(
		t,
		statistics.OldestReceivedAt,
		receipt,
	)

	requireStorageStatisticsTimestamp(
		t,
		statistics.LatestReceivedAt,
		receipt,
	)

	requirePerformanceRowCount(t, ctx, db, 1)
}

func TestPerformanceStatisticsMultipleDevices(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	base := testPerformanceTime().
		Truncate(time.Millisecond)

	firstReceipt := base.Add(-time.Hour)
	delayedReceipt := base.Add(2 * time.Hour)
	latestReceipt := base.Add(time.Hour)

	first := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("a", 32),
		base.Add(-2*time.Hour),
	)

	delayed := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("b", 32),
		base.Add(-48*time.Hour),
	)

	latest := testQueryPerformanceRequest(
		repositoryOtherDeviceID,
		strings.Repeat("a", 32),
		base.Add(123456789*time.Nanosecond),
	)

	// Insert records out of chronological order.
	requirePerformanceInsert(
		t, ctx, repository, latest, latestReceipt,
	)

	requirePerformanceInsert(
		t, ctx, repository, first, firstReceipt,
	)

	requirePerformanceInsert(
		t, ctx, repository, delayed, delayedReceipt,
	)

	statistics, err := repository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if statistics.TotalSamples != 3 {
		t.Fatal("incorrect historical sample count")
	}

	if statistics.ReportingDevices != 2 {
		t.Fatal("incorrect distinct device count")
	}

	expectedBytes := int64(
		proto.Size(first.Snapshot) +
			proto.Size(delayed.Snapshot) +
			proto.Size(latest.Snapshot),
	)

	if statistics.PayloadBytes != expectedBytes {
		t.Fatalf(
			"expected %d payload bytes, got %d",
			expectedBytes,
			statistics.PayloadBytes,
		)
	}

	requireStorageStatisticsTimestamp(
		t,
		statistics.OldestCollectedAt,
		delayed.Snapshot.CollectedAt.AsTime(),
	)

	requireStorageStatisticsTimestamp(
		t,
		statistics.LatestCollectedAt,
		latest.Snapshot.CollectedAt.AsTime(),
	)

	requireStorageStatisticsTimestamp(
		t,
		statistics.OldestReceivedAt,
		firstReceipt,
	)

	requireStorageStatisticsTimestamp(
		t,
		statistics.LatestReceivedAt,
		delayedReceipt,
	)

	requirePerformanceRowCount(t, ctx, db, 3)
}

func TestPerformanceStatisticsIdenticalRetry(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	receipt := testPerformanceTime().
		Truncate(time.Millisecond)

	request := testPerformanceRequest(receipt)

	requirePerformanceInsert(
		t, ctx, repository, request, receipt,
	)

	before, err := repository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	requirePerformanceInsert(
		t,
		ctx,
		repository,
		request,
		receipt.Add(time.Minute),
	)

	after, err := repository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(before, after) {
		t.Fatal("identical retry changed storage statistics")
	}

	requirePerformanceRowCount(t, ctx, db, 1)
}

func TestPerformanceStatisticsAfterRetention(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	expiredReceipt := cutoff.Add(-time.Hour)
	retainedReceipt := cutoff.Add(time.Hour)

	expired := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("a", 32),
		expiredReceipt.Add(-time.Second),
	)

	retained := testQueryPerformanceRequest(
		repositoryOtherDeviceID,
		strings.Repeat("b", 32),
		retainedReceipt.Add(-time.Second),
	)

	requirePerformanceInsert(
		t, ctx, repository, expired, expiredReceipt,
	)

	requirePerformanceInsert(
		t, ctx, repository, retained, retainedReceipt,
	)

	before, err := repository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if before.TotalSamples != 2 ||
		before.ReportingDevices != 2 {
		t.Fatal("incorrect initial statistics")
	}

	deleted, err := repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		10,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 1 {
		t.Fatal("incorrect retention deletion count")
	}

	after, err := repository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if after.TotalSamples != 1 ||
		after.ReportingDevices != 1 {
		t.Fatal("statistics did not reflect retention")
	}

	expectedBytes := int64(
		proto.Size(retained.Snapshot),
	)

	if after.PayloadBytes != expectedBytes {
		t.Fatal("expired payload bytes remained in statistics")
	}

	if before.PayloadBytes-after.PayloadBytes !=
		int64(proto.Size(expired.Snapshot)) {
		t.Fatal("incorrect payload storage reduction")
	}

	requireStorageStatisticsTimestamp(
		t,
		after.OldestCollectedAt,
		retained.Snapshot.CollectedAt.AsTime(),
	)

	requireStorageStatisticsTimestamp(
		t,
		after.LatestCollectedAt,
		retained.Snapshot.CollectedAt.AsTime(),
	)

	requireStorageStatisticsTimestamp(
		t,
		after.OldestReceivedAt,
		retainedReceipt,
	)

	requireStorageStatisticsTimestamp(
		t,
		after.LatestReceivedAt,
		retainedReceipt,
	)

	// Expire the remaining sample.
	deleted, err = repository.DeleteExpiredBatch(
		ctx,
		retainedReceipt.Add(time.Millisecond),
		10,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 1 {
		t.Fatal("remaining sample was not deleted")
	}

	empty, err := repository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if empty != (StorageStatistics{}) {
		t.Fatal("statistics did not return to empty state")
	}

	requirePerformanceRowCount(t, ctx, db, 0)
}

func TestPerformanceStatisticsCorruptPayload(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	receipt := testPerformanceTime().
		Truncate(time.Millisecond)

	request := testPerformanceRequest(receipt)

	requirePerformanceInsert(
		t, ctx, repository, request, receipt,
	)

	_, err := db.ExecContext(ctx, `
		UPDATE device_performance
		SET payload = X'FF'
		WHERE device_id = ?
			AND sample_id = ?
	`,
		request.DeviceId,
		request.SampleId,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = repository.Get(
		ctx,
		request.DeviceId,
		request.SampleId,
	)

	requirePerformanceRepositoryError(
		t,
		err,
		ErrCorruptPerformance,
	)

	// Statistics must remain available because
	// payload decoding is not required.
	statistics, err := repository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if statistics.TotalSamples != 1 ||
		statistics.ReportingDevices != 1 ||
		statistics.PayloadBytes != 1 {
		t.Fatal("incorrect corrupt payload statistics")
	}

	requirePerformanceRowCount(t, ctx, db, 1)
}

func TestPerformanceStatisticsUnsupportedPayloadVersion(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	receipt := testPerformanceTime().
		Truncate(time.Millisecond)

	request := testPerformanceRequest(receipt)

	requirePerformanceInsert(
		t, ctx, repository, request, receipt,
	)

	before, err := repository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.ExecContext(ctx, `
		UPDATE device_performance
		SET payload_version = 999
		WHERE device_id = ?
			AND sample_id = ?
	`,
		request.DeviceId,
		request.SampleId,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = repository.Get(
		ctx,
		request.DeviceId,
		request.SampleId,
	)

	requirePerformanceRepositoryError(
		t,
		err,
		ErrUnsupportedPayloadVersion,
	)

	after, err := repository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(before, after) {
		t.Fatal("payload version changed storage statistics")
	}
}

func TestPerformanceStatisticsInvalidStoredTimestamp(
	t *testing.T,
) {
	for _, column := range []string{
		"collected_at",
		"received_at",
	} {
		t.Run(column, func(t *testing.T) {
			ctx, db, repository, _ :=
				setupPerformanceRepository(t)

			receipt := testPerformanceTime().
				Truncate(time.Millisecond)

			request := testPerformanceRequest(receipt)

			requirePerformanceInsert(
				t, ctx, repository, request, receipt,
			)

			// The schema permits positive integers
			// outside the supported timestamp range.
			invalidTimestamp := time.Date(
				10000, 1, 1,
				0, 0, 0, 0,
				time.UTC,
			).UnixMilli()

			_, err := db.ExecContext(
				ctx,
				"UPDATE device_performance SET "+
					column+" = ?",
				invalidTimestamp,
			)
			if err != nil {
				t.Fatal(err)
			}

			statistics, err :=
				repository.Statistics(ctx)

			requirePerformanceRepositoryError(
				t,
				err,
				ErrCorruptPerformance,
			)

			if statistics != (StorageStatistics{}) {
				t.Fatal("failed query returned partial statistics")
			}

			requirePerformanceRowCount(
				t, ctx, db, 1,
			)
		})
	}
}

func TestPerformanceStatisticsCanceledContext(t *testing.T) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	statistics, err := repository.Statistics(canceled)

	requirePerformanceRepositoryError(
		t,
		err,
		context.Canceled,
	)

	if statistics != (StorageStatistics{}) {
		t.Fatal("canceled query returned statistics")
	}
}

func TestPerformanceStatisticsDatabaseFailure(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	statistics, err := repository.Statistics(ctx)

	if err == nil {
		t.Fatal("closed database returned statistics")
	}

	if statistics != (StorageStatistics{}) {
		t.Fatal("failed query returned partial statistics")
	}
}

func TestPerformanceStatisticsTransactionVisibility(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	receipt := testPerformanceTime().
		Truncate(time.Millisecond)

	request := testPerformanceRequest(receipt)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	transactionRepository := repository.WithTx(tx)

	requirePerformanceInsert(
		t,
		ctx,
		transactionRepository,
		request,
		receipt,
	)

	statistics, err :=
		transactionRepository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if statistics.TotalSamples != 1 ||
		statistics.ReportingDevices != 1 {
		t.Fatal("transaction statistics omitted pending sample")
	}

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	statistics, err = repository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if statistics != (StorageStatistics{}) {
		t.Fatal("rolled back sample affected statistics")
	}

	requirePerformanceRowCount(t, ctx, db, 0)
}

func TestPerformanceStatisticsTimestampValidation(
	t *testing.T,
) {
	for _, value := range []sql.NullInt64{
		{},
		{Int64: 0, Valid: true},
		{Int64: -1, Valid: true},
		{Int64: 253402300800000, Valid: true},
	} {
		timestamp, err := storageStatisticsTimestamp(
			value,
			"test timestamp",
		)

		requirePerformanceRepositoryError(
			t,
			err,
			ErrCorruptPerformance,
		)

		if timestamp != nil {
			t.Fatal("invalid timestamp returned a value")
		}
	}

	for _, value := range []int64{
		1,
		253402300799999,
	} {
		timestamp, err := storageStatisticsTimestamp(
			sql.NullInt64{
				Int64: value,
				Valid: true,
			},
			"test timestamp",
		)
		if err != nil {
			t.Fatal(err)
		}

		requireStorageStatisticsTimestamp(
			t,
			timestamp,
			time.UnixMilli(value),
		)
	}
}
