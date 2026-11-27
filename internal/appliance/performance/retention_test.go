package performance

import (
	"context"
	"strings"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"

	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/retention"
)

// requireRetentionSampleDeleted verifies that a
// specific historical record no longer exists.
func requireRetentionSampleDeleted(
	t *testing.T,
	ctx context.Context,
	repository *Repository,
	request *rackosv1.ReportPerformanceRequest,
) {
	t.Helper()

	_, err := repository.Get(
		ctx,
		request.DeviceId,
		request.SampleId,
	)

	requirePerformanceRepositoryError(
		t,
		err,
		ErrNotFound,
	)
}

func TestPerformanceRetentionEmptyDatabase(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	deleted, err := repository.DeleteExpiredBatch(
		ctx,
		testPerformanceTime().Truncate(time.Millisecond),
		retention.DefaultBatchSize,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 0 {
		t.Fatalf("expected zero deletions, got %d", deleted)
	}

	requirePerformanceRowCount(t, ctx, db, 0)
}

func TestPerformanceRetentionReceiptBoundary(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	expired := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("a", 32),
		cutoff.Add(-time.Hour),
	)

	boundary := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("b", 32),
		cutoff.Add(-time.Hour),
	)

	retained := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("c", 32),
		cutoff.Add(-time.Hour),
	)

	expiredReceipt := cutoff.Add(-time.Millisecond)
	boundaryReceipt := cutoff
	retainedReceipt := cutoff.Add(time.Millisecond)

	requirePerformanceInsert(
		t, ctx, repository, expired, expiredReceipt,
	)

	requirePerformanceInsert(
		t, ctx, repository, boundary, boundaryReceipt,
	)

	requirePerformanceInsert(
		t, ctx, repository, retained, retainedReceipt,
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
		t.Fatalf("expected one deletion, got %d", deleted)
	}

	requireRetentionSampleDeleted(
		t, ctx, repository, expired,
	)

	requirePerformanceRecord(
		t, ctx, repository, boundary, boundaryReceipt,
	)

	requirePerformanceRecord(
		t, ctx, repository, retained, retainedReceipt,
	)

	requirePerformanceRowCount(t, ctx, db, 2)
}

func TestPerformanceRetentionReceiptNotCollection(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	// This sample was collected long ago but
	// received recently and must remain stored.
	delayed := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("a", 32),
		cutoff.Add(-90*24*time.Hour),
	)

	delayedReceipt := cutoff.Add(time.Hour)

	// This sample was collected more recently,
	// but its receipt timestamp has expired.
	expired := testQueryPerformanceRequest(
		repositoryDeviceID,
		strings.Repeat("b", 32),
		cutoff.Add(-2*time.Hour),
	)

	expiredReceipt := cutoff.Add(-time.Hour)

	requirePerformanceInsert(
		t, ctx, repository, delayed, delayedReceipt,
	)

	requirePerformanceInsert(
		t, ctx, repository, expired, expiredReceipt,
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
		t.Fatal("incorrect receipt-based deletion count")
	}

	requireRetentionSampleDeleted(
		t, ctx, repository, expired,
	)

	requirePerformanceRecord(
		t, ctx, repository, delayed, delayedReceipt,
	)

	requirePerformanceRowCount(t, ctx, db, 1)
}

func TestPerformanceRetentionBoundedBatches(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	requests := make(
		[]*rackosv1.ReportPerformanceRequest,
		0,
		6,
	)

	receipts := make([]time.Time, 0, 6)

	for i, character := range []string{
		"a", "b", "c", "d", "e", "f",
	} {
		receipt := cutoff.Add(
			time.Duration(i-5) * time.Minute,
		)

		request := testQueryPerformanceRequest(
			repositoryDeviceID,
			strings.Repeat(character, 32),
			receipt.Add(-time.Second),
		)

		requests = append(requests, request)
		receipts = append(receipts, receipt)
	}

	// Deliberately insert records out of order.
	for _, i := range []int{4, 0, 5, 2, 1, 3} {
		requirePerformanceInsert(
			t,
			ctx,
			repository,
			requests[i],
			receipts[i],
		)
	}

	requirePerformanceRowCount(t, ctx, db, 6)

	for _, tt := range []struct {
		wantDeleted   int64
		totalDeleted  int
		wantRemaining int
	}{
		{2, 2, 4},
		{2, 4, 2},
		{1, 5, 1},
		{0, 5, 1},
	} {
		deleted, err := repository.DeleteExpiredBatch(
			ctx,
			cutoff,
			2,
		)
		if err != nil {
			t.Fatal(err)
		}

		if deleted != tt.wantDeleted {
			t.Fatalf(
				"expected %d deletions, got %d",
				tt.wantDeleted,
				deleted,
			)
		}

		for i, request := range requests {
			if i < tt.totalDeleted {
				requireRetentionSampleDeleted(
					t, ctx, repository, request,
				)
				continue
			}

			requirePerformanceRecord(
				t,
				ctx,
				repository,
				request,
				receipts[i],
			)
		}

		requirePerformanceRowCount(
			t, ctx, db, tt.wantRemaining,
		)
	}
}

