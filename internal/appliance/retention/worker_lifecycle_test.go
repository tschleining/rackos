package retention

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"testing"
	"time"
)

// startLifecycleWorker starts a worker in a separate
// goroutine and registers cleanup to ensure it stops
// even when a test fails.
func startLifecycleWorker(
	t *testing.T,
	worker *Worker,
) (context.CancelFunc, <-chan error) {
	t.Helper()

	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	result := make(chan error, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)

		result <- worker.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("retention worker did not stop")
		}
	})

	return cancel, result
}

func awaitLifecycleSignal(
	t *testing.T,
	signal <-chan struct{},
) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for worker")
	}
}

func awaitLifecycleResult(
	t *testing.T,
	result <-chan error,
) {
	t.Helper()

	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}

	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for worker shutdown")
	}
}

func TestWorkerLifecycleCanceledBeforeStartup(t *testing.T) {
	deleter := batchDeleterFunc(
		func(
			context.Context,
			time.Time,
			int,
		) (int64, error) {
			t.Fatal("canceled worker attempted deletion")
			return 0, nil
		},
	)

	worker := newTestRetentionWorker(
		t,
		testRetentionWorkerPolicy(),
		deleter,
	)

	worker.now = func() time.Time {
		t.Fatal("canceled worker accessed clock")
		return time.Time{}
	}

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}

	if worker.running.Load() {
		t.Fatal("canceled worker remained running")
	}
}

func TestWorkerLifecycleCancelActiveDeletion(t *testing.T) {
	policy := testRetentionWorkerPolicy()

	entered := make(chan struct{}, 1)
	batchErrors := make(chan error, 1)

	calls := 0

	deleter := batchDeleterFunc(
		func(
			ctx context.Context,
			_ time.Time,
			_ int,
		) (int64, error) {
			calls++

			entered <- struct{}{}

			<-ctx.Done()

			batchErrors <- ctx.Err()

			return 0, ctx.Err()
		},
	)

	worker := newTestRetentionWorker(
		t,
		policy,
		deleter,
	)

	cancel, result := startLifecycleWorker(
		t,
		worker,
	)

	awaitLifecycleSignal(t, entered)

	cancel()

	awaitLifecycleResult(t, result)

	select {
	case err := <-batchErrors:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf(
				"expected batch cancellation, got %v",
				err,
			)
		}

	default:
		t.Fatal("active batch did not receive cancellation")
	}

	if calls != 1 {
		t.Fatalf("expected one batch, got %d", calls)
	}

	if worker.running.Load() {
		t.Fatal("worker remained running after cancellation")
	}
}

func TestWorkerLifecycleCancelDuringWait(t *testing.T) {
	tests := []struct {
		name          string
		deletionCount int64
		expectedDelay time.Duration
	}{
		{
			name:          "between batches",
			deletionCount: 3,
			expectedDelay: MaxBatchDelay,
		},
		{
			name:          "between cycles",
			deletionCount: 0,
			expectedDelay: MinCleanupInterval,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := testRetentionWorkerPolicy()

			policy.BatchDelay = MaxBatchDelay
			policy.CleanupInterval = MinCleanupInterval

			calls := 0

			deleter := batchDeleterFunc(
				func(
					context.Context,
					time.Time,
					int,
				) (int64, error) {
					calls++

					return tt.deletionCount, nil
				},
			)

			worker := newTestRetentionWorker(
				t,
				policy,
				deleter,
			)

			waitStarted := make(chan time.Duration, 1)

			worker.wait = func(
				ctx context.Context,
				duration time.Duration,
			) error {
				waitStarted <- duration

				return waitForDuration(
					ctx,
					duration,
				)
			}

			cancel, result :=
				startLifecycleWorker(t, worker)

			select {
			case duration := <-waitStarted:
				if duration != tt.expectedDelay {
					t.Fatalf(
						"expected delay %s, got %s",
						tt.expectedDelay,
						duration,
					)
				}

			case <-time.After(5 * time.Second):
				t.Fatal("worker did not enter wait")
			}

			cancel()

			awaitLifecycleResult(t, result)

			if calls != 1 {
				t.Fatalf(
					"unexpected deletion count: %d calls",
					calls,
				)
			}

			if worker.running.Load() {
				t.Fatal("worker did not shut down")
			}
		})
	}
}

