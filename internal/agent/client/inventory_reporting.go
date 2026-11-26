package client

import (
	"context"
	"log/slog"
	"time"

	"github.com/tschleining/rackos/internal/agent/inventory"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	inventoryRefreshInterval = 6 * time.Hour
	inventoryRetryInitial    = 5 * time.Second
	inventoryRetryMaximum    = 5 * time.Minute
	inventoryRecollectDelay  = 5 * time.Minute
	inventoryCollectionLimit = 10 * time.Second
)

// RunWithInventory runs heartbeat monitoring and
// hardware inventory reporting concurrently.
//
// Both workers are stopped before this method returns.
func (c *Client) RunWithInventory(
	ctx context.Context,
	deviceID string,
	token string,
	logger *slog.Logger,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	inventoryDone := make(chan error, 1)

	go func() {
		err := runInventoryReporting(
			ctx,
			inventory.Collect,
			func(
				ctx context.Context,
				snapshot inventory.Inventory,
			) error {
				return c.ReportInventory(
					ctx,
					deviceID,
					token,
					snapshot,
				)
			},
			waitForInventory,
			logger,
		)

		// A fatal inventory authentication failure
		// must also stop the heartbeat worker.
		if err != nil {
			cancel()
		}

		inventoryDone <- err
	}()

	// Preserve the existing heartbeat implementation.
	heartbeatErr := c.Run(
		ctx,
		deviceID,
		token,
		logger,
	)

	// Stop inventory collection and reporting when
	// heartbeat monitoring terminates.
	cancel()

	inventoryErr := <-inventoryDone

	if inventoryErr != nil {
		return inventoryErr
	}

	return heartbeatErr
}

// runInventoryReporting collects and reports inventory
// independently of the heartbeat worker.
//
// Collector, reporter and wait functions are injected
// to support deterministic scheduler testing.
func runInventoryReporting(
	ctx context.Context,
	collect func(context.Context) (inventory.Inventory, error),
	report func(context.Context, inventory.Inventory) error,
	wait func(context.Context, time.Duration) bool,
	logger *slog.Logger,
) error {
	retryDelay := inventoryRetryInitial

	var pending *inventory.Inventory

	for {
		if ctx.Err() != nil {
			return nil
		}

		if pending == nil {
			collectionCtx, cancel := context.WithTimeout(
				ctx,
				inventoryCollectionLimit,
			)

			snapshot, err := collect(collectionCtx)
			cancel()

			if ctx.Err() != nil {
				return nil
			}

			if err != nil {
				logger.Warn(
					"hardware inventory collection failed",
					"retry_in", retryDelay,
				)

				if !wait(ctx, retryDelay) {
					return nil
				}

				retryDelay = nextInventoryRetry(
					retryDelay,
				)

				continue
			}

			pending = &snapshot
		}

		err := report(ctx, *pending)

		if ctx.Err() != nil {
			return nil
		}

		if err == nil {
			logger.Info(
				"hardware inventory acknowledged",
				"next_report_in", inventoryRefreshInterval,
			)

			pending = nil
			retryDelay = inventoryRetryInitial

			if !wait(
				ctx,
				inventoryRefreshInterval,
			) {
				return nil
			}

			continue
		}

		rpcStatus, isRPCError := status.FromError(err)

		// Local conversion errors or unexpected
		// response contents require a fresh snapshot.
		if !isRPCError {
			logger.Warn(
				"hardware inventory reporting failed",
				"recollect_in", inventoryRecollectDelay,
			)

			pending = nil
			retryDelay = inventoryRetryInitial

			if !wait(
				ctx,
				inventoryRecollectDelay,
			) {
				return nil
			}

			continue
		}

		code := rpcStatus.Code()

		switch code {
		case codes.Unauthenticated,
			codes.PermissionDenied:

			logger.Error(
				"inventory authentication rejected",
				"code", code.String(),
			)

			return status.Error(
				code,
				"inventory authentication rejected",
			)

		case codes.Unimplemented:
			// Older appliances may not support
			// inventory reporting. Heartbeats continue.
			logger.Warn(
				"appliance does not support inventory reporting",
			)

			return nil

		case codes.InvalidArgument,
			codes.FailedPrecondition,
			codes.AlreadyExists:

			// A rejected snapshot should not be
			// retried indefinitely.
			logger.Warn(
				"hardware inventory rejected",
				"code", code.String(),
				"recollect_in", inventoryRecollectDelay,
			)

			pending = nil
			retryDelay = inventoryRetryInitial

			if !wait(
				ctx,
				inventoryRecollectDelay,
			) {
				return nil
			}

		case codes.Unavailable,
			codes.DeadlineExceeded,
			codes.Internal,
			codes.ResourceExhausted,
			codes.Aborted,
			codes.Canceled,
			codes.Unknown:

			// Keep the same snapshot and collection
			// timestamp for idempotent retries.
			logger.Warn(
				"temporary inventory reporting failure",
				"code", code.String(),
				"retry_in", retryDelay,
			)

			if !wait(ctx, retryDelay) {
				return nil
			}

			retryDelay = nextInventoryRetry(
				retryDelay,
			)

		default:
			logger.Warn(
				"unexpected inventory reporting failure",
				"code", code.String(),
				"recollect_in", inventoryRecollectDelay,
			)

			pending = nil
			retryDelay = inventoryRetryInitial

			if !wait(
				ctx,
				inventoryRecollectDelay,
			) {
				return nil
			}
		}
	}
}

// nextInventoryRetry doubles the current retry delay
// without exceeding the configured maximum.
func nextInventoryRetry(
	current time.Duration,
) time.Duration {
	if current <= 0 {
		return inventoryRetryInitial
	}

	if current >= inventoryRetryMaximum/2 {
		return inventoryRetryMaximum
	}

	return current * 2
}

// waitForInventory waits until the requested interval
// expires or the agent context is canceled.
func waitForInventory(
	ctx context.Context,
	delay time.Duration,
) bool {
	if ctx.Err() != nil {
		return false
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false

	case <-timer.C:
		return ctx.Err() == nil
	}
}
