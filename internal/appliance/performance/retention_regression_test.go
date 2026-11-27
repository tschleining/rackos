package performance

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"

	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/retention"
)

// TestPerformanceRetentionRegressionIndex verifies
// the execution plan of the actual deletion statement.
func TestPerformanceRetentionRegressionIndex(t *testing.T) {
	ctx, db, _, _ := setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	rows, err := db.QueryContext(
		ctx,
		"EXPLAIN QUERY PLAN "+deleteExpiredBatchSQL,
		cutoff.UnixMilli(),
		retention.DefaultBatchSize,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var details []string

	for rows.Next() {
		var id, parent, unused int
		var detail string

		if err := rows.Scan(
			&id,
			&parent,
			&unused,
			&detail,
		); err != nil {
			t.Fatal(err)
		}

		details = append(details, detail)
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	plan := strings.Join(details, "\n")

	t.Logf("SQLite retention query plan:\n%s", plan)

	if !strings.Contains(
		plan,
		"idx_device_performance_received_at",
	) {
		t.Fatal("receipt timestamp index was not used")
	}

	if strings.Contains(
		strings.ToUpper(plan),
		"TEMP B-TREE FOR ORDER BY",
	) {
		t.Fatal("retention query requires temporary sorting")
	}
}

// TestPerformanceRetentionRegressionAtomicFailure
// injects a failure after deletion has already started.
//
// The complete statement must roll back.
func TestPerformanceRetentionRegressionAtomicFailure(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	receipt := cutoff.Add(-time.Hour)

	var requests []*rackosv1.ReportPerformanceRequest

	for _, character := range []string{"a", "b", "c"} {
		request := testQueryPerformanceRequest(
			repositoryDeviceID,
			strings.Repeat(character, 32),
			receipt.Add(-time.Second),
		)

		requirePerformanceInsert(
			t, ctx, repository, request, receipt,
		)

		requests = append(requests, request)
	}

	// Abort after the second record has been
	// removed within the deletion statement.
	_, err := db.ExecContext(ctx, `
		CREATE TRIGGER retention_failure
		AFTER DELETE ON device_performance
		WHEN (
			SELECT COUNT(*) FROM device_performance
		) = 1
		BEGIN
			SELECT RAISE(
				ABORT,
				'injected retention failure'
			);
		END;
	`)
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		3,
	)

	if err == nil {
		t.Fatal("injected failure was not reported")
	}

	if !strings.Contains(
		err.Error(),
		"injected retention failure",
	) {
		t.Fatalf("unexpected deletion error: %v", err)
	}

	if deleted != 0 {
		t.Fatal("failed deletion reported success")
	}

	requirePerformanceRowCount(t, ctx, db, 3)

	for _, request := range requests {
		requirePerformanceRecord(
			t, ctx, repository, request, receipt,
		)
	}

	// Remove the failure and retry.
	_, err = db.ExecContext(
		ctx,
		"DROP TRIGGER retention_failure",
	)
	if err != nil {
		t.Fatal(err)
	}

	deleted, err = repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		3,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 3 {
		t.Fatalf("expected 3 deletions, got %d", deleted)
	}

	requirePerformanceRowCount(t, ctx, db, 0)
}

func TestPerformanceRetentionRegressionCorruptPayloads(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	expiredReceipt := cutoff.Add(-time.Hour)
	recentReceipt := cutoff.Add(time.Hour)

	corrupt := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("a", 32),
		expiredReceipt.Add(-time.Second),
	)

	unsupported := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("b", 32),
		expiredReceipt.Add(-time.Second),
	)

	recent := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("c", 32),
		recentReceipt.Add(-time.Second),
	)

	requirePerformanceInsert(
		t, ctx, repository, corrupt, expiredReceipt,
	)

	requirePerformanceInsert(
		t, ctx, repository, unsupported, expiredReceipt,
	)

	requirePerformanceInsert(
		t, ctx, repository, recent, recentReceipt,
	)

	_, err := db.ExecContext(ctx, `
		UPDATE device_performance
		SET payload = X'FF'
		WHERE device_id = ?
			AND sample_id IN (?, ?)
	`,
		repositoryDeviceID,
		corrupt.SampleId,
		recent.SampleId,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.ExecContext(ctx, `
		UPDATE device_performance
		SET payload_version = 999
		WHERE device_id = ?
			AND sample_id = ?
	`,
		repositoryDeviceID,
		unsupported.SampleId,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Confirm the stored records fail normal
	// repository retrieval.
	_, err = repository.Get(
		ctx,
		corrupt.DeviceId,
		corrupt.SampleId,
	)

	requirePerformanceRepositoryError(
		t, err, ErrCorruptPerformance,
	)

	_, err = repository.Get(
		ctx,
		unsupported.DeviceId,
		unsupported.SampleId,
	)

	requirePerformanceRepositoryError(
		t, err, ErrUnsupportedPayloadVersion,
	)

	// Retention operates on receipt timestamps,
	// independently of stored payload validity.
	deleted, err := repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		10,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 2 {
		t.Fatalf("expected 2 deletions, got %d", deleted)
	}

	requireRetentionSampleDeleted(
		t, ctx, repository, corrupt,
	)

	requireRetentionSampleDeleted(
		t, ctx, repository, unsupported,
	)

	// Recent corrupt data must not be removed
	// simply because its payload is invalid.
	_, err = repository.Get(
		ctx,
		recent.DeviceId,
		recent.SampleId,
	)

	requirePerformanceRepositoryError(
		t, err, ErrCorruptPerformance,
	)

	requirePerformanceRowCount(t, ctx, db, 1)
}

func TestPerformanceRetentionRegressionMaximumBatch(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	total := retention.MaxBatchSize + 1

	// Seed the test database within one
	// transaction to avoid unnecessary commits.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	transactionRepository := repository.WithTx(tx)

	var newest *rackosv1.ReportPerformanceRequest
	var newestReceipt time.Time

	for i := 0; i < total; i++ {
		receipt := cutoff.Add(
			-time.Duration(total-i) * time.Second,
		)

		request := testQueryPerformanceRequest(
			repositoryDeviceID,
			fmt.Sprintf("%032x", i),
			receipt.Add(-time.Second),
		)

		requirePerformanceInsert(
			t,
			ctx,
			transactionRepository,
			request,
			receipt,
		)

		newest = request
		newestReceipt = receipt
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	requirePerformanceRowCount(t, ctx, db, total)

	deleted, err := repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		retention.MaxBatchSize,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != int64(retention.MaxBatchSize) {
		t.Fatalf(
			"expected maximum batch of %d, got %d",
			retention.MaxBatchSize,
			deleted,
		)
	}

	requirePerformanceRowCount(t, ctx, db, 1)

	requirePerformanceRecord(
		t,
		ctx,
		repository,
		newest,
		newestReceipt,
	)

	deleted, err = repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		retention.MaxBatchSize,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 1 {
		t.Fatal("remaining expired record was not removed")
	}

	requirePerformanceRowCount(t, ctx, db, 0)
}

func TestPerformanceRetentionRegressionDatabaseRestart(t *testing.T) {
	ctx, db, repository, dir :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	oldReceipt := cutoff.Add(-time.Hour)
	newReceipt := cutoff.Add(time.Hour)

	expired := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("a", 32),
		oldReceipt.Add(-time.Second),
	)

	retained := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("b", 32),
		newReceipt.Add(-time.Second),
	)

	requirePerformanceInsert(
		t, ctx, repository, expired, oldReceipt,
	)

	requirePerformanceInsert(
		t, ctx, repository, retained, newReceipt,
	)

	deleted, err := repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		10,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 1 {
		t.Fatal("incorrect initial deletion count")
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Operations against the closed database
	// must report an error.
	deleted, err = repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		10,
	)

	if err == nil || deleted != 0 {
		t.Fatal("closed database reported successful deletion")
	}

	reopened, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	repository = NewRepository(reopened)

	requireRetentionSampleDeleted(
		t, ctx, repository, expired,
	)

	requirePerformanceRecord(
		t, ctx, repository, retained, newReceipt,
	)

	requirePerformanceRowCount(t, ctx, reopened, 1)

	// Repeating cleanup after restart must
	// preserve the remaining unexpired record.
	deleted, err = repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		10,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 0 {
		t.Fatal("restart cleanup removed unexpired data")
	}

	// Advance the cutoff and verify that
	// cleanup remains operational.
	deleted, err = repository.DeleteExpiredBatch(
		ctx,
		newReceipt.Add(time.Millisecond),
		10,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 1 {
		t.Fatal("cleanup did not recover after restart")
	}

	requirePerformanceRowCount(t, ctx, reopened, 0)
}

func TestPerformanceRetentionRegressionConcurrentIngestion(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	const sampleCount = 20

	oldReceipt := cutoff.Add(-time.Hour)
	newReceipt := cutoff.Add(time.Hour)

	var expired []*rackosv1.ReportPerformanceRequest
	var incoming []*rackosv1.ReportPerformanceRequest

	for i := 0; i < sampleCount; i++ {
		oldRequest := testQueryPerformanceRequest(
			repositoryDeviceID,
			fmt.Sprintf("%032x", i),
			oldReceipt.Add(-time.Second),
		)

		requirePerformanceInsert(
			t, ctx, repository, oldRequest, oldReceipt,
		)

		expired = append(expired, oldRequest)

		newRequest := testQueryPerformanceRequest(
			repositoryDeviceID,
			fmt.Sprintf("%032x", i+sampleCount),
			newReceipt.Add(-time.Second),
		)

		incoming = append(incoming, newRequest)
	}

	start := make(chan struct{})
	results := make(chan error, 2)

	// Run performance ingestion concurrently
	// with bounded retention operations.
	go func() {
		<-start

		for _, request := range incoming {
			_, err := repository.Insert(
				ctx,
				request,
				newReceipt,
			)
			if err != nil {
				results <- err
				return
			}
		}

		results <- nil
	}()

	go func() {
		<-start

		var totalDeleted int64

		for {
			deleted, err :=
				repository.DeleteExpiredBatch(
					ctx,
					cutoff,
					3,
				)
			if err != nil {
				results <- err
				return
			}

			totalDeleted += deleted

			if deleted < 3 {
				break
			}
		}

		if totalDeleted != sampleCount {
			results <- fmt.Errorf(
				"expected %d deletions, got %d",
				sampleCount,
				totalDeleted,
			)
			return
		}

		results <- nil
	}()

	close(start)

	// Join both goroutines before checking errors.
	firstErr := <-results
	secondErr := <-results

	if firstErr != nil {
		t.Fatal(firstErr)
	}

	if secondErr != nil {
		t.Fatal(secondErr)
	}

	for _, request := range expired {
		requireRetentionSampleDeleted(
			t, ctx, repository, request,
		)
	}

	for _, request := range incoming {
		requirePerformanceRecord(
			t, ctx, repository, request, newReceipt,
		)
	}

	requirePerformanceRowCount(
		t, ctx, db, sampleCount,
	)
}

// retentionRegressionTableRows captures table contents
// for exact before-and-after comparisons.
func retentionRegressionTableRows(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
) [][]any {
	t.Helper()

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}

	result := make([][]any, 0)

	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))

		for i := range values {
			destinations[i] = &values[i]
		}

		if err := rows.Scan(
			destinations...,
		); err != nil {
			t.Fatal(err)
		}

		result = append(result, values)
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	return result
}

