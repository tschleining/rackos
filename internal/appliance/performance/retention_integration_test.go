package performance

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/retention"

	"google.golang.org/protobuf/proto"
)

func TestPerformanceRetentionIntegration(t *testing.T) {
	ctx, db, repository, dir :=
		setupPerformanceRepository(t)

	now := time.Now().UTC().
		Truncate(time.Millisecond)

	policy := retention.DefaultPolicy()

	policy.RetentionDuration =
		retention.MinRetentionDuration

	policy.BatchSize = 2
	policy.MaxBatchesPerCycle = 5
	policy.BatchDelay = retention.MinBatchDelay

	expiredReceipt := now.Add(-48 * time.Hour)
	recentReceipt := now.Add(-time.Hour)

	var expired []*rackosv1.ReportPerformanceRequest
	var retained []*rackosv1.ReportPerformanceRequest

	// Three expired records across two devices.
	for i, character := range []string{
		"a", "b", "c",
	} {
		deviceID := repositoryDeviceID

		if i == 2 {
			deviceID = repositoryOtherDeviceID
		}

		request := testQueryPerformanceRequest(
			deviceID,
			strings.Repeat(character, 32),
			expiredReceipt.Add(-time.Second),
		)

		requirePerformanceInsert(
			t,
			ctx,
			repository,
			request,
			expiredReceipt,
		)

		expired = append(expired, request)
	}

	// Two recently received reports with older
	// collection timestamps must be preserved.
	for i, character := range []string{"d", "e"} {
		request := testQueryPerformanceRequest(
			repositoryDeviceID,
			strings.Repeat(character, 32),
			now.Add(
				-time.Duration(72+i)*time.Hour,
			),
		)

		requirePerformanceInsert(
			t,
			ctx,
			repository,
			request,
			recentReceipt,
		)

		retained = append(retained, request)
	}

	before, err := repository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if before.TotalSamples != 5 ||
		before.ReportingDevices != 2 {
		t.Fatalf(
			"incorrect initial statistics: %+v",
			before,
		)
	}

	// Capture a historical cursor pointing to
	// a record that retention will remove.
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
		t.Fatal("missing initial history cursor")
	}

	if first.NextCursor.SampleID != expired[0].SampleId {
		t.Fatal("unexpected initial cursor position")
	}

	options.After = first.NextCursor

	logger := slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)

	worker, err := retention.NewWorker(
		policy,
		repository,
		logger,
	)
	if err != nil {
		t.Fatal(err)
	}

	workerCtx, cancel := context.WithCancel(ctx)

	done := make(chan struct{})

	var workerErr error

	go func() {
		defer close(done)

		workerErr = worker.Run(workerCtx)
	}()

	// Guarantee worker shutdown before database
	// cleanup, even when an assertion fails.
	defer func() {
		cancel()
		<-done
	}()

	ticker := time.NewTicker(
		10 * time.Millisecond,
	)
	defer ticker.Stop()

	// Wait for the actual background worker
	// to complete its bounded deletion batches.
	for {
		statistics, err :=
			repository.Statistics(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if statistics.TotalSamples == 2 {
			break
		}

		select {
		case <-ctx.Done():
			t.Fatal("retention cleanup timed out")

		case <-done:
			t.Fatalf(
				"worker stopped unexpectedly: %v",
				workerErr,
			)

		case <-ticker.C:
		}
	}

	cancel()
	<-done

	if workerErr != nil {
		t.Fatal(workerErr)
	}

	for _, request := range expired {
		requireRetentionSampleDeleted(
			t,
			ctx,
			repository,
			request,
		)
	}

	var expectedBytes int64

	for _, request := range retained {
		requirePerformanceRecord(
			t,
			ctx,
			repository,
			request,
			recentReceipt,
		)

		expectedBytes += int64(
			proto.Size(request.Snapshot),
		)
	}

	after, err := repository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if after.TotalSamples != 2 ||
		after.ReportingDevices != 1 ||
		after.PayloadBytes != expectedBytes {
		t.Fatalf(
			"incorrect post-retention statistics: %+v",
			after,
		)
	}

	requireStorageStatisticsTimestamp(
		t,
		after.OldestReceivedAt,
		recentReceipt,
	)

	requireStorageStatisticsTimestamp(
		t,
		after.LatestReceivedAt,
		recentReceipt,
	)

	// A cursor remains valid even after its
	// original record has been deleted.
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
		t.Fatal("incorrect history after retention")
	}

	for i, request := range retained {
		requireQueryPerformanceRecord(
			t,
			second.Samples[i],
			request,
			recentReceipt,
		)
	}

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
		retained[0],
		recentReceipt,
	)

	// The second device has lost all historical
	// samples but remains registered.
	_, err = repository.Latest(
		ctx,
		repositoryOtherDeviceID,
	)

	requirePerformanceRepositoryError(
		t,
		err,
		ErrNotFound,
	)

	empty, err := repository.History(
		ctx,
		repositoryOtherDeviceID,
		HistoryQuery{
			Limit: DefaultHistoryLimit,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if empty.Samples == nil ||
		len(empty.Samples) != 0 ||
		empty.HasMore() {
		t.Fatal("incorrect empty historical response")
	}

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

	// Inspect storage through a separate
	// read-only connection while SQLite is open.
	reader, err := database.OpenReadOnly(
		ctx,
		dir,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	readOnlyStatistics, err :=
		NewRepository(reader).Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(
		after,
		readOnlyStatistics,
	) {
		t.Fatal("read-only statistics differ from repository")
	}

	usage, err := database.InspectStorage(
		ctx,
		reader,
	)
	if err != nil {
		t.Fatal(err)
	}

	if usage.AllocatedBytes !=
		usage.ReusableBytes+usage.NonFreelistBytes {
		t.Fatal("inconsistent SQLite storage capacity")
	}

	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Verify historical persistence after
	// closing and reopening the database.
	reopened, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	reopenedRepository := NewRepository(reopened)

	persisted, err :=
		reopenedRepository.Statistics(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(after, persisted) {
		t.Fatal("storage statistics changed after restart")
	}

	for _, request := range expired {
		requireRetentionSampleDeleted(
			t,
			ctx,
			reopenedRepository,
			request,
		)
	}

	for _, request := range retained {
		requirePerformanceRecord(
			t,
			ctx,
			reopenedRepository,
			request,
			recentReceipt,
		)
	}

	requirePerformanceRowCount(
		t,
		ctx,
		reopened,
		2,
	)

	var integrity string

	err = reopened.QueryRowContext(
		ctx,
		"PRAGMA integrity_check",
	).Scan(&integrity)
	if err != nil {
		t.Fatal(err)
	}

	if integrity != "ok" {
		t.Fatalf(
			"SQLite integrity check failed: %s",
			integrity,
		)
	}
}
