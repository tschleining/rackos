package performance

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/database"
)

func TestPerformanceQueryRegressionInvalidStoredRecords(
	t *testing.T,
) {
	tests := []struct {
		name     string
		update   string
		expected error
	}{
		{
			name: "corrupt payload",
			update: `
				UPDATE device_performance
				SET payload = X'FF'
				WHERE device_id = ?
					AND sample_id = ?
			`,
			expected: ErrCorruptPerformance,
		},
		{
			name: "unsupported payload version",
			update: `
				UPDATE device_performance
				SET payload_version = 999
				WHERE device_id = ?
					AND sample_id = ?
			`,
			expected: ErrUnsupportedPayloadVersion,
		},
		{
			name: "collection timestamp mismatch",
			update: `
				UPDATE device_performance
				SET collected_at = collected_at + 1
				WHERE device_id = ?
					AND sample_id = ?
			`,
			expected: ErrCorruptPerformance,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, db, repository, _ :=
				setupPerformanceRepository(t)

			_, requests := setupHistoryQuerySamples(
				t,
				ctx,
				repository,
			)

			// Corrupt a record in the middle of
			// the requested historical page.
			target := requests[3]

			_, err := db.ExecContext(
				ctx,
				tt.update,
				target.DeviceId,
				target.SampleId,
			)
			if err != nil {
				t.Fatal(err)
			}

			page, err := repository.History(
				ctx,
				repositoryDeviceID,
				HistoryQuery{
					Limit: DefaultHistoryLimit,
				},
			)

			requirePerformanceRepositoryError(
				t,
				err,
				tt.expected,
			)

			// A failed query must not return a
			// partially populated page.
			if page.Samples != nil ||
				page.NextCursor != nil {
				t.Fatal("failed query returned partial results")
			}

			// Retrieval failures must not modify
			// or delete historical records.
			requirePerformanceRowCount(
				t,
				ctx,
				db,
				6,
			)
		})
	}
}

func TestPerformanceQueryRegressionCorruptLookahead(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	_, requests := setupHistoryQuerySamples(
		t,
		ctx,
		repository,
	)

	// With a limit of two, requests[3] is
	// the first page's lookahead record.
	target := requests[3]

	_, err := db.ExecContext(ctx, `
		UPDATE device_performance
		SET payload = X'FF'
		WHERE device_id = ?
			AND sample_id = ?
	`,
		target.DeviceId,
		target.SampleId,
	)
	if err != nil {
		t.Fatal(err)
	}

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
		t.Fatal("incorrect first page")
	}

	if first.Samples[0].SampleID != requests[5].SampleId ||
		first.Samples[1].SampleID != requests[4].SampleId {
		t.Fatal("incorrect first page ordering")
	}

	// The lookahead payload is decoded only
	// when its actual page is requested.
	options.After = first.NextCursor

	second, err := repository.History(
		ctx,
		repositoryDeviceID,
		options,
	)

	requirePerformanceRepositoryError(
		t,
		err,
		ErrCorruptPerformance,
	)

	if second.Samples != nil ||
		second.NextCursor != nil {
		t.Fatal("corrupt continuation returned partial results")
	}
}