func TestPerformanceRetentionRegressionUnrelatedData(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	receipt := cutoff.Add(-time.Hour)
	millis := receipt.UnixMilli()

	// Seed representative records in every
	// unrelated appliance table.
	fixtures := []struct {
		query string
		args  []any
	}{
		{
			query: `
				INSERT INTO appliance_metadata (
					key, value
				) VALUES (?, ?)
			`,
			args: []any{
				"retention-test",
				"preserved metadata",
			},
		},
		{
			query: `
				INSERT INTO enrollment_tokens (
					id,
					token_hash,
					created_at,
					expires_at
				) VALUES (?, zeroblob(32), ?, ?)
			`,
			args: []any{
				"retention-test-token",
				millis,
				receipt.Add(time.Hour).UnixMilli(),
			},
		},
		{
			query: `
				INSERT INTO agent_credentials (
					device_id,
					token_hash,
					created_at
				) VALUES (?, zeroblob(32), ?)
			`,
			args: []any{
				repositoryDeviceID,
				millis,
			},
		},
		{
			query: `
				INSERT INTO device_inventory (
					device_id,
					hostname,
					os,
					architecture,
					logical_cpus,
					memory_bytes,
					collected_at,
					received_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			`,
			args: []any{
				repositoryDeviceID,
				"retention-test.local",
				"linux",
				"arm64",
				8,
				int64(16 * 1024 * 1024 * 1024),
				millis,
				millis,
			},
		},
	}

	for _, fixture := range fixtures {
		_, err := db.ExecContext(
			ctx,
			fixture.query,
			fixture.args...,
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	queries := []string{
		"SELECT * FROM appliance_metadata ORDER BY key",
		"SELECT * FROM devices ORDER BY id",
		"SELECT * FROM enrollment_tokens ORDER BY id",
		"SELECT * FROM agent_credentials ORDER BY device_id",
		"SELECT * FROM device_inventory ORDER BY device_id",
	}

	before := make([][][]any, len(queries))

	for i, query := range queries {
		before[i] = retentionRegressionTableRows(
			t, ctx, db, query,
		)

		if len(before[i]) == 0 {
			t.Fatalf("missing fixture data: %s", query)
		}
	}

	for _, deviceID := range []string{
		repositoryDeviceID,
		repositoryOtherDeviceID,
	} {
		request := testQueryPerformanceRequest(
			deviceID,
			strings.Repeat("a", 32),
			receipt.Add(-time.Second),
		)

		requirePerformanceInsert(
			t, ctx, repository, request, receipt,
		)
	}

	deleted, err := repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		10,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 2 {
		t.Fatal("incorrect deletion count")
	}

	requirePerformanceRowCount(t, ctx, db, 0)

	for i, query := range queries {
		after := retentionRegressionTableRows(
			t, ctx, db, query,
		)

		if !reflect.DeepEqual(before[i], after) {
			t.Fatalf(
				"retention modified unrelated data: %s",
				query,
			)
		}
	}

	rows, err := db.QueryContext(
		ctx,
		"PRAGMA foreign_key_check",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	if rows.Next() {
		t.Fatal("retention introduced a foreign key violation")
	}

	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestPerformanceRetentionRegressionHistoricalCursor(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	oldReceipt := cutoff.Add(-time.Hour)
	newReceipt := cutoff.Add(time.Hour)

	// The cursor sample expires based on its
	// receipt time, even though older collected
	// samples were received more recently.
	newest := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("d", 32),
		cutoff.Add(-2*time.Hour),
	)

	cursorRecord := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("c", 32),
		cutoff.Add(-3*time.Hour),
	)

	older := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("b", 32),
		cutoff.Add(-4*time.Hour),
	)

	oldest := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("a", 32),
		cutoff.Add(-5*time.Hour),
	)

	requirePerformanceInsert(
		t, ctx, repository, newest, newReceipt,
	)

	requirePerformanceInsert(
		t, ctx, repository, cursorRecord, oldReceipt,
	)

	requirePerformanceInsert(
		t, ctx, repository, older, newReceipt,
	)

	requirePerformanceInsert(
		t, ctx, repository, oldest, oldReceipt,
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
		first.NextCursor == nil {
		t.Fatal("incorrect first historical page")
	}

	if first.Samples[0].SampleID != newest.SampleId ||
		first.NextCursor.SampleID != cursorRecord.SampleId {
		t.Fatal("incorrect initial historical ordering")
	}

	options.After = first.NextCursor

	deleted, err := repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		10,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 2 {
		t.Fatal("incorrect retention deletion count")
	}

	requireRetentionSampleDeleted(
		t, ctx, repository, cursorRecord,
	)

	requireRetentionSampleDeleted(
		t, ctx, repository, oldest,
	)

	second, err := repository.History(
		ctx,
		repositoryDeviceID,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(second.Samples) != 1 ||
		second.HasMore() {
		t.Fatal("incorrect history after retention")
	}

	requireQueryPerformanceRecord(
		t,
		second.Samples[0],
		older,
		newReceipt,
	)

	latest, err := repository.Latest(
		ctx,
		repositoryDeviceID,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireQueryPerformanceRecord(
		t,
		latest,
		newest,
		newReceipt,
	)

	requirePerformanceRowCount(t, ctx, db, 2)
}

func TestPerformanceRetentionRegressionExpiredSampleReplay(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	originalReceipt := cutoff.Add(-time.Hour)
	replayReceipt := cutoff.Add(time.Hour)

	request := testPerformanceRequest(originalReceipt)

	requirePerformanceInsert(
		t, ctx, repository, request, originalReceipt,
	)

	deleted, err := repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		10,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 1 {
		t.Fatal("original sample did not expire")
	}

	requireRetentionSampleDeleted(
		t, ctx, repository, request,
	)

	// Idempotency is scoped to records that
	// still exist in historical storage.
	acceptedAt := requirePerformanceInsert(
		t, ctx, repository, request, replayReceipt,
	)

	if !acceptedAt.Equal(replayReceipt) {
		t.Fatal("replayed sample has incorrect receipt time")
	}

	requirePerformanceRecord(
		t, ctx, repository, request, replayReceipt,
	)

	// The replay is retained according to
	// its new appliance receipt timestamp.
	deleted, err = repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		10,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 0 {
		t.Fatal("recently accepted replay was removed")
	}

	requirePerformanceRowCount(t, ctx, db, 1)
}
