package metrics

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNormalizeLinuxFilesystemCapacity(t *testing.T) {
	tests := []struct {
		name       string
		statistics unix.Statfs_t
		blockSize  uint64
	}{
		{
			name: "fragment size preferred",
			statistics: unix.Statfs_t{
				Bsize:  8192,
				Frsize: 4096,
				Blocks: 1000,
				Bfree:  350,
				Bavail: 300,
			},
			blockSize: 4096,
		},
		{
			name: "block size fallback",
			statistics: unix.Statfs_t{
				Bsize:  4096,
				Blocks: 1000,
				Bfree:  350,
				Bavail: 300,
			},
			blockSize: 4096,
		},
		{
			name: "512 byte accounting blocks",
			statistics: unix.Statfs_t{
				Bsize:  4096,
				Frsize: 512,
				Blocks: 1000,
				Bfree:  350,
				Bavail: 300,
			},
			blockSize: 512,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capacity, err := normalizeLinuxFilesystemCapacity(
				tt.statistics,
			)
			if err != nil {
				t.Fatal(err)
			}

			if capacity.TotalBytes !=
				1000*tt.blockSize {
				t.Fatal("incorrect total capacity")
			}

			if capacity.FreeBytes !=
				350*tt.blockSize {
				t.Fatal("incorrect free capacity")
			}

			if capacity.AvailableBytes !=
				300*tt.blockSize {
				t.Fatal("incorrect available capacity")
			}

			if capacity.UsedBytes !=
				650*tt.blockSize {
				t.Fatal("incorrect used capacity")
			}

			if capacity.ReservedBytes !=
				50*tt.blockSize {
				t.Fatal("incorrect reserved capacity")
			}

			requireMetricPercent(
				t,
				capacity.UsagePercent,
				65,
			)

			requireFilesystemAccounting(
				t,
				capacity,
			)
		})
	}
}

func TestNormalizeLinuxFilesystemCapacityInvalid(t *testing.T) {
	tests := []struct {
		name       string
		statistics unix.Statfs_t
	}{
		{
			name: "zero block size",
			statistics: unix.Statfs_t{
				Blocks: 100,
			},
		},
		{
			name: "negative block size",
			statistics: unix.Statfs_t{
				Bsize:  -4096,
				Blocks: 100,
			},
		},
		{
			name: "negative fragment size",
			statistics: unix.Statfs_t{
				Bsize:  4096,
				Frsize: -4096,
				Blocks: 100,
			},
		},
		{
			name: "zero capacity",
			statistics: unix.Statfs_t{
				Bsize: 4096,
			},
		},
		{
			name: "free exceeds total",
			statistics: unix.Statfs_t{
				Bsize:  4096,
				Blocks: 100,
				Bfree:  101,
			},
		},
		{
			name: "available exceeds free",
			statistics: unix.Statfs_t{
				Bsize:  4096,
				Blocks: 100,
				Bfree:  50,
				Bavail: 51,
			},
		},
		{
			name: "byte conversion overflow",
			statistics: unix.Statfs_t{
				Bsize:  4096,
				Blocks: math.MaxUint64/4096 + 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capacity, err := normalizeLinuxFilesystemCapacity(
				tt.statistics,
			)

			if !errors.Is(err, ErrInvalidFilesystem) {
				t.Fatalf(
					"expected ErrInvalidFilesystem, got %v",
					err,
				)
			}

			if capacity != (FilesystemCapacity{}) {
				t.Fatal("invalid statistics returned capacity")
			}
		})
	}
}

