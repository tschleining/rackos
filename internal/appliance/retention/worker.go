package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

var ErrWorkerRunning = errors.New(
	"performance retention worker is already running",
)

// BatchDeleter defines the persistence operation
// required by the retention worker.
//
// Each successful invocation must delete at most
// limit records received strictly before cutoff.
//
// Successful deletions must be committed before
// the operation returns.
type BatchDeleter interface {
	DeleteExpiredBatch(
		ctx context.Context,
		cutoff time.Time,
		limit int,
	) (int64, error)
}

// Worker manages automatic historical performance
// expiration.
//
// Cleanup cycles are sequential and bounded.
//
// Workers must be constructed using NewWorker
// and must not be copied after construction.
type Worker struct {
	policy  Policy
	deleter BatchDeleter
	logger  *slog.Logger

	running atomic.Bool

	// Internal timing functions allow deterministic
	// scheduler testing without real-time delays.
	now  func() time.Time
	wait func(context.Context, time.Duration) error
}

// cycleResult describes successfully completed
// deletion operations within one cleanup cycle.
//
// BatchLimitReached indicates that the cycle ended
// after a full final batch at its configured limit.
// Additional expired records may still exist.
type cycleResult struct {
	Cutoff            time.Time
	Deleted           int64
	Batches           int
	BatchLimitReached bool
}

// NewWorker constructs a retention worker using
// a validated policy.
//
// A deletion repository is required when automatic
// retention is enabled.
//
// Constructing a worker does not start cleanup.
func NewWorker(
	policy Policy,
	deleter BatchDeleter,
	logger *slog.Logger,
) (*Worker, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}

	if policy.Enabled() && deleter == nil {
		return nil, errors.New(
			"retention requires a deletion repository",
		)
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &Worker{
		policy:  policy,
		deleter: deleter,
		logger:  logger,

		now:  time.Now,
		wait: waitForDuration,
	}, nil
}

// Run executes automatic historical retention until
// the supplied context is canceled.
//
// Disabled retention returns immediately without
// accessing the deletion repository.
//
// Enabled retention performs an initial cleanup
// immediately, followed by sequential periodic cycles.
//
// Cleanup failures are logged and retried during
// subsequent cycles.
//
// Context cancellation represents normal shutdown.
func (w *Worker) Run(ctx context.Context) error {
	if !w.policy.Enabled() {
		return nil
	}

	if !w.running.CompareAndSwap(false, true) {
		return ErrWorkerRunning
	}
	defer w.running.Store(false)

	if ctx.Err() != nil {
		return nil
	}

	w.logger.Info(
		"Performance retention worker started",
		"retention", w.policy.RetentionDuration.String(),
		"cleanup_interval", w.policy.CleanupInterval.String(),
		"batch_size", w.policy.BatchSize,
		"max_batches_per_cycle", w.policy.MaxBatchesPerCycle,
	)

	defer w.logger.Info(
		"Performance retention worker stopped",
	)

	for {
		if ctx.Err() != nil {
			return nil
		}

		result, err := w.runCycle(ctx)

		if ctx.Err() != nil {
			return nil
		}

		w.logCycle(result, err)

		// Schedule the next cycle relative to the
		// completion of the current cycle.
		//
		// Slow cleanup cannot accumulate queued
		// or overlapping cleanup operations.
		if err := w.wait(
			ctx,
			w.policy.CleanupInterval,
		); err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return fmt.Errorf(
				"wait for retention cleanup: %w",
				err,
			)
		}
	}
}

// runCycle calculates a fixed expiration cutoff
// and performs a bounded series of deletion batches.
//
// Successful batches commit independently.
//
// A failed batch terminates the current cycle
// without discarding earlier successful deletions.
func (w *Worker) runCycle(
	ctx context.Context,
) (cycleResult, error) {
	var result cycleResult

	if err := ctx.Err(); err != nil {
		return result, err
	}

	cutoff, err := w.policy.Cutoff(w.now())
	if err != nil {
		return result, err
	}

	result.Cutoff = cutoff

	for batch := 0; batch < w.policy.MaxBatchesPerCycle; batch++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		batchCtx, cancel := context.WithTimeout(
			ctx,
			w.policy.BatchTimeout,
		)

		deleted, err := w.deleter.DeleteExpiredBatch(
			batchCtx,
			cutoff,
			w.policy.BatchSize,
		)

		cancel()

		if err != nil {
			return result, fmt.Errorf(
				"retention batch %d: %w",
				batch+1,
				err,
			)
		}

		if deleted < 0 ||
			deleted > int64(w.policy.BatchSize) {
			return result, fmt.Errorf(
				"retention batch %d returned invalid deletion count: %d",
				batch+1,
				deleted,
			)
		}

		result.Deleted += deleted
		result.Batches++

		// A partial batch indicates that no
		// additional eligible records were found
		// when this statement executed.
		if deleted < int64(w.policy.BatchSize) {
			return result, nil
		}

		// Do not wait unnecessarily after the
		// final permitted batch.
		if result.Batches == w.policy.MaxBatchesPerCycle {
			result.BatchLimitReached = true
			return result, nil
		}

		// Release the database connection between
		// batches so other appliance operations
		// have an opportunity to proceed.
		if err := w.wait(
			ctx,
			w.policy.BatchDelay,
		); err != nil {
			return result, err
		}
	}

	return result, nil
}

// logCycle reports cleanup activity without
// logging device identities or raw measurements.
func (w *Worker) logCycle(
	result cycleResult,
	err error,
) {
	attributes := []any{
		"cutoff", result.Cutoff,
		"deleted_samples", result.Deleted,
		"completed_batches", result.Batches,
		"batch_limit_reached", result.BatchLimitReached,
	}

	if err != nil {
		attributes = append(
			attributes,
			"error", err,
		)

		w.logger.Warn(
			"Performance retention cleanup failed",
			attributes...,
		)

		return
	}

	if result.Deleted > 0 ||
		result.BatchLimitReached {
		w.logger.Info(
			"Performance retention cleanup completed",
			attributes...,
		)

		return
	}

	w.logger.Debug(
		"Performance retention cleanup found no expired samples",
		attributes...,
	)
}

// waitForDuration waits for a duration while
// allowing immediate context cancellation.
func waitForDuration(
	ctx context.Context,
	duration time.Duration,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()

	case <-timer.C:
		return ctx.Err()
	}
}