func TestPerformanceQueryRegressionMaximumPage(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	total := MaxHistoryLimit + 1

	for i := 1; i <= total; i++ {
		request := testQueryPerformanceRequest(
			repositoryDeviceID,
			fmt.Sprintf("%032x", i),
			now.Add(
				-time.Duration(total+1-i)*
					time.Minute,
			),
		)

		requirePerformanceInsert(
			t,
			ctx,
			repository,
			request,
			now,
		)
	}

	options := HistoryQuery{
		Limit: MaxHistoryLimit,
	}

	first, err := repository.History(
		ctx,
		repositoryDeviceID,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(first.Samples) != MaxHistoryLimit {
		t.Fatal("maximum page size was not respected")
	}

	if !first.HasMore() {
		t.Fatal("missing continuation cursor")
	}

	for i, sample := range first.Samples {
		expected := fmt.Sprintf(
			"%032x",
			total-i,
		)

		if sample.SampleID != expected {
			t.Fatal("incorrect maximum page ordering")
		}
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

	if len(second.Samples) != 1 ||
		second.HasMore() {
		t.Fatal("incorrect final page")
	}

	if second.Samples[0].SampleID != fmt.Sprintf("%032x", 1) {
		t.Fatal("incorrect final sample")
	}

	requirePerformanceRowCount(
		t,
		ctx,
		db,
		total,
	)
}

func TestPerformanceQueryRegressionTransactionVisibility(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	request := testPerformanceRequest(now)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	transactionRepository := repository.WithTx(tx)

	receipt := requirePerformanceInsert(
		t,
		ctx,
		transactionRepository,
		request,
		now,
	)

	latest, err := transactionRepository.Latest(
		ctx,
		request.DeviceId,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireQueryPerformanceRecord(
		t,
		latest,
		request,
		receipt,
	)

	page, err := transactionRepository.History(
		ctx,
		request.DeviceId,
		HistoryQuery{
			Limit: DefaultHistoryLimit,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(page.Samples) != 1 ||
		page.HasMore() {
		t.Fatal("transaction history is incorrect")
	}

	requireQueryPerformanceRecord(
		t,
		page.Samples[0],
		request,
		receipt,
	)

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// The transaction's historical record
	// must no longer be visible.
	_, err = repository.Latest(
		ctx,
		request.DeviceId,
	)

	requirePerformanceRepositoryError(
		t,
		err,
		ErrNotFound,
	)

	page, err = repository.History(
		ctx,
		request.DeviceId,
		HistoryQuery{
			Limit: DefaultHistoryLimit,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if page.Samples == nil ||
		len(page.Samples) != 0 ||
		page.HasMore() {
		t.Fatal("rolled back history remained visible")
	}

	requirePerformanceRowCount(
		t,
		ctx,
		db,
		0,
	)
}

func TestPerformanceQueryRegressionTransactionCommit(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	request := testPerformanceRequest(now)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	receipt := requirePerformanceInsert(
		t,
		ctx,
		repository.WithTx(tx),
		request,
		now,
	)

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	page, err := repository.History(
		ctx,
		request.DeviceId,
		HistoryQuery{
			Limit: DefaultHistoryLimit,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(page.Samples) != 1 ||
		page.HasMore() {
		t.Fatal("committed history is incorrect")
	}

	requireQueryPerformanceRecord(
		t,
		page.Samples[0],
		request,
		receipt,
	)

	latest, err := repository.Latest(
		ctx,
		request.DeviceId,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireQueryPerformanceRecord(
		t,
		latest,
		request,
		receipt,
	)
}

func TestPerformanceQueryRegressionDatabaseRestart(
	t *testing.T,
) {
	ctx, db, repository, dir :=
		setupPerformanceRepository(t)

	_, requests := setupHistoryQuerySamples(
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
		t.Fatal("incorrect initial historical page")
	}

	// Preserve the original cursor across
	// closing and reopening the database.
	options.After = first.NextCursor

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	repository = NewRepository(reopened)

	receipt := testPerformanceTime().Truncate(
		time.Millisecond,
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
		requests[5],
		receipt,
	)

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
		t.Fatal("incorrect page after restart")
	}

	requireQueryPerformanceRecord(
		t,
		second.Samples[0],
		requests[3],
		receipt,
	)

	requireQueryPerformanceRecord(
		t,
		second.Samples[1],
		requests[2],
		receipt,
	)

	options.After = second.NextCursor

	final, err := repository.History(
		ctx,
		repositoryDeviceID,
		options,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(final.Samples) != 2 ||
		final.HasMore() {
		t.Fatal("incorrect final page after restart")
	}

	requireQueryPerformanceRecord(
		t,
		final.Samples[0],
		requests[1],
		receipt,
	)

	requireQueryPerformanceRecord(
		t,
		final.Samples[1],
		requests[0],
		receipt,
	)

	requirePerformanceRowCount(
		t,
		ctx,
		reopened,
		6,
	)
}

func TestPerformanceQueryRegressionDeletedCursor(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	_, requests := setupHistoryQuerySamples(
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
		t.Fatal("missing continuation cursor")
	}

	options.After = first.NextCursor

	// Simulate future retention cleanup removing
	// the historical record used as a cursor.
	result, err := db.ExecContext(ctx, `
		DELETE FROM device_performance
		WHERE device_id = ?
			AND sample_id = ?
	`,
		repositoryDeviceID,
		options.After.SampleID,
	)
	if err != nil {
		t.Fatal(err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}

	if affected != 1 {
		t.Fatal("cursor record was not deleted")
	}

	// A keyset cursor represents an ordering
	// position, not a required existing row.
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
		t.Fatal("incorrect page after cursor deletion")
	}

	if second.Samples[0].SampleID != requests[3].SampleId ||
		second.Samples[1].SampleID != requests[2].SampleId {
		t.Fatal("cursor deletion affected historical ordering")
	}

	requirePerformanceRowCount(
		t,
		ctx,
		db,
		5,
	)
}

func TestPerformanceQueryRegressionDatabaseFailure(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	latest, err := repository.Latest(
		ctx,
		repositoryDeviceID,
	)

	if err == nil {
		t.Fatal("closed database returned successful latest result")
	}

	if latest.Snapshot != nil {
		t.Fatal("failed latest query returned a snapshot")
	}

	page, err := repository.History(
		ctx,
		repositoryDeviceID,
		HistoryQuery{
			Limit: DefaultHistoryLimit,
		},
	)

	if err == nil {
		t.Fatal("closed database returned successful history")
	}

	if page.Samples != nil ||
		page.NextCursor != nil {
		t.Fatal("failed history query returned results")
	}
}

func TestPerformanceQueryRegressionHistoricalIndex(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	base, requests := setupHistoryQuerySamples(
		t,
		ctx,
		repository,
	)

	tests := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name: "latest",
			query: `
				SELECT ` + performanceColumns + `
				FROM device_performance
				WHERE device_id = ?
				ORDER BY collected_at DESC,
					sample_id DESC
				LIMIT 1
			`,
			args: []any{
				repositoryDeviceID,
			},
		},
		{
			name: "bounded history",
			query: `
				SELECT ` + performanceColumns + `
				FROM device_performance
				WHERE device_id = ?
					AND collected_at >= ?
					AND collected_at < ?
				ORDER BY collected_at DESC,
					sample_id DESC
				LIMIT ?
			`,
			args: []any{
				repositoryDeviceID,
				base.UnixMilli(),
				base.Add(6 * time.Minute).UnixMilli(),
				DefaultHistoryLimit + 1,
			},
		},
		{
			name: "cursor continuation",
			query: `
				SELECT ` + performanceColumns + `
				FROM device_performance
				WHERE device_id = ?
					AND (collected_at, sample_id) < (?, ?)
				ORDER BY collected_at DESC,
					sample_id DESC
				LIMIT ?
			`,
			args: []any{
				repositoryDeviceID,
				requests[4].Snapshot.CollectedAt.
					AsTime().UnixMilli(),
				requests[4].SampleId,
				DefaultHistoryLimit + 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := db.QueryContext(
				ctx,
				"EXPLAIN QUERY PLAN "+tt.query,
				tt.args...,
			)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()

			var details []string

			for rows.Next() {
				var (
					id     int
					parent int
					unused int
					detail string
				)

				err := rows.Scan(
					&id,
					&parent,
					&unused,
					&detail,
				)
				if err != nil {
					t.Fatal(err)
				}

				details = append(
					details,
					detail,
				)
			}

			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}

			plan := strings.Join(
				details,
				"\n",
			)

			t.Logf("SQLite query plan:\n%s", plan)

			if !strings.Contains(
				plan,
				"idx_device_performance_history",
			) {
				t.Fatal("historical index was not used")
			}

			if strings.Contains(
				strings.ToUpper(plan),
				"TEMP B-TREE",
			) {
				t.Fatal("query requires temporary sorting")
			}
		})
	}
}
