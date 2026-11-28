package session

import (
	"context"
	"log/slog"
	"time"
)

// RunMaintenance periodically removes invalid sessions.
//
// The caller must cancel and join this worker before
// closing the database.
func (r *Repository) RunMaintenance(
	ctx context.Context,
	logger *slog.Logger,
) {
	r.runMaintenance(
		ctx,
		logger,
		CleanupInterval,
	)
}

// The interval is private so production behavior
// cannot be changed by an untrusted caller.
//
// Tests use a shorter interval to exercise actual
// periodic cleanup without waiting one minute.
func (r *Repository) runMaintenance(
	ctx context.Context,
	logger *slog.Logger,
	interval time.Duration,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}

		cleanupCtx, cancel := context.WithTimeout(
			ctx,
			10*time.Second,
		)

		count, err := r.Cleanup(cleanupCtx)

		cancel()

		if ctx.Err() != nil {
			return
		}

		if err != nil {
			logger.Error(
				"Administrator session cleanup failed",
				"error", err,
			)
		} else if count > 0 {
			logger.Debug(
				"Invalid administrator sessions removed",
				"count", count,
			)
		}

		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
		}
	}
}
