package retention

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"
)

// batchDeleterFunc adapts a function to the
// BatchDeleter interface for isolated worker tests.
type batchDeleterFunc func(
	context.Context,
	time.Time,
	int,
) (int64, error)

func (f batchDeleterFunc) DeleteExpiredBatch(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) (int64, error) {
	return f(ctx, cutoff, limit)
}

func testRetentionWorkerPolicy() Policy {
	policy := DefaultPolicy()

	policy.RetentionDuration =
		RecommendedRetentionDuration

	policy.BatchSize = 3
	policy.MaxBatchesPerCycle = 5

	return policy
}

func testRetentionWorkerTime() time.Time {
	return time.Date(
		2026, 11, 27,
		12, 0, 0, 123456789,
		time.UTC,
	)
}

func testRetentionWorkerLogger() *slog.Logger {
	return slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)
}

func newTestRetentionWorker(
	t *testing.T,
	policy Policy,
	deleter BatchDeleter,
) *Worker {
	t.Helper()

	worker, err := NewWorker(
		policy,
		deleter,
		testRetentionWorkerLogger(),
	)
	if err != nil {
		t.Fatal(err)
	}

	worker.now = testRetentionWorkerTime

	// Eliminate real delays from the initial
	// worker behavior tests.
	worker.wait = func(
		ctx context.Context,
		_ time.Duration,
	) error {
		return ctx.Err()
	}

	return worker
}

func TestNewWorker(t *testing.T) {
	t.Run("disabled without repository", func(t *testing.T) {
		worker, err := NewWorker(
			DefaultPolicy(),
			nil,
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}

		if worker == nil {
			t.Fatal("missing disabled worker")
		}
	})

	t.Run("enabled without repository", func(t *testing.T) {
		worker, err := NewWorker(
			testRetentionWorkerPolicy(),
			nil,
			nil,
		)

		if err == nil || worker != nil {
			t.Fatal("enabled worker accepted missing repository")
		}
	})

	t.Run("invalid policy", func(t *testing.T) {
		policy := DefaultPolicy()
		policy.BatchSize = 0

		worker, err := NewWorker(
			policy,
			nil,
			nil,
		)

		if !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("unexpected error: %v", err)
		}

		if worker != nil {
			t.Fatal("invalid policy returned worker")
		}
	})

	t.Run("enabled with repository", func(t *testing.T) {
		deleter := batchDeleterFunc(
			func(
				context.Context,
				time.Time,
				int,
			) (int64, error) {
				t.Fatal("constructor performed deletion")
				return 0, nil
			},
		)

		worker, err := NewWorker(
			testRetentionWorkerPolicy(),
			deleter,
			testRetentionWorkerLogger(),
		)
		if err != nil {
			t.Fatal(err)
		}

		if worker == nil {
			t.Fatal("missing enabled worker")
		}
	})
}

