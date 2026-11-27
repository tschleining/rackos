package metrics

import (
	"context"
	"fmt"
	"time"
)

// Collect gathers system-wide CPU and memory metrics
// using the native collectors for the current platform.
//
// CPU utilization is measured over the default sampling
// interval. Memory utilization is measured immediately
// after CPU sampling completes.
//
// Collect returns an empty snapshot if any stage fails.
func Collect(ctx context.Context) (Snapshot, error) {
	return collectWithStorageReaders(
		ctx,
		func(ctx context.Context) (Snapshot, error) {
			return collectWithReaders(
				ctx,
				readCPUCounters,
				readMemoryUsage,
				waitForCPUSample,
				time.Now,
			)
		},
		readDiskUsage,
		readFilesystemUsage,
		time.Now,
	)
}

// collectWithReaders coordinates metrics collection.
//
// Dependencies are injectable so collection behavior
// can be tested without real hardware or timing delays.
func collectWithReaders(
	ctx context.Context,
	readCPU func(context.Context) (cpuCounters, error),
	readMemory func(context.Context) (MemoryUsage, error),
	wait func(context.Context, time.Duration) error,
	now func() time.Time,
) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	first, err := readCPU(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf(
			"read initial CPU counters: %w",
			err,
		)
	}

	startedAt := now()

	if err := wait(
		ctx,
		DefaultCPUSampleInterval,
	); err != nil {
		return Snapshot{}, err
	}

	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	second, err := readCPU(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf(
			"read final CPU counters: %w",
			err,
		)
	}

	finishedAt := now()

	elapsedMillis := finishedAt.Sub(
		startedAt,
	).Milliseconds()

	if elapsedMillis <= 0 {
		return Snapshot{}, fmt.Errorf(
			"%w: invalid CPU sampling interval",
			ErrInvalidCPUCounters,
		)
	}

	cpuUsage, err := calculateCPUUsage(
		first,
		second,
	)
	if err != nil {
		return Snapshot{}, fmt.Errorf(
			"calculate CPU utilization: %w",
			err,
		)
	}

	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	memory, err := readMemory(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf(
			"collect memory utilization: %w",
			err,
		)
	}

	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	return Snapshot{
		CPU: CPUUsage{
			UsagePercent:         cpuUsage,
			SampleIntervalMillis: elapsedMillis,
		},
		Memory:      memory,
		CollectedAt: now().UTC(),
	}, nil
}

// waitForCPUSample waits for the CPU sampling interval
// while allowing prompt context cancellation.
func waitForCPUSample(
	ctx context.Context,
	interval time.Duration,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()

	case <-timer.C:
		return ctx.Err()
	}
}