func TestWorkerLifecycleBatchTimeout(t *testing.T) {
	policy := testRetentionWorkerPolicy()

	policy.BatchTimeout = MinBatchTimeout

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	calls := 0

	deleter := batchDeleterFunc(
		func(
			batchCtx context.Context,
			_ time.Time,
			_ int,
		) (int64, error) {
			calls++

			deadline, exists := batchCtx.Deadline()
			if !exists {
				t.Fatal("batch context has no deadline")
			}

			parentDeadline, _ := ctx.Deadline()

			if !deadline.Before(parentDeadline) {
				t.Fatal("batch did not receive its own timeout")
			}

			<-batchCtx.Done()

			return 0, batchCtx.Err()
		},
	)

	worker := newTestRetentionWorker(
		t,
		policy,
		deleter,
	)

	result, err := worker.runCycle(ctx)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf(
			"expected batch timeout, got %v",
			err,
		)
	}

	if ctx.Err() != nil {
		t.Fatal("batch timeout canceled parent context")
	}

	if calls != 1 {
		t.Fatal("worker continued after batch timeout")
	}

	if result.Deleted != 0 ||
		result.Batches != 0 ||
		result.BatchLimitReached {
		t.Fatalf(
			"timed-out batch reported success: %+v",
			result,
		)
	}
}