func TestWorkerDisabled(t *testing.T) {
	deleter := batchDeleterFunc(
		func(
			context.Context,
			time.Time,
			int,
		) (int64, error) {
			t.Fatal("disabled worker attempted deletion")
			return 0, nil
		},
	)

	worker := newTestRetentionWorker(
		t,
		DefaultPolicy(),
		deleter,
	)

	worker.now = func() time.Time {
		t.Fatal("disabled worker accessed clock")
		return time.Time{}
	}

	worker.wait = func(
		context.Context,
		time.Duration,
	) error {
		t.Fatal("disabled worker scheduled cleanup")
		return nil
	}

	if err := worker.Run(
		context.Background(),
	); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerCycleFixedCutoff(t *testing.T) {
	policy := testRetentionWorkerPolicy()

	expectedCutoff, err := policy.Cutoff(
		testRetentionWorkerTime(),
	)
	if err != nil {
		t.Fatal(err)
	}

	deletionCounts := []int64{3, 3, 1}

	calls := 0
	clockCalls := 0

	var delays []time.Duration

	deleter := batchDeleterFunc(
		func(
			ctx context.Context,
			cutoff time.Time,
			limit int,
		) (int64, error) {
			if calls >= len(deletionCounts) {
				t.Fatal("unexpected additional batch")
			}

			if !cutoff.Equal(expectedCutoff) {
				t.Fatal("cutoff changed during cleanup cycle")
			}

			if limit != policy.BatchSize {
				t.Fatal("incorrect batch size")
			}

			if _, exists := ctx.Deadline(); !exists {
				t.Fatal("missing batch timeout")
			}

			deleted := deletionCounts[calls]
			calls++

			return deleted, nil
		},
	)

	worker := newTestRetentionWorker(
		t,
		policy,
		deleter,
	)

	worker.now = func() time.Time {
		value := testRetentionWorkerTime().Add(
			time.Duration(clockCalls) * time.Hour,
		)

		clockCalls++

		return value
	}

	worker.wait = func(
		ctx context.Context,
		duration time.Duration,
	) error {
		delays = append(delays, duration)
		return ctx.Err()
	}

	result, err := worker.runCycle(
		context.Background(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if result.Deleted != 7 ||
		result.Batches != 3 ||
		result.BatchLimitReached {
		t.Fatalf("incorrect cycle result: %+v", result)
	}

	if !result.Cutoff.Equal(expectedCutoff) {
		t.Fatal("incorrect result cutoff")
	}

	if clockCalls != 1 {
		t.Fatal("cleanup calculated multiple cutoffs")
	}

	expectedDelays := []time.Duration{
		policy.BatchDelay,
		policy.BatchDelay,
	}

	if !reflect.DeepEqual(delays, expectedDelays) {
		t.Fatalf("incorrect batch delays: %v", delays)
	}
}

func TestWorkerCycleBatchLimit(t *testing.T) {
	policy := testRetentionWorkerPolicy()
	policy.MaxBatchesPerCycle = 3

	calls := 0
	delays := 0

	deleter := batchDeleterFunc(
		func(
			_ context.Context,
			_ time.Time,
			limit int,
		) (int64, error) {
			calls++
			return int64(limit), nil
		},
	)

	worker := newTestRetentionWorker(
		t,
		policy,
		deleter,
	)

	worker.wait = func(
		ctx context.Context,
		duration time.Duration,
	) error {
		if duration != policy.BatchDelay {
			t.Fatal("incorrect batch delay")
		}

		delays++

		return ctx.Err()
	}

	result, err := worker.runCycle(
		context.Background(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 3 ||
		result.Batches != 3 ||
		result.Deleted != 9 ||
		!result.BatchLimitReached {
		t.Fatalf("incorrect bounded result: %+v", result)
	}

	if delays != 2 {
		t.Fatal("incorrect number of inter-batch delays")
	}
}

func TestWorkerCycleEmpty(t *testing.T) {
	policy := testRetentionWorkerPolicy()

	calls := 0

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

	worker.wait = func(
		context.Context,
		time.Duration,
	) error {
		t.Fatal("empty cleanup scheduled another batch")
		return nil
	}

	result, err := worker.runCycle(
		context.Background(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 1 ||
		result.Batches != 1 ||
		result.Deleted != 0 ||
		result.BatchLimitReached {
		t.Fatalf("incorrect empty cycle: %+v", result)
	}
}

func TestWorkerCyclePartialFailure(t *testing.T) {
	policy := testRetentionWorkerPolicy()

	injectedError := errors.New(
		"injected database failure",
	)

	calls := 0

	deleter := batchDeleterFunc(
		func(
			_ context.Context,
			_ time.Time,
			limit int,
		) (int64, error) {
			calls++

			if calls == 1 {
				return int64(limit), nil
			}

			return 0, injectedError
		},
	)

	worker := newTestRetentionWorker(
		t,
		policy,
		deleter,
	)

	result, err := worker.runCycle(
		context.Background(),
	)

	if !errors.Is(err, injectedError) {
		t.Fatalf("unexpected cycle error: %v", err)
	}

	if calls != 2 ||
		result.Batches != 1 ||
		result.Deleted != int64(policy.BatchSize) ||
		result.BatchLimitReached {
		t.Fatalf("incorrect partial result: %+v", result)
	}
}

func TestWorkerCycleInvalidDeletionCount(t *testing.T) {
	policy := testRetentionWorkerPolicy()

	for _, count := range []int64{
		-1,
		int64(policy.BatchSize) + 1,
	} {
		deleter := batchDeleterFunc(
			func(
				context.Context,
				time.Time,
				int,
			) (int64, error) {
				return count, nil
			},
		)

		worker := newTestRetentionWorker(
			t,
			policy,
			deleter,
		)

		result, err := worker.runCycle(
			context.Background(),
		)

		if err == nil {
			t.Fatal("invalid deletion count accepted")
		}

		if result.Deleted != 0 ||
			result.Batches != 0 ||
			result.BatchLimitReached {
			t.Fatal("invalid batch was counted as successful")
		}
	}
}

func TestWorkerRunStartupAndInterval(t *testing.T) {
	policy := testRetentionWorkerPolicy()

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	var events []string

	deleter := batchDeleterFunc(
		func(
			context.Context,
			time.Time,
			int,
		) (int64, error) {
			events = append(events, "cleanup")
			return 0, nil
		},
	)

	worker := newTestRetentionWorker(
		t,
		policy,
		deleter,
	)

	waits := 0

	worker.wait = func(
		ctx context.Context,
		duration time.Duration,
	) error {
		if duration != policy.CleanupInterval {
			t.Fatal("incorrect cleanup interval")
		}

		events = append(events, "wait")
		waits++

		if waits == 2 {
			cancel()
		}

		return ctx.Err()
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}

	expected := []string{
		"cleanup",
		"wait",
		"cleanup",
		"wait",
	}

	if !reflect.DeepEqual(events, expected) {
		t.Fatalf("incorrect worker schedule: %v", events)
	}

	if worker.running.Load() {
		t.Fatal("worker remained running after shutdown")
	}
}
