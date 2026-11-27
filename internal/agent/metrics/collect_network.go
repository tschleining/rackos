package metrics

import (
	"context"
	"fmt"
	"time"
)

// collectWithNetworkReaders extends an existing
// performance collection with network measurements.
//
// Network sampling shares the existing performance
// collection wait rather than introducing a
// separate sampling delay.
func collectWithNetworkReaders(
	ctx context.Context,
	collectPerformance func(context.Context) (Snapshot, error),
	readNetwork func(context.Context) ([]NetworkInterfaceUsage, error),
	now func() time.Time,
) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	previous, err := readNetwork(ctx)

	if ctxErr := ctx.Err(); ctxErr != nil {
		return Snapshot{}, ctxErr
	}

	if err != nil {
		return Snapshot{}, fmt.Errorf(
			"read initial network counters: %w",
			err,
		)
	}

	startedAt := now()

	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	snapshot, err := collectPerformance(ctx)

	if ctxErr := ctx.Err(); ctxErr != nil {
		return Snapshot{}, ctxErr
	}

	if err != nil {
		return Snapshot{}, err
	}

	current, err := readNetwork(ctx)

	if ctxErr := ctx.Err(); ctxErr != nil {
		return Snapshot{}, ctxErr
	}

	if err != nil {
		return Snapshot{}, fmt.Errorf(
			"read final network counters: %w",
			err,
		)
	}

	finishedAt := now()

	rates, err := calculateNetworkRates(
		previous,
		current,
		finishedAt.Sub(startedAt),
	)
	if err != nil {
		return Snapshot{}, fmt.Errorf(
			"calculate network rates: %w",
			err,
		)
	}

	// Preserve the JSON array contract when no
	// network interfaces are available.
	if current == nil {
		current = []NetworkInterfaceUsage{}
	}

	snapshot.Network = &NetworkUsage{
		Interfaces:     current,
		InterfaceRates: rates,
	}

	// Record completion of the entire unified
	// performance snapshot.
	snapshot.CollectedAt = now().UTC()

	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	return snapshot, nil
}