func TestWorkerLifecycleFailureRecovery(t *testing.T) {
	policy := testRetentionWorkerPolicy()

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	injectedError := errors.New(
		"injected temporary database failure",
	)

	var cutoffs []time.Time
	var waits []time.Duration

	calls := 0
	clockCalls := 0

	deleter := batchDeleterFunc(
		func(
			_ context.Context,
			cutoff time.Time,
			limit int,
		) (int64, error) {
			cutoffs = append(cutoffs, cutoff)
			calls++

			switch calls {
			case 1:
				return int64(limit), nil

			case 2:
				return 0, injectedError

			case 3:
				return 1, nil

			default:
				t.Fatal("unexpected cleanup batch")
				return 0, nil
			}
		},
	)

	worker := newTestRetentionWorker(
		t,
		policy,
		deleter,
	)

	worker.now = func() time.Time {
		now := testRetentionWorkerTime().Add(
			time.Duration(clockCalls) * time.Hour,
		)

		clockCalls++

		return now
	}

	var logs bytes.Buffer

	worker.logger = slog.New(
		slog.NewJSONHandler(
			&logs,
			&slog.HandlerOptions{
				Level: slog.LevelDebug,
			},
		),
	)

	intervals := 0

	worker.wait = func(
		ctx context.Context,
		duration time.Duration,
	) error {
		waits = append(waits, duration)

		if duration == policy.CleanupInterval {
			intervals++

			if intervals == 2 {
				cancel()
			}
		}

		return ctx.Err()
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}

	if calls != 3 || clockCalls != 2 {
		t.Fatal("incorrect cleanup recovery behavior")
	}

	firstCutoff, err := policy.Cutoff(
		testRetentionWorkerTime(),
	)
	if err != nil {
		t.Fatal(err)
	}

	secondCutoff, err := policy.Cutoff(
		testRetentionWorkerTime().Add(time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}

	expectedCutoffs := []time.Time{
		firstCutoff,
		firstCutoff,
		secondCutoff,
	}

	if !reflect.DeepEqual(
		cutoffs,
		expectedCutoffs,
	) {
		t.Fatalf("incorrect cycle cutoffs: %v", cutoffs)
	}

	expectedWaits := []time.Duration{
		policy.BatchDelay,
		policy.CleanupInterval,
		policy.CleanupInterval,
	}

	if !reflect.DeepEqual(waits, expectedWaits) {
		t.Fatalf("incorrect recovery scheduling: %v", waits)
	}

	entries := decodeLifecycleLogs(
		t,
		logs.Bytes(),
	)

	if len(entries) != 4 {
		t.Fatalf(
			"expected four log entries, got %d",
			len(entries),
		)
	}

	if entries[0].Message !=
		"Performance retention worker started" {
		t.Fatal("missing startup log")
	}

	failure := entries[1]

	if failure.Level != "WARN" ||
		failure.Message !=
			"Performance retention cleanup failed" {
		t.Fatal("missing cleanup failure warning")
	}

	if failure.DeletedSamples !=
		int64(policy.BatchSize) ||
		failure.CompletedBatches != 1 {
		t.Fatal("failure log omitted committed deletions")
	}

	if failure.Error !=
		"retention batch 2: injected temporary database failure" {
		t.Fatalf(
			"incorrect failure log: %s",
			failure.Error,
		)
	}

	success := entries[2]

	if success.Level != "INFO" ||
		success.Message !=
			"Performance retention cleanup completed" ||
		success.DeletedSamples != 1 ||
		success.CompletedBatches != 1 {
		t.Fatal("incorrect recovery success log")
	}

	if entries[3].Message !=
		"Performance retention worker stopped" {
		t.Fatal("missing shutdown log")
	}

	if worker.running.Load() {
		t.Fatal("worker remained running")
	}
}

func TestWorkerLifecycleInvalidClockRecovery(t *testing.T) {
	policy := testRetentionWorkerPolicy()

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	calls := 0
	clockCalls := 0
	waits := 0

	deleter := batchDeleterFunc(
		func(
			context.Context,
			time.Time,
			int,
		) (int64, error) {
			calls++

			return 0, nil
		},
	)

	worker := newTestRetentionWorker(
		t,
		policy,
		deleter,
	)

	worker.now = func() time.Time {
		clockCalls++

		if clockCalls == 1 {
			return time.Time{}
		}

		return testRetentionWorkerTime()
	}

	worker.wait = func(
		ctx context.Context,
		duration time.Duration,
	) error {
		if duration != policy.CleanupInterval {
			t.Fatal("incorrect recovery interval")
		}

		waits++

		if waits == 1 && calls != 0 {
			t.Fatal("invalid clock permitted deletion")
		}

		if waits == 2 {
			cancel()
		}

		return ctx.Err()
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}

	if clockCalls != 2 ||
		waits != 2 ||
		calls != 1 {
		t.Fatal("worker did not recover after clock correction")
	}
}

func TestWorkerLifecycleConcurrentRunAndRestart(t *testing.T) {
	policy := testRetentionWorkerPolicy()

	entered := make(chan struct{}, 4)

	deleter := batchDeleterFunc(
		func(
			ctx context.Context,
			_ time.Time,
			_ int,
		) (int64, error) {
			entered <- struct{}{}

			<-ctx.Done()

			return 0, ctx.Err()
		},
	)

	worker := newTestRetentionWorker(
		t,
		policy,
		deleter,
	)

	cancel, result := startLifecycleWorker(
		t,
		worker,
	)

	awaitLifecycleSignal(t, entered)

	if !worker.running.Load() {
		t.Fatal("worker is not marked as running")
	}

	duplicateCtx, duplicateCancel :=
		context.WithTimeout(
			context.Background(),
			100*time.Millisecond,
		)
	defer duplicateCancel()

	err := worker.Run(duplicateCtx)

	if !errors.Is(err, ErrWorkerRunning) {
		t.Fatalf(
			"expected concurrent execution error, got %v",
			err,
		)
	}

	select {
	case <-entered:
		t.Fatal("duplicate worker attempted deletion")

	default:
	}

	cancel()

	awaitLifecycleResult(t, result)

	if worker.running.Load() {
		t.Fatal("worker running flag was not cleared")
	}

	// The same worker may be started again after
	// its previous Run invocation has completed.
	restartCancel, restartResult :=
		startLifecycleWorker(t, worker)

	awaitLifecycleSignal(t, entered)

	restartCancel()

	awaitLifecycleResult(t, restartResult)

	if worker.running.Load() {
		t.Fatal("restarted worker did not shut down")
	}
}

func TestWorkerLifecycleUnexpectedWaitFailure(t *testing.T) {
	policy := testRetentionWorkerPolicy()

	injectedError := errors.New(
		"injected scheduler failure",
	)

	deleter := batchDeleterFunc(
		func(
			context.Context,
			time.Time,
			int,
		) (int64, error) {
			return 0, nil
		},
	)

	worker := newTestRetentionWorker(
		t,
		policy,
		deleter,
	)

	worker.wait = func(
		context.Context,
		time.Duration,
	) error {
		return injectedError
	}

	err := worker.Run(context.Background())

	if !errors.Is(err, injectedError) {
		t.Fatalf("unexpected worker error: %v", err)
	}

	if worker.running.Load() {
		t.Fatal("failed worker remained marked as running")
	}
}

type lifecycleLogEntry struct {
	Level             string `json:"level"`
	Message           string `json:"msg"`
	Error             string `json:"error"`
	DeletedSamples    int64  `json:"deleted_samples"`
	CompletedBatches  int    `json:"completed_batches"`
	BatchLimitReached bool   `json:"batch_limit_reached"`
}

func decodeLifecycleLogs(
	t *testing.T,
	data []byte,
) []lifecycleLogEntry {
	t.Helper()

	var entries []lifecycleLogEntry

	for _, line := range bytes.Split(
		bytes.TrimSpace(data),
		[]byte("\n"),
	) {
		if len(line) == 0 {
			continue
		}

		var entry lifecycleLogEntry

		if err := json.Unmarshal(
			line,
			&entry,
		); err != nil {
			t.Fatal(err)
		}

		entries = append(entries, entry)
	}

	return entries
}

func TestWorkerLifecycleCycleLogging(t *testing.T) {
	worker, err := NewWorker(
		DefaultPolicy(),
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		result  cycleResult
		err     error
		level   string
		message string
	}{
		{
			name: "empty cleanup",
			result: cycleResult{
				Batches: 1,
			},
			level: "DEBUG",
			message: "Performance retention cleanup " +
				"found no expired samples",
		},
		{
			name: "successful cleanup",
			result: cycleResult{
				Deleted: 10,
				Batches: 2,
			},
			level: "INFO",
			message: "Performance retention cleanup " +
				"completed",
		},
		{
			name: "cycle limit reached",
			result: cycleResult{
				Deleted:           10000,
				Batches:           20,
				BatchLimitReached: true,
			},
			level: "INFO",
			message: "Performance retention cleanup " +
				"completed",
		},
		{
			name: "failed cleanup",
			result: cycleResult{
				Deleted: 500,
				Batches: 1,
			},
			err:   errors.New("database unavailable"),
			level: "WARN",
			message: "Performance retention cleanup " +
				"failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer

			worker.logger = slog.New(
				slog.NewJSONHandler(
					&output,
					&slog.HandlerOptions{
						Level: slog.LevelDebug,
					},
				),
			)

			worker.logCycle(tt.result, tt.err)

			entries := decodeLifecycleLogs(
				t,
				output.Bytes(),
			)

			if len(entries) != 1 {
				t.Fatal("incorrect number of log entries")
			}

			entry := entries[0]

			if entry.Level != tt.level ||
				entry.Message != tt.message {
				t.Fatalf(
					"incorrect log entry: %+v",
					entry,
				)
			}

			if entry.DeletedSamples !=
				tt.result.Deleted ||
				entry.CompletedBatches !=
					tt.result.Batches ||
				entry.BatchLimitReached !=
					tt.result.BatchLimitReached {
				t.Fatal("incorrect logged cycle statistics")
			}

			if tt.err != nil {
				if entry.Error != tt.err.Error() {
					t.Fatal("incorrect logged error")
				}
			} else if entry.Error != "" {
				t.Fatal("successful cycle logged an error")
			}
		})
	}
}

func TestWorkerLifecycleWaitForDuration(t *testing.T) {
	t.Run("completed timer", func(t *testing.T) {
		err := waitForDuration(
			context.Background(),
			time.Millisecond,
		)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("already canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(
			context.Background(),
		)
		cancel()

		err := waitForDuration(
			ctx,
			time.Hour,
		)

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected wait error: %v", err)
		}
	})

	t.Run("context deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Millisecond,
		)
		defer cancel()

		err := waitForDuration(
			ctx,
			time.Hour,
		)

		if !errors.Is(
			err,
			context.DeadlineExceeded,
		) {
			t.Fatalf("unexpected wait error: %v", err)
		}
	})
}
