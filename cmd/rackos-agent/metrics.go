package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/tschleining/rackos/internal/agent/metrics"
)

const metricsCollectionTimeout = 10 * time.Second

// runMetrics collects and prints a single system-wide
// performance snapshot without connecting to an appliance.
func runMetrics(
	ctx context.Context,
	args []string,
	stdout io.Writer,
) error {
	return runMetricsWithCollector(
		ctx,
		args,
		stdout,
		metrics.Collect,
	)
}

// runMetricsWithCollector allows command behavior to be
// tested independently of native hardware collection.
func runMetricsWithCollector(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	collect func(context.Context) (metrics.Snapshot, error),
) error {
	if len(args) != 0 {
		return fmt.Errorf(
			"usage: rackos-agent metrics",
		)
	}

	ctx, cancel := context.WithTimeout(
		ctx,
		metricsCollectionTimeout,
	)
	defer cancel()

	if err := ctx.Err(); err != nil {
		return err
	}

	snapshot, err := collect(ctx)
	if err != nil {
		return fmt.Errorf(
			"collect metrics: %w",
			err,
		)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(snapshot); err != nil {
		return fmt.Errorf(
			"write metrics snapshot: %w",
			err,
		)
	}

	return nil
}
