package metrics

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	darwinMountBufferSlack = 16
	darwinMaxMounts        = 16384
	darwinMountAttempts    = 4
)

var errInvalidDarwinMountInfo = errors.New(
	"invalid macOS mount information",
)

// darwinMount represents a mounted filesystem
// visible to the macOS agent.
type darwinMount struct {
	Source         string
	MountPoint     string
	FilesystemType string

	ReadOnly bool

	FilesystemID unix.Fsid
}

// discoverDarwinMounts discovers local filesystems
// eligible for storage monitoring.
func discoverDarwinMounts(
	ctx context.Context,
) ([]darwinMount, error) {
	statistics, err := readDarwinMountStatistics(
		ctx,
		unix.Getfsstat,
	)
	if err != nil {
		return nil, err
	}

	mounts, err := filterDarwinMounts(statistics)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return mounts, nil
}

// readDarwinMountStatistics retrieves the native
// mount table using cached kernel information.
//
// Additional buffer space and bounded retries
// account for filesystems being mounted between
// the initial count and the subsequent read.
func readDarwinMountStatistics(
	ctx context.Context,
	getfsstat func([]unix.Statfs_t, int) (int, error),
) ([]unix.Statfs_t, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	count, err := getfsstat(
		nil,
		unix.MNT_NOWAIT,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"count mounted filesystems: %w",
			err,
		)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if count < 0 || count > darwinMaxMounts {
		return nil, fmt.Errorf(
			"%w: invalid mount count %d",
			errInvalidDarwinMountInfo,
			count,
		)
	}

	size := min(
		count+darwinMountBufferSlack,
		darwinMaxMounts,
	)

	for attempt := 0; attempt < darwinMountAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		buffer := make([]unix.Statfs_t, size)

		actual, err := getfsstat(
			buffer,
			unix.MNT_NOWAIT,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"read mounted filesystems: %w",
				err,
			)
		}

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if actual < 0 || actual > darwinMaxMounts {
			return nil, fmt.Errorf(
				"%w: invalid returned mount count %d",
				errInvalidDarwinMountInfo,
				actual,
			)
		}

		// A partially filled buffer indicates
		// that all visible records were retrieved.
		if actual < len(buffer) {
			return buffer[:actual], nil
		}

		// A full buffer may indicate truncation.
		// Retry with additional capacity.
		if size == darwinMaxMounts {
			break
		}

		size = min(
			max(
				size*2,
				actual+darwinMountBufferSlack,
			),
			darwinMaxMounts,
		)
	}

	return nil, fmt.Errorf(
		"%w: unable to obtain complete mount table",
		errInvalidDarwinMountInfo,
	)
}

// filterDarwinMounts converts eligible native
// filesystem records into RackOS mount metadata.
func filterDarwinMounts(
	statistics []unix.Statfs_t,
) ([]darwinMount, error) {
	mounts := make(
		[]darwinMount,
		0,
		len(statistics),
	)

	for _, entry := range statistics {
		filesystemType := unix.ByteSliceToString(
			entry.Fstypename[:],
		)

		if !isMonitoredDarwinFilesystem(
			filesystemType,
			entry.Flags,
		) {
			continue
		}

		source := unix.ByteSliceToString(
			entry.Mntfromname[:],
		)

		mountPoint := unix.ByteSliceToString(
			entry.Mntonname[:],
		)

		if source == "" {
			return nil, fmt.Errorf(
				"%w: empty filesystem source",
				errInvalidDarwinMountInfo,
			)
		}

		if !strings.HasPrefix(mountPoint, "/") {
			return nil, fmt.Errorf(
				"%w: invalid mount point %q",
				errInvalidDarwinMountInfo,
				mountPoint,
			)
		}

		mounts = append(
			mounts,
			darwinMount{
				Source:         source,
				MountPoint:     mountPoint,
				FilesystemType: filesystemType,
				ReadOnly: entry.Flags&
					unix.MNT_RDONLY != 0,
				FilesystemID: entry.Fsid,
			},
		)
	}

	// Stable ordering simplifies diagnostics
	// and comparisons between snapshots.
	sort.SliceStable(mounts, func(i, j int) bool {
		if mounts[i].MountPoint == mounts[j].MountPoint {
			return mounts[i].Source <
				mounts[j].Source
		}

		return mounts[i].MountPoint <
			mounts[j].MountPoint
	})

	return mounts, nil
}

// isMonitoredDarwinFilesystem defines the initial
// macOS filesystem inclusion policy.
func isMonitoredDarwinFilesystem(
	filesystemType string,
	flags uint32,
) bool {
	if flags&unix.MNT_LOCAL == 0 {
		return false
	}

	switch filesystemType {
	case "apfs",
		"hfs", "hfsplus",
		"msdos", "exfat",
		"ntfs",
		"udf", "cd9660", "iso9660",
		"zfs":
		return true

	default:
		return false
	}
}
