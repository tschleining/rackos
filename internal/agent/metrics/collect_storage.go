package metrics

import (
	"context"
	"fmt"
	"time"
)

// collectWithStorageReaders extends CPU and memory
// collection with filesystem and disk measurements.
//
// Disk counters are sampled around the existing
// CPU and memory collection operation, avoiding
// an additional sampling wait.
//
// Injectable readers and clock functions allow
// deterministic testing without native hardware.
func collectWithStorageReaders(
	ctx context.Context,
	collectPerformance func(context.Context) (Snapshot, error),
	readDisks func(context.Context) ([]DiskUsage, error),
	readFilesystems func(context.Context) ([]FilesystemUsage, error),
	now func() time.Time,
) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	previousDisks, err := readDisks(ctx)
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf(
			"read initial disk counters: %w",
			err,
		)
	}

	diskStartedAt := now()

	// The existing collector performs CPU sampling,
	// the one-second wait, and memory collection.
	snapshot, err := collectPerformance(ctx)
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	if err != nil {
		return Snapshot{}, err
	}

	currentDisks, err := readDisks(ctx)
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf(
			"read final disk counters: %w",
			err,
		)
	}

	diskFinishedAt := now()

	diskInterval := diskFinishedAt.Sub(
		diskStartedAt,
	)

	rates, err := calculateDiskRates(
		previousDisks,
		currentDisks,
		diskInterval,
	)
	if err != nil {
		return Snapshot{}, fmt.Errorf(
			"calculate disk rates: %w",
			err,
		)
	}

	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	filesystems, err := readFilesystems(ctx)
	if ctx.Err() != nil {
		return Snapshot{}, ctx.Err()
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf(
			"collect filesystem capacity: %w",
			err,
		)
	}

	// Empty collections are represented as JSON
	// arrays rather than null.
	if currentDisks == nil {
		currentDisks = make([]DiskUsage, 0)
	}

	if filesystems == nil {
		filesystems = make([]FilesystemUsage, 0)
	}

	snapshot.Storage = &StorageUsage{
		Filesystems: filesystems,
		Disks:       currentDisks,
		DiskRates:   rates,
	}

	// The unified snapshot timestamp represents
	// completion of all performance measurements.
	snapshot.CollectedAt = now().UTC()

	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	return snapshot, nil
}
