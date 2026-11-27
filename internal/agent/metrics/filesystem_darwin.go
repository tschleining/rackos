package metrics

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

var errDarwinMountChanged = errors.New(
	"macOS mount is no longer visible",
)

// readFilesystemUsage discovers and measures
// eligible macOS filesystems.
func readFilesystemUsage(
	ctx context.Context,
) ([]FilesystemUsage, error) {
	mounts, err := discoverDarwinMounts(ctx)
	if err != nil {
		return nil, err
	}

	return collectDarwinFilesystemUsage(
		ctx,
		mounts,
		readDarwinFilesystem,
	)
}

// collectDarwinFilesystemUsage coordinates filesystem
// collection using an injectable native reader.
func collectDarwinFilesystemUsage(
	ctx context.Context,
	mounts []darwinMount,
	read func(context.Context, darwinMount) (FilesystemUsage, error),
) ([]FilesystemUsage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	filesystems := make(
		[]FilesystemUsage,
		0,
		len(mounts),
	)

	for _, mount := range mounts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		filesystem, err := read(ctx, mount)

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if errors.Is(err, errDarwinMountChanged) {
			// The filesystem disappeared or another
			// filesystem now occupies its mount point.
			continue
		}

		if err != nil {
			return nil, fmt.Errorf(
				"collect filesystem %q: %w",
				mount.MountPoint,
				err,
			)
		}

		filesystems = append(
			filesystems,
			filesystem,
		)
	}

	return filesystems, nil
}

// readDarwinFilesystem retrieves current filesystem
// statistics for a discovered mount.
func readDarwinFilesystem(
	ctx context.Context,
	mount darwinMount,
) (FilesystemUsage, error) {
	return readDarwinFilesystemWithStatfs(
		ctx,
		mount,
		unix.Statfs,
	)
}

// readDarwinFilesystemWithStatfs allows native
// filesystem behavior to be tested independently.
func readDarwinFilesystemWithStatfs(
	ctx context.Context,
	mount darwinMount,
	statfs func(string, *unix.Statfs_t) error,
) (FilesystemUsage, error) {
	if err := ctx.Err(); err != nil {
		return FilesystemUsage{}, err
	}

	var statistics unix.Statfs_t

	err := statfs(
		mount.MountPoint,
		&statistics,
	)

	if ctx.Err() != nil {
		return FilesystemUsage{}, ctx.Err()
	}

	if err != nil {
		switch {
		case errors.Is(err, unix.ENOENT),
			errors.Is(err, unix.ENOTDIR),
			errors.Is(err, unix.ESTALE):
			return FilesystemUsage{}, fmt.Errorf(
				"%w: %v",
				errDarwinMountChanged,
				err,
			)

		default:
			return FilesystemUsage{}, fmt.Errorf(
				"read filesystem statistics: %w",
				err,
			)
		}
	}

	source := unix.ByteSliceToString(
		statistics.Mntfromname[:],
	)

	mountPoint := unix.ByteSliceToString(
		statistics.Mntonname[:],
	)

	filesystemType := unix.ByteSliceToString(
		statistics.Fstypename[:],
	)

	// Verify that the returned statistics belong
	// to the filesystem discovered earlier.
	if statistics.Fsid != mount.FilesystemID ||
		source != mount.Source ||
		mountPoint != mount.MountPoint ||
		filesystemType != mount.FilesystemType {
		return FilesystemUsage{}, fmt.Errorf(
			"%w: filesystem identity mismatch",
			errDarwinMountChanged,
		)
	}

	// Recheck the inclusion policy because mount
	// flags may have changed since discovery.
	if !isMonitoredDarwinFilesystem(
		filesystemType,
		statistics.Flags,
	) {
		return FilesystemUsage{}, fmt.Errorf(
			"%w: filesystem is no longer eligible",
			errDarwinMountChanged,
		)
	}

	capacity, err := normalizeDarwinFilesystemCapacity(
		statistics,
	)
	if err != nil {
		return FilesystemUsage{}, err
	}

	return FilesystemUsage{
		Source:         source,
		MountPoint:     mountPoint,
		FilesystemType: filesystemType,
		ReadOnly: statistics.Flags&
			unix.MNT_RDONLY != 0,
		Capacity: capacity,
	}, nil
}

// normalizeDarwinFilesystemCapacity converts native
// filesystem block counters into byte measurements.
func normalizeDarwinFilesystemCapacity(
	statistics unix.Statfs_t,
) (FilesystemCapacity, error) {
	return filesystemCapacityFromBlocks(
		uint64(statistics.Bsize),
		statistics.Blocks,
		statistics.Bfree,
		statistics.Bavail,
	)
}