func TestPerformanceRetentionAcrossDevices(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	receipt := cutoff.Add(-time.Hour)

	sampleID := strings.Repeat("a", 32)

	first := testQueryPerformanceRequest(
		repositoryDeviceID,
		sampleID,
		receipt.Add(-time.Second),
	)

	second := testQueryPerformanceRequest(
		repositoryOtherDeviceID,
		sampleID,
		receipt.Add(-time.Second),
	)

	requirePerformanceInsert(
		t, ctx, repository, first, receipt,
	)

	requirePerformanceInsert(
		t, ctx, repository, second, receipt,
	)

	// Both records have the same receipt time
	// and sample identity. The older rowid must
	// be selected first.
	deleted, err := repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		1,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 1 {
		t.Fatal("incorrect first deletion count")
	}

	requireRetentionSampleDeleted(
		t, ctx, repository, first,
	)

	requirePerformanceRecord(
		t, ctx, repository, second, receipt,
	)

	deleted, err = repository.DeleteExpiredBatch(
		ctx,
		cutoff,
		1,
	)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 1 {
		t.Fatal("incorrect second deletion count")
	}

	requireRetentionSampleDeleted(
		t, ctx, repository, second,
	)

	requirePerformanceRowCount(t, ctx, db, 0)

	// Performance retention must not remove
	// the corresponding device registrations.
	deviceRepository := devices.NewRepository(db)

	for _, deviceID := range []string{
		repositoryDeviceID,
		repositoryOtherDeviceID,
	} {
		if _, err := deviceRepository.Get(
			ctx,
			deviceID,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPerformanceRetentionInvalidQuery(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	receipt := cutoff.Add(-time.Hour)

	request := testPerformanceRequest(receipt)

	requirePerformanceInsert(
		t, ctx, repository, request, receipt,
	)

	tests := []struct {
		name   string
		cutoff time.Time
		limit  int
	}{
		{
			"zero cutoff",
			time.Time{},
			10,
		},
		{
			"negative cutoff",
			time.UnixMilli(-1),
			10,
		},
		{
			"unix epoch",
			time.UnixMilli(0),
			10,
		},
		{
			"unsupported year",
			time.Date(
				10000, 1, 1,
				0, 0, 0, 0,
				time.UTC,
			),
			10,
		},
		{
			"sub-millisecond cutoff",
			cutoff.Add(time.Nanosecond),
			10,
		},
		{
			"zero limit",
			cutoff,
			0,
		},
		{
			"negative limit",
			cutoff,
			-1,
		},
		{
			"oversized batch",
			cutoff,
			retention.MaxBatchSize + 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deleted, err :=
				repository.DeleteExpiredBatch(
					ctx,
					tt.cutoff,
					tt.limit,
				)

			requirePerformanceRepositoryError(
				t,
				err,
				ErrInvalidRetentionQuery,
			)

			if deleted != 0 {
				t.Fatal("invalid query deleted records")
			}

			requirePerformanceRecord(
				t, ctx, repository, request, receipt,
			)

			requirePerformanceRowCount(
				t, ctx, db, 1,
			)
		})
	}
}

func TestPerformanceRetentionCanceledContext(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	receipt := cutoff.Add(-time.Hour)

	request := testPerformanceRequest(receipt)

	requirePerformanceInsert(
		t, ctx, repository, request, receipt,
	)

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	deleted, err := repository.DeleteExpiredBatch(
		canceled,
		cutoff,
		10,
	)

	requirePerformanceRepositoryError(
		t,
		err,
		context.Canceled,
	)

	if deleted != 0 {
		t.Fatal("canceled request deleted records")
	}

	requirePerformanceRecord(
		t, ctx, repository, request, receipt,
	)

	requirePerformanceRowCount(t, ctx, db, 1)
}

func TestPerformanceRetentionTransactionRollback(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	receipt := cutoff.Add(-time.Hour)

	request := testPerformanceRequest(receipt)

	requirePerformanceInsert(
		t, ctx, repository, request, receipt,
	)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	transactionRepository := repository.WithTx(tx)

	deleted, err :=
		transactionRepository.DeleteExpiredBatch(
			ctx,
			cutoff,
			10,
		)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 1 {
		t.Fatal("incorrect transactional deletion count")
	}

	requireRetentionSampleDeleted(
		t,
		ctx,
		transactionRepository,
		request,
	)

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	requirePerformanceRecord(
		t, ctx, repository, request, receipt,
	)

	requirePerformanceRowCount(t, ctx, db, 1)
}

func TestPerformanceRetentionTransactionCommit(
	t *testing.T,
) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	cutoff := testPerformanceTime().
		Truncate(time.Millisecond)

	receipt := cutoff.Add(-time.Hour)

	request := testPerformanceRequest(receipt)

	requirePerformanceInsert(
		t, ctx, repository, request, receipt,
	)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	deleted, err :=
		repository.WithTx(tx).DeleteExpiredBatch(
			ctx,
			cutoff,
			10,
		)
	if err != nil {
		t.Fatal(err)
	}

	if deleted != 1 {
		t.Fatal("incorrect transactional deletion count")
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	requireRetentionSampleDeleted(
		t, ctx, repository, request,
	)

	requirePerformanceRowCount(t, ctx, db, 0)
}
