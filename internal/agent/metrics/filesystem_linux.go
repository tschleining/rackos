package metrics

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

var errLinuxMountChanged = errors.New(
	"Linux mount is no longer visible",
)

// readFilesystemUsage discovers and measures
// eligible Linux filesystems.
func readFilesystemUsage(
	ctx context.Context,
) ([]FilesystemUsage, error) {
	mounts, err := discoverLinuxMounts(ctx)
	if err != nil {
		return nil, err
	}

	return collectLinuxFilesystemUsage(
		ctx,
		mounts,
		readLinuxFilesystem,
	)
}

// collectLinuxFilesystemUsage coordinates filesystem
// collection using an injectable native reader.
func collectLinuxFilesystemUsage(
	ctx context.Context,
	mounts []linuxMount,
	read func(context.Context, linuxMount) (FilesystemUsage, error),
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

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if errors.Is(err, errLinuxMountChanged) {
			// The mount disappeared or is no longer
			// visible at its original path.
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

// readLinuxFilesystem collects capacity statistics
// from a specific mounted filesystem.
//
// A file descriptor is used to ensure mount identity
// and capacity are measured against the same object.
func readLinuxFilesystem(
	ctx context.Context,
	mount linuxMount,
) (FilesystemUsage, error) {
	if err := ctx.Err(); err != nil {
		return FilesystemUsage{}, err
	}

	fd, err := unix.Open(
		mount.MountPoint,
		unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		switch {
		case errors.Is(err, unix.ENOENT),
			errors.Is(err, unix.ENOTDIR),
			errors.Is(err, unix.ESTALE):
			return FilesystemUsage{}, fmt.Errorf(
				"%w: %v",
				errLinuxMountChanged,
				err,
			)

		default:
			return FilesystemUsage{}, fmt.Errorf(
				"open mount point: %w",
				err,
			)
		}
	}
	defer unix.Close(fd)

	if err := ctx.Err(); err != nil {
		return FilesystemUsage{}, err
	}

	// Verify that the opened path still belongs to
	// the mount discovered through mountinfo.
	var identity unix.Statx_t

	if err := unix.Statx(
		fd,
		"",
		unix.AT_EMPTY_PATH|unix.AT_STATX_DONT_SYNC,
		unix.STATX_MNT_ID,
		&identity,
	); err != nil {
		return FilesystemUsage{}, fmt.Errorf(
			"read mount identity: %w",
			err,
		)
	}

	if identity.Mask&unix.STATX_MNT_ID == 0 {
		return FilesystemUsage{}, fmt.Errorf(
			"%w: kernel did not provide mount ID",
			ErrInvalidMountInfo,
		)
	}

	if identity.Mnt_id != mount.ID {
		return FilesystemUsage{}, fmt.Errorf(
			"%w: expected mount ID %d, found %d",
			errLinuxMountChanged,
			mount.ID,
			identity.Mnt_id,
		)
	}

	if err := ctx.Err(); err != nil {
		return FilesystemUsage{}, err
	}

	var statistics unix.Statfs_t

	if err := unix.Fstatfs(
		fd,
		&statistics,
	); err != nil {
		return FilesystemUsage{}, fmt.Errorf(
			"read filesystem statistics: %w",
			err,
		)
	}

	if err := ctx.Err(); err != nil {
		return FilesystemUsage{}, err
	}

	capacity, err := normalizeLinuxFilesystemCapacity(
		statistics,
	)
	if err != nil {
		return FilesystemUsage{}, err
	}

	return FilesystemUsage{
		Source:         mount.Source,
		MountPoint:     mount.MountPoint,
		FilesystemType: mount.FilesystemType,
		ReadOnly:       mount.ReadOnly,
		Capacity:       capacity,
	}, nil
}

// normalizeLinuxFilesystemCapacity converts native
// filesystem counters into RackOS byte measurements.
func normalizeLinuxFilesystemCapacity(
	statistics unix.Statfs_t,
) (FilesystemCapacity, error) {
	// Fragment size is the accounting unit for
	// filesystem block counters.
	blockSize := statistics.Frsize

	// Some filesystems may not provide a separate
	// fragment size.
	if blockSize == 0 {
		blockSize = statistics.Bsize
	}

	if blockSize <= 0 {
		return FilesystemCapacity{}, fmt.Errorf(
			"%w: invalid native block size",
			ErrInvalidFilesystem,
		)
	}

	return filesystemCapacityFromBlocks(
		uint64(blockSize),
		uint64(statistics.Blocks),
		uint64(statistics.Bfree),
		uint64(statistics.Bavail),
	)
}
