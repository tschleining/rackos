package performance

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	repositoryDeviceID      = "e2a104d8-9c72-4b31-8fa2-63001b24c9e8"
	repositoryOtherDeviceID = "22222222-2222-4222-8222-222222222222"

	repositoryOtherSampleID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func setupPerformanceRepository(
	t *testing.T,
) (context.Context, *sql.DB, *Repository, string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	t.Cleanup(cancel)

	dir := t.TempDir()

	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	deviceRepository := devices.NewRepository(db)

	for _, deviceID := range []string{
		repositoryDeviceID,
		repositoryOtherDeviceID,
	} {
		_, err := deviceRepository.Create(
			ctx,
			deviceID,
			devices.DeviceInfo{
				Hostname:     "test-device.local",
				OS:           "linux",
				Architecture: "arm64",
				AgentVersion: "0.1.0-test",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	return ctx, db, NewRepository(db), dir
}

func requirePerformanceInsert(
	t *testing.T,
	ctx context.Context,
	repository *Repository,
	request *rackosv1.ReportPerformanceRequest,
	receivedAt time.Time,
) time.Time {
	t.Helper()

	acceptedAt, err := repository.Insert(
		ctx,
		request,
		receivedAt,
	)
	if err != nil {
		t.Fatal(err)
	}

	return acceptedAt
}

func requirePerformanceRecord(
	t *testing.T,
	ctx context.Context,
	repository *Repository,
	request *rackosv1.ReportPerformanceRequest,
	receivedAt time.Time,
) {
	t.Helper()

	actual, err := repository.Get(
		ctx,
		request.DeviceId,
		request.SampleId,
	)
	if err != nil {
		t.Fatal(err)
	}

	if actual.DeviceID != request.DeviceId ||
		actual.SampleID != request.SampleId {
		t.Fatal("stored sample identity changed")
	}

	if actual.PayloadVersion != CurrentPayloadVersion {
		t.Fatal("incorrect payload version")
	}

	if !actual.CollectedAt.Equal(
		request.Snapshot.CollectedAt.AsTime(),
	) {
		t.Fatal("collection timestamp changed")
	}

	expectedReceipt := time.UnixMilli(
		receivedAt.UnixMilli(),
	).UTC()

	if !actual.ReceivedAt.Equal(expectedReceipt) {
		t.Fatal("receipt timestamp changed")
	}

	if actual.CollectedAt.Location() != time.UTC ||
		actual.ReceivedAt.Location() != time.UTC {
		t.Fatal("stored timestamps are not UTC")
	}

	if !proto.Equal(
		actual.Snapshot,
		request.Snapshot,
	) {
		t.Fatal("stored snapshot differs from original")
	}
}

func requirePerformanceRowCount(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	expected int,
) {
	t.Helper()

	var actual int

	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM device_performance
	`).Scan(&actual)
	if err != nil {
		t.Fatal(err)
	}

	if actual != expected {
		t.Fatalf(
			"expected %d rows, got %d",
			expected,
			actual,
		)
	}
}

func requirePerformanceRepositoryError(
	t *testing.T,
	err error,
	expected error,
) {
	t.Helper()

	if !errors.Is(err, expected) {
		t.Fatalf(
			"expected %v, got %v",
			expected,
			err,
		)
	}
}

func TestPerformanceRepositoryInsertAndGet(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	request := testPerformanceRequest(now)

	before := proto.Clone(request)

	acceptedAt := requirePerformanceInsert(
		t, ctx, repository, request, now,
	)

	if !acceptedAt.Equal(now) {
		t.Fatal("incorrect receipt timestamp")
	}

	requirePerformanceRecord(
		t, ctx, repository, request, now,
	)

	requirePerformanceRowCount(t, ctx, db, 1)

	if !proto.Equal(request, before) {
		t.Fatal("repository modified the supplied request")
	}
}

func TestPerformanceRepositoryGetNotFound(t *testing.T) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	request := testPerformanceRequest(
		testPerformanceTime(),
	)

	_, err := repository.Get(
		ctx,
		request.DeviceId,
		request.SampleId,
	)

	requirePerformanceRepositoryError(
		t, err, ErrNotFound,
	)
}

func TestPerformanceRepositoryInvalidGet(t *testing.T) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	request := testPerformanceRequest(
		testPerformanceTime(),
	)

	for _, tt := range []struct {
		deviceID string
		sampleID string
	}{
		{"", request.SampleId},
		{" ", request.SampleId},
		{request.DeviceId, ""},
		{request.DeviceId, "invalid"},
	} {
		_, err := repository.Get(
			ctx,
			tt.deviceID,
			tt.sampleID,
		)

		requirePerformanceRepositoryError(
			t, err, ErrInvalidPerformance,
		)
	}
}

func TestPerformanceRepositoryHistoricalSamples(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	newer := testPerformanceRequest(now)

	older := testPerformanceRequest(
		now.Add(-time.Hour),
	)

	older.SampleId = repositoryOtherSampleID

	requirePerformanceInsert(
		t, ctx, repository, newer, now,
	)

	requirePerformanceInsert(
		t, ctx, repository, older, now,
	)

	requirePerformanceRecord(
		t, ctx, repository, newer, now,
	)

	requirePerformanceRecord(
		t, ctx, repository, older, now,
	)

	requirePerformanceRowCount(t, ctx, db, 2)
}

func TestPerformanceRepositorySameCollectionTime(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	first := testPerformanceRequest(now)
	second := testPerformanceRequest(now)

	second.SampleId = repositoryOtherSampleID
	second.Snapshot.Cpu.UsagePercent = 85

	requirePerformanceInsert(
		t, ctx, repository, first, now,
	)

	requirePerformanceInsert(
		t, ctx, repository, second, now,
	)

	requirePerformanceRecord(
		t, ctx, repository, first, now,
	)

	requirePerformanceRecord(
		t, ctx, repository, second, now,
	)

	requirePerformanceRowCount(t, ctx, db, 2)
}

func TestPerformanceRepositoryIdenticalRetry(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()
	request := testPerformanceRequest(now)

	originalReceipt := requirePerformanceInsert(
		t, ctx, repository, request, now,
	)

	var originalPayload []byte

	err := db.QueryRowContext(ctx, `
		SELECT payload FROM device_performance
		WHERE device_id = ? AND sample_id = ?
	`, request.DeviceId, request.SampleId).
		Scan(&originalPayload)
	if err != nil {
		t.Fatal(err)
	}

	retry := proto.Clone(request).(*rackosv1.ReportPerformanceRequest)

	retryReceipt := requirePerformanceInsert(
		t,
		ctx,
		repository,
		retry,
		now.Add(10*time.Minute),
	)

	if !retryReceipt.Equal(originalReceipt) {
		t.Fatal("retry did not preserve original receipt")
	}

	requirePerformanceRecord(
		t, ctx, repository, request, now,
	)

	requirePerformanceRowCount(t, ctx, db, 1)

	var actualPayload []byte

	err = db.QueryRowContext(ctx, `
		SELECT payload FROM device_performance
		WHERE device_id = ? AND sample_id = ?
	`, request.DeviceId, request.SampleId).
		Scan(&actualPayload)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(originalPayload, actualPayload) {
		t.Fatal("identical retry modified stored payload")
	}
}

func TestPerformanceRepositoryConflictingSample(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()
	original := testPerformanceRequest(now)

	requirePerformanceInsert(
		t, ctx, repository, original, now,
	)

	tests := []struct {
		name   string
		change func(*rackosv1.PerformanceSnapshot)
	}{
		{
			name: "different CPU utilization",
			change: func(s *rackosv1.PerformanceSnapshot) {
				s.Cpu.UsagePercent = 80
			},
		},
		{
			name: "different cumulative counter",
			change: func(s *rackosv1.PerformanceSnapshot) {
				s.Network.Interfaces[0].ReceivedBytes++
			},
		},
		{
			name: "different collection timestamp",
			change: func(s *rackosv1.PerformanceSnapshot) {
				s.CollectedAt = timestamppb.New(
					s.CollectedAt.AsTime().Add(
						time.Nanosecond,
					),
				)
			},
		},
		{
			name: "absent instead of explicit zero",
			change: func(s *rackosv1.PerformanceSnapshot) {
				s.Network.Interfaces[0].ReceiveDropped = nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conflicting := proto.Clone(original).(*rackosv1.ReportPerformanceRequest)

			tt.change(conflicting.Snapshot)

			acceptedAt, err := repository.Insert(
				ctx,
				conflicting,
				now.Add(time.Minute),
			)

			requirePerformanceRepositoryError(
				t, err, ErrPerformanceConflict,
			)

			if !acceptedAt.IsZero() {
				t.Fatal("conflicting sample returned receipt")
			}

			requirePerformanceRecord(
				t, ctx, repository, original, now,
			)

			requirePerformanceRowCount(t, ctx, db, 1)
		})
	}
}

func TestPerformanceRepositoryDeviceIsolation(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	first := testPerformanceRequest(now)
	second := testPerformanceRequest(now)

	second.DeviceId = repositoryOtherDeviceID
	second.Snapshot.Cpu.UsagePercent = 75

	// Both devices intentionally use the same sample ID.
	requirePerformanceInsert(
		t, ctx, repository, first, now,
	)

	requirePerformanceInsert(
		t, ctx, repository, second, now,
	)

	requirePerformanceRecord(
		t, ctx, repository, first, now,
	)

	requirePerformanceRecord(
		t, ctx, repository, second, now,
	)

	requirePerformanceRowCount(t, ctx, db, 2)
}

func TestPerformanceRepositoryInvalidRequest(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	tests := []struct {
		name   string
		change func(*rackosv1.ReportPerformanceRequest)
	}{
		{
			name: "missing device",
			change: func(r *rackosv1.ReportPerformanceRequest) {
				r.DeviceId = ""
			},
		},
		{
			name: "invalid sample",
			change: func(r *rackosv1.ReportPerformanceRequest) {
				r.SampleId = "invalid"
			},
		},
		{
			name: "missing snapshot",
			change: func(r *rackosv1.ReportPerformanceRequest) {
				r.Snapshot = nil
			},
		},
		{
			name: "invalid measurements",
			change: func(r *rackosv1.ReportPerformanceRequest) {
				r.Snapshot.Cpu.UsagePercent = math.NaN()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := testPerformanceRequest(now)
			tt.change(request)

			acceptedAt, err := repository.Insert(
				ctx,
				request,
				now,
			)

			requirePerformanceRepositoryError(
				t, err, ErrInvalidPerformance,
			)

			if !acceptedAt.IsZero() {
				t.Fatal("invalid request returned receipt")
			}

			requirePerformanceRowCount(t, ctx, db, 0)
		})
	}
}

func TestPerformanceRepositoryOversizedRequest(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()
	request := testPerformanceRequest(now)

	unknown := protowire.AppendTag(
		nil, 1000, protowire.BytesType,
	)

	unknown = protowire.AppendBytes(
		unknown,
		make([]byte, MaxRequestBytes),
	)

	request.Snapshot.ProtoReflect().SetUnknown(
		unknown,
	)

	_, err := repository.Insert(ctx, request, now)

	requirePerformanceRepositoryError(
		t, err, ErrPerformanceTooLarge,
	)

	requirePerformanceRowCount(t, ctx, db, 0)
}

func TestPerformanceRepositoryUnknownDevice(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()
	request := testPerformanceRequest(now)

	request.DeviceId =
		"33333333-3333-4333-8333-333333333333"

	acceptedAt, err := repository.Insert(
		ctx,
		request,
		now,
	)

	if err == nil {
		t.Fatal("unknown device was accepted")
	}

	if !acceptedAt.IsZero() {
		t.Fatal("unknown device returned receipt")
	}

	requirePerformanceRowCount(t, ctx, db, 0)
}

func TestPerformanceRepositoryInvalidReceipt(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	request := testPerformanceRequest(
		testPerformanceTime(),
	)

	for _, receivedAt := range []time.Time{
		{},
		time.Unix(0, 0),
		time.Unix(-1, 0),
		time.Unix(0, 1),
		time.Date(
			10000, 1, 1, 0, 0, 0, 0, time.UTC,
		),
	} {
		_, err := repository.Insert(
			ctx,
			request,
			receivedAt,
		)

		requirePerformanceRepositoryError(
			t, err, ErrInvalidPerformance,
		)
	}

	requirePerformanceRowCount(t, ctx, db, 0)
}

func TestPerformanceRepositoryTimestampPrecision(t *testing.T) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime().Add(
		987654321 * time.Nanosecond,
	)

	request := testPerformanceRequest(now)

	collectedAt := testPerformanceTime().
		Add(-time.Second).
		Add(123456789 * time.Nanosecond)

	request.Snapshot.CollectedAt = timestamppb.New(
		collectedAt,
	)

	acceptedAt := requirePerformanceInsert(
		t, ctx, repository, request, now,
	)

	expectedReceipt := time.UnixMilli(
		now.UnixMilli(),
	).UTC()

	if !acceptedAt.Equal(expectedReceipt) {
		t.Fatal("receipt timestamp was not normalized")
	}

	requirePerformanceRecord(
		t, ctx, repository, request, now,
	)
}

func TestPerformanceRepositoryFutureSkewBoundary(t *testing.T) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime().Add(
		999999 * time.Nanosecond,
	)

	request := testPerformanceRequest(now)

	request.Snapshot.CollectedAt = timestamppb.New(
		now.Add(maxFutureClockSkew),
	)

	requirePerformanceInsert(
		t, ctx, repository, request, now,
	)

	// Reading the stored snapshot must not fail
	// because receipt precision was reduced.
	requirePerformanceRecord(
		t, ctx, repository, request, now,
	)
}

func TestPerformanceRepositoryLargeCounters(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()
	request := testPerformanceRequest(now)

	snapshot := request.Snapshot

	snapshot.Memory.TotalBytes = math.MaxUint64
	snapshot.Memory.AvailableBytes = 0
	snapshot.Memory.UsedBytes = math.MaxUint64
	snapshot.Memory.UsagePercent = 100

	disk := snapshot.Storage.Disks[0]

	disk.ReadBytes = math.MaxUint64
	disk.WriteBytes = math.MaxUint64 - 1
	disk.ReadOperations = math.MaxUint64 - 2
	disk.WriteOperations = math.MaxUint64 - 3

	iface := snapshot.Network.Interfaces[0]

	iface.ReceivedBytes = math.MaxUint64
	iface.TransmittedBytes = math.MaxUint64 - 1

	iface.ReceiveDropped = testUint64Ptr(
		math.MaxUint64,
	)

	requirePerformanceInsert(
		t, ctx, repository, request, now,
	)

	requirePerformanceRecord(
		t, ctx, repository, request, now,
	)

	var storageType string

	err := db.QueryRowContext(ctx, `
		SELECT typeof(payload)
		FROM device_performance
	`).Scan(&storageType)
	if err != nil {
		t.Fatal(err)
	}

	if storageType != "blob" {
		t.Fatal("payload was not stored as a BLOB")
	}
}

func TestPerformanceRepositoryIndependentSnapshots(t *testing.T) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()
	request := testPerformanceRequest(now)

	original := proto.Clone(request).(*rackosv1.ReportPerformanceRequest)

	requirePerformanceInsert(
		t, ctx, repository, request, now,
	)

	// Mutating the original request must not modify
	// the stored historical record.
	request.Snapshot.Cpu.UsagePercent = 99

	*request.Snapshot.Network.Interfaces[0].
		ReceiveDropped = 999

	requirePerformanceRecord(
		t, ctx, repository, original, now,
	)

	record, err := repository.Get(
		ctx,
		original.DeviceId,
		original.SampleId,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Mutating a retrieved snapshot must not modify
	// subsequent repository reads.
	record.Snapshot.Cpu.UsagePercent = 10

	requirePerformanceRecord(
		t, ctx, repository, original, now,
	)
}

func TestPerformanceRepositoryUnknownFields(t *testing.T) {
	ctx, _, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()
	request := testPerformanceRequest(now)

	unknown := protowire.AppendTag(
		nil, 1000, protowire.VarintType,
	)
	unknown = protowire.AppendVarint(unknown, 42)

	request.Snapshot.ProtoReflect().SetUnknown(
		unknown,
	)

	requirePerformanceInsert(
		t, ctx, repository, request, now,
	)

	requirePerformanceRecord(
		t, ctx, repository, request, now,
	)

	// An identical retry must preserve unknown fields.
	requirePerformanceInsert(
		t,
		ctx,
		repository,
		request,
		now.Add(time.Minute),
	)
}

func TestPerformanceRepositoryTransactionRollback(t *testing.T) {
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

	requirePerformanceInsert(
		t,
		ctx,
		transactionRepository,
		request,
		now,
	)

	requirePerformanceRecord(
		t,
		ctx,
		transactionRepository,
		request,
		now,
	)

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	_, err = repository.Get(
		ctx,
		request.DeviceId,
		request.SampleId,
	)

	requirePerformanceRepositoryError(
		t, err, ErrNotFound,
	)

	requirePerformanceRowCount(t, ctx, db, 0)
}

func TestPerformanceRepositoryTransactionCommit(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()
	request := testPerformanceRequest(now)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	requirePerformanceInsert(
		t,
		ctx,
		repository.WithTx(tx),
		request,
		now,
	)

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	requirePerformanceRecord(
		t, ctx, repository, request, now,
	)

	requirePerformanceRowCount(t, ctx, db, 1)
}

func TestPerformanceRepositoryCanceledContext(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()
	request := testPerformanceRequest(now)

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	_, err := repository.Insert(
		canceled,
		request,
		now,
	)

	requirePerformanceRepositoryError(
		t, err, context.Canceled,
	)

	_, err = repository.Get(
		canceled,
		request.DeviceId,
		request.SampleId,
	)

	requirePerformanceRepositoryError(
		t, err, context.Canceled,
	)

	requirePerformanceRowCount(t, ctx, db, 0)
}

func TestPerformanceRepositoryConcurrentRetries(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()

	const workers = 16

	type result struct {
		receivedAt time.Time
		err        error
	}

	results := make(chan result, workers)

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)

		go func(index int) {
			defer wg.Done()

			request := testPerformanceRequest(now)

			receivedAt, err := repository.Insert(
				ctx,
				request,
				now.Add(
					time.Duration(index)*
						time.Millisecond,
				),
			)

			results <- result{
				receivedAt: receivedAt,
				err:        err,
			}
		}(i)
	}

	wg.Wait()
	close(results)

	var originalReceipt time.Time

	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}

		if originalReceipt.IsZero() {
			originalReceipt = result.receivedAt
		}

		if !result.receivedAt.Equal(originalReceipt) {
			t.Fatal("concurrent retries returned different receipts")
		}
	}

	if originalReceipt.IsZero() {
		t.Fatal("missing receipt timestamp")
	}

	requirePerformanceRowCount(t, ctx, db, 1)

	requirePerformanceRecord(
		t,
		ctx,
		repository,
		testPerformanceRequest(now),
		originalReceipt,
	)
}

func TestPerformanceRepositoryCorruptPayload(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()
	request := testPerformanceRequest(now)

	requirePerformanceInsert(
		t, ctx, repository, request, now,
	)

	_, err := db.ExecContext(ctx, `
		UPDATE device_performance
		SET payload = X'FF'
		WHERE device_id = ? AND sample_id = ?
	`, request.DeviceId, request.SampleId)
	if err != nil {
		t.Fatal(err)
	}

	_, err = repository.Get(
		ctx,
		request.DeviceId,
		request.SampleId,
	)

	requirePerformanceRepositoryError(
		t, err, ErrCorruptPerformance,
	)

	// A corrupted record must not be acknowledged
	// as a successful identical retry.
	_, err = repository.Insert(
		ctx,
		request,
		now.Add(time.Minute),
	)

	requirePerformanceRepositoryError(
		t, err, ErrCorruptPerformance,
	)
}

func TestPerformanceRepositoryTimestampMismatch(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()
	request := testPerformanceRequest(now)

	requirePerformanceInsert(
		t, ctx, repository, request, now,
	)

	_, err := db.ExecContext(ctx, `
		UPDATE device_performance
		SET collected_at = collected_at + 1
		WHERE device_id = ? AND sample_id = ?
	`, request.DeviceId, request.SampleId)
	if err != nil {
		t.Fatal(err)
	}

	_, err = repository.Get(
		ctx,
		request.DeviceId,
		request.SampleId,
	)

	requirePerformanceRepositoryError(
		t, err, ErrCorruptPerformance,
	)
}

func TestPerformanceRepositoryUnsupportedVersion(t *testing.T) {
	ctx, db, repository, _ :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()
	request := testPerformanceRequest(now)

	requirePerformanceInsert(
		t, ctx, repository, request, now,
	)

	_, err := db.ExecContext(ctx, `
		UPDATE device_performance
		SET payload_version = 999
		WHERE device_id = ? AND sample_id = ?
	`, request.DeviceId, request.SampleId)
	if err != nil {
		t.Fatal(err)
	}

	_, err = repository.Get(
		ctx,
		request.DeviceId,
		request.SampleId,
	)

	requirePerformanceRepositoryError(
		t, err, ErrUnsupportedPayloadVersion,
	)
}

func TestPerformanceRepositoryDatabaseReopen(t *testing.T) {
	ctx, db, repository, dir :=
		setupPerformanceRepository(t)

	now := testPerformanceTime()
	request := testPerformanceRequest(now)

	requirePerformanceInsert(
		t, ctx, repository, request, now,
	)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	repository = NewRepository(reopened)

	requirePerformanceRecord(
		t, ctx, repository, request, now,
	)

	// Idempotency must survive appliance restarts.
	acceptedAt := requirePerformanceInsert(
		t,
		ctx,
		repository,
		request,
		now.Add(time.Hour),
	)

	if !acceptedAt.Equal(now) {
		t.Fatal("restart changed original receipt timestamp")
	}

	requirePerformanceRowCount(
		t, ctx, reopened, 1,
	)
}
