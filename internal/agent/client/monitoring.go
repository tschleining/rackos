package client

import (
	"context"
	"log/slog"
)

// RunWithMonitoring runs heartbeat, hardware inventory
// and performance reporting concurrently.
//
// All workers are stopped before this method returns.
func (c *Client) RunWithMonitoring(
	ctx context.Context,
	deviceID string,
	token string,
	logger *slog.Logger,
) error {
	return runMonitoringWorkers(
		ctx,
		func(ctx context.Context) error {
			return c.RunWithInventory(
				ctx,
				deviceID,
				token,
				logger,
			)
		},
		func(ctx context.Context) error {
			return c.RunPerformance(
				ctx,
				deviceID,
				token,
				logger,
			)
		},
	)
}

// runMonitoringWorkers coordinates the existing core
// monitoring lifecycle and the optional performance
// reporting worker.
//
// A fatal error in either worker cancels the shared
// context. A normal performance worker exit does not
// interrupt heartbeat or inventory monitoring.
func runMonitoringWorkers(
	ctx context.Context,
	runCore func(context.Context) error,
	runPerformance func(context.Context) error,
) error {
	if ctx.Err() != nil {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	performanceDone := make(chan error, 1)

	go func() {
		err := runPerformance(ctx)

		// A fatal performance reporting error must
		// also stop heartbeat and inventory.
		if err != nil {
			cancel()
		}

		performanceDone <- err
	}()

	// The existing core worker supervises heartbeat
	// and inventory reporting.
	coreErr := runCore(ctx)

	// When core monitoring terminates, stop
	// performance collection and reporting.
	cancel()

	// Wait for the performance worker before
	// allowing the client connection to close.
	performanceErr := <-performanceDone

	if performanceErr != nil {
		return performanceErr
	}

	return coreErr
}