func TestCollectLinuxFilesystemUsage(t *testing.T) {
	mounts := []linuxMount{
		{
			ID:             1,
			MountPoint:     "/",
			Source:         "/dev/sda1",
			FilesystemType: "ext4",
		},
		{
			ID:             2,
			MountPoint:     "/mnt/data",
			Source:         "/dev/sdb1",
			FilesystemType: "xfs",
			ReadOnly:       true,
		},
	}

	capacity, err := calculateFilesystemCapacity(
		1000,
		350,
		300,
	)
	if err != nil {
		t.Fatal(err)
	}

	calls := 0

	filesystems, err := collectLinuxFilesystemUsage(
		context.Background(),
		mounts,
		func(
			ctx context.Context,
			mount linuxMount,
		) (FilesystemUsage, error) {
			calls++

			return FilesystemUsage{
				Source:         mount.Source,
				MountPoint:     mount.MountPoint,
				FilesystemType: mount.FilesystemType,
				ReadOnly:       mount.ReadOnly,
				Capacity:       capacity,
			}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 2 || len(filesystems) != 2 {
		t.Fatal("unexpected filesystem collection count")
	}

	for i, filesystem := range filesystems {
		mount := mounts[i]

		if filesystem.Source != mount.Source ||
			filesystem.MountPoint != mount.MountPoint ||
			filesystem.FilesystemType != mount.FilesystemType ||
			filesystem.ReadOnly != mount.ReadOnly {
			t.Fatal("filesystem metadata was not preserved")
		}

		if filesystem.Capacity != capacity {
			t.Fatal("unexpected filesystem capacity")
		}
	}
}

func TestCollectLinuxFilesystemUsageSkipsChangedMount(t *testing.T) {
	mounts := []linuxMount{
		{ID: 1, MountPoint: "/"},
		{ID: 2, MountPoint: "/mnt/hidden"},
		{ID: 3, MountPoint: "/mnt/data"},
	}

	filesystems, err := collectLinuxFilesystemUsage(
		context.Background(),
		mounts,
		func(
			ctx context.Context,
			mount linuxMount,
		) (FilesystemUsage, error) {
			if mount.ID == 2 {
				return FilesystemUsage{}, fmt.Errorf(
					"mount mismatch: %w",
					errLinuxMountChanged,
				)
			}

			return FilesystemUsage{
				MountPoint: mount.MountPoint,
			}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(filesystems) != 2 {
		t.Fatalf(
			"expected two visible filesystems, got %d",
			len(filesystems),
		)
	}

	if filesystems[0].MountPoint != "/" ||
		filesystems[1].MountPoint != "/mnt/data" {
		t.Fatal("unexpected visible filesystems")
	}
}

func TestCollectLinuxFilesystemUsageFailure(t *testing.T) {
	expectedError := errors.New(
		"filesystem statistics unavailable",
	)

	mounts := []linuxMount{
		{ID: 1, MountPoint: "/"},
		{ID: 2, MountPoint: "/mnt/data"},
		{ID: 3, MountPoint: "/mnt/backup"},
	}

	calls := 0

	filesystems, err := collectLinuxFilesystemUsage(
		context.Background(),
		mounts,
		func(
			ctx context.Context,
			mount linuxMount,
		) (FilesystemUsage, error) {
			calls++

			if mount.ID == 2 {
				return FilesystemUsage{}, expectedError
			}

			return FilesystemUsage{
				MountPoint: mount.MountPoint,
			}, nil
		},
	)

	if !errors.Is(err, expectedError) {
		t.Fatalf(
			"expected collection error, got %v",
			err,
		)
	}

	if filesystems != nil {
		t.Fatal("failed collection returned partial results")
	}

	if calls != 2 {
		t.Fatal("collection continued after failure")
	}
}

func TestCollectLinuxFilesystemUsageEmpty(t *testing.T) {
	filesystems, err := collectLinuxFilesystemUsage(
		context.Background(),
		nil,
		func(
			ctx context.Context,
			mount linuxMount,
		) (FilesystemUsage, error) {
			t.Fatal("unexpected filesystem reader invocation")
			return FilesystemUsage{}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if filesystems == nil || len(filesystems) != 0 {
		t.Fatal("expected initialized empty filesystem slice")
	}
}

func TestCollectLinuxFilesystemUsageCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	mounts := []linuxMount{
		{ID: 1, MountPoint: "/"},
		{ID: 2, MountPoint: "/mnt/data"},
	}

	calls := 0

	filesystems, err := collectLinuxFilesystemUsage(
		ctx,
		mounts,
		func(
			ctx context.Context,
			mount linuxMount,
		) (FilesystemUsage, error) {
			calls++
			cancel()

			return FilesystemUsage{
				MountPoint: mount.MountPoint,
			}, nil
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if filesystems != nil {
		t.Fatal("canceled collection returned results")
	}

	if calls != 1 {
		t.Fatal("collection continued after cancellation")
	}
}

func TestReadLinuxFilesystemCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	filesystem, err := readLinuxFilesystem(
		ctx,
		linuxMount{MountPoint: "/"},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if filesystem != (FilesystemUsage{}) {
		t.Fatal("canceled read returned filesystem")
	}
}

func TestReadLinuxFilesystemMissingMount(t *testing.T) {
	missing := filepath.Join(
		t.TempDir(),
		"missing-mount",
	)

	filesystem, err := readLinuxFilesystem(
		context.Background(),
		linuxMount{
			ID:         1,
			MountPoint: missing,
		},
	)

	if !errors.Is(err, errLinuxMountChanged) {
		t.Fatalf(
			"expected changed mount error, got %v",
			err,
		)
	}

	if filesystem != (FilesystemUsage{}) {
		t.Fatal("missing mount returned filesystem")
	}
}

func TestReadLinuxFilesystemMountMismatch(t *testing.T) {
	// Zero is not a valid mount ID. Opening the
	// root filesystem must therefore produce
	// a mount identity mismatch.
	filesystem, err := readLinuxFilesystem(
		context.Background(),
		linuxMount{
			ID:         0,
			MountPoint: "/",
		},
	)

	if !errors.Is(err, errLinuxMountChanged) {
		t.Fatalf(
			"expected mount mismatch error, got %v",
			err,
		)
	}

	if filesystem != (FilesystemUsage{}) {
		t.Fatal("mismatched mount returned filesystem")
	}
}

func TestReadLinuxFilesystemUsageNative(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	filesystems, err := readFilesystemUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if filesystems == nil {
		t.Fatal("filesystem slice is nil")
	}

	if len(filesystems) == 0 {
		t.Skip("no eligible filesystems on this host")
	}

	t.Logf(
		"Collected %d filesystems",
		len(filesystems),
	)

	for _, filesystem := range filesystems {
		capacity := filesystem.Capacity

		if capacity.TotalBytes == 0 {
			t.Fatal("filesystem capacity is zero")
		}

		requireFilesystemAccounting(t, capacity)

		expectedUsage := float64(
			capacity.UsedBytes,
		) / float64(
			capacity.TotalBytes,
		) * 100

		requireMetricPercent(
			t,
			capacity.UsagePercent,
			expectedUsage,
		)

		t.Logf(
			"mount=%q source=%q type=%q readonly=%v",
			filesystem.MountPoint,
			filesystem.Source,
			filesystem.FilesystemType,
			filesystem.ReadOnly,
		)

		t.Logf(
			"total=%d free=%d available=%d used=%d reserved=%d utilization=%.2f%%",
			capacity.TotalBytes,
			capacity.FreeBytes,
			capacity.AvailableBytes,
			capacity.UsedBytes,
			capacity.ReservedBytes,
			capacity.UsagePercent,
		)
	}
}
