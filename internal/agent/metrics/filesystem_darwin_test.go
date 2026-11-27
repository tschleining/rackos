package metrics

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNormalizeDarwinFilesystemCapacity(t *testing.T) {
	for _, blockSize := range []uint32{
		512,
		4096,
		16384,
	} {
		statistics := unix.Statfs_t{
			Bsize:  blockSize,
			Blocks: 1000,
			Bfree:  350,
			Bavail: 300,
		}

		capacity, err := normalizeDarwinFilesystemCapacity(
			statistics,
		)
		if err != nil {
			t.Fatal(err)
		}

		size := uint64(blockSize)

		if capacity.TotalBytes != 1000*size ||
			capacity.FreeBytes != 350*size ||
			capacity.AvailableBytes != 300*size ||
			capacity.UsedBytes != 650*size ||
			capacity.ReservedBytes != 50*size {
			t.Fatalf(
				"incorrect capacity for block size %d: %+v",
				blockSize,
				capacity,
			)
		}

		requireMetricPercent(
			t,
			capacity.UsagePercent,
			65,
		)

		requireFilesystemAccounting(t, capacity)
	}
}

func TestNormalizeDarwinFilesystemCapacityInvalid(t *testing.T) {
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
			capacity, err := normalizeDarwinFilesystemCapacity(
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

// testDarwinFilesystemFixture creates matching
// discovery metadata and native capacity statistics.
func testDarwinFilesystemFixture() (
	darwinMount,
	unix.Statfs_t,
) {
	statistics := testDarwinMountStatistics(
		"/dev/disk3s5",
		"/System/Volumes/Data",
		"apfs",
		unix.MNT_LOCAL,
	)

	statistics.Fsid = unix.Fsid{
		Val: [2]int32{123, 456},
	}

	statistics.Bsize = 4096
	statistics.Blocks = 1000
	statistics.Bfree = 350
	statistics.Bavail = 300

	mount := darwinMount{
		Source:         "/dev/disk3s5",
		MountPoint:     "/System/Volumes/Data",
		FilesystemType: "apfs",
		FilesystemID:   statistics.Fsid,
	}

	return mount, statistics
}

func TestReadDarwinFilesystem(t *testing.T) {
	mount, statistics := testDarwinFilesystemFixture()

	filesystem, err := readDarwinFilesystemWithStatfs(
		context.Background(),
		mount,
		func(path string, result *unix.Statfs_t) error {
			if path != mount.MountPoint {
				t.Fatalf("unexpected mount path: %q", path)
			}

			*result = statistics
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if filesystem.Source != mount.Source ||
		filesystem.MountPoint != mount.MountPoint ||
		filesystem.FilesystemType != mount.FilesystemType ||
		filesystem.ReadOnly {
		t.Fatal("unexpected filesystem metadata")
	}

	if filesystem.Capacity.TotalBytes != 4096000 {
		t.Fatal("incorrect filesystem capacity")
	}

	requireFilesystemAccounting(
		t,
		filesystem.Capacity,
	)

	requireMetricPercent(
		t,
		filesystem.Capacity.UsagePercent,
		65,
	)
}

func TestReadDarwinFilesystemRefreshesReadOnly(t *testing.T) {
	mount, statistics := testDarwinFilesystemFixture()

	// The filesystem was writable during discovery
	// but became read-only before capacity collection.
	statistics.Flags |= unix.MNT_RDONLY

	filesystem, err := readDarwinFilesystemWithStatfs(
		context.Background(),
		mount,
		func(path string, result *unix.Statfs_t) error {
			*result = statistics
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !filesystem.ReadOnly {
		t.Fatal("read-only status was not refreshed")
	}
}

func TestReadDarwinFilesystemIdentityMismatch(t *testing.T) {
	tests := []struct {
		name   string
		change func(*unix.Statfs_t)
	}{
		{
			name: "different filesystem ID",
			change: func(s *unix.Statfs_t) {
				s.Fsid.Val[0]++
			},
		},
		{
			name: "different source",
			change: func(s *unix.Statfs_t) {
				clear(s.Mntfromname[:])
				copy(s.Mntfromname[:], "/dev/disk9")
			},
		},
		{
			name: "different mount point",
			change: func(s *unix.Statfs_t) {
				clear(s.Mntonname[:])
				copy(s.Mntonname[:], "/Volumes/Other")
			},
		},
		{
			name: "different filesystem type",
			change: func(s *unix.Statfs_t) {
				clear(s.Fstypename[:])
				copy(s.Fstypename[:], "hfs")
			},
		},
		{
			name: "filesystem no longer local",
			change: func(s *unix.Statfs_t) {
				s.Flags &^= unix.MNT_LOCAL
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mount, statistics := testDarwinFilesystemFixture()

			tt.change(&statistics)

			filesystem, err := readDarwinFilesystemWithStatfs(
				context.Background(),
				mount,
				func(path string, result *unix.Statfs_t) error {
					*result = statistics
					return nil
				},
			)

			if !errors.Is(err, errDarwinMountChanged) {
				t.Fatalf(
					"expected changed mount error, got %v",
					err,
				)
			}

			if filesystem != (FilesystemUsage{}) {
				t.Fatal("mismatched mount returned filesystem")
			}
		})
	}
}

func TestReadDarwinFilesystemNativeErrors(t *testing.T) {
	tests := []struct {
		name     string
		native   error
		expected error
	}{
		{"missing mount", unix.ENOENT, errDarwinMountChanged},
		{"not a directory", unix.ENOTDIR, errDarwinMountChanged},
		{"stale mount", unix.ESTALE, errDarwinMountChanged},
		{"permission denied", unix.EACCES, unix.EACCES},
		{"IO failure", unix.EIO, unix.EIO},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mount, _ := testDarwinFilesystemFixture()

			filesystem, err := readDarwinFilesystemWithStatfs(
				context.Background(),
				mount,
				func(path string, result *unix.Statfs_t) error {
					return tt.native
				},
			)

			if !errors.Is(err, tt.expected) {
				t.Fatalf(
					"expected %v, got %v",
					tt.expected,
					err,
				)
			}

			if filesystem != (FilesystemUsage{}) {
				t.Fatal("failed collection returned filesystem")
			}
		})
	}
}

func TestReadDarwinFilesystemInvalidCapacity(t *testing.T) {
	mount, statistics := testDarwinFilesystemFixture()

	statistics.Bsize = 0

	filesystem, err := readDarwinFilesystemWithStatfs(
		context.Background(),
		mount,
		func(path string, result *unix.Statfs_t) error {
			*result = statistics
			return nil
		},
	)

	if !errors.Is(err, ErrInvalidFilesystem) {
		t.Fatalf(
			"expected invalid filesystem error, got %v",
			err,
		)
	}

	if filesystem != (FilesystemUsage{}) {
		t.Fatal("invalid capacity returned filesystem")
	}
}

func TestReadDarwinFilesystemCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	mount, _ := testDarwinFilesystemFixture()

	filesystem, err := readDarwinFilesystemWithStatfs(
		ctx,
		mount,
		func(path string, result *unix.Statfs_t) error {
			t.Fatal("native reader invoked after cancellation")
			return nil
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	if filesystem != (FilesystemUsage{}) {
		t.Fatal("canceled read returned filesystem")
	}
}

func TestReadDarwinFilesystemCanceledDuringRead(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	mount, statistics := testDarwinFilesystemFixture()

	filesystem, err := readDarwinFilesystemWithStatfs(
		ctx,
		mount,
		func(path string, result *unix.Statfs_t) error {
			*result = statistics
			cancel()
			return nil
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	if filesystem != (FilesystemUsage{}) {
		t.Fatal("canceled read returned filesystem")
	}
}

func TestCollectDarwinFilesystemUsage(t *testing.T) {
	mounts := []darwinMount{
		{MountPoint: "/"},
		{MountPoint: "/System/Volumes/Data"},
		{MountPoint: "/Volumes/External"},
	}

	calls := 0

	filesystems, err := collectDarwinFilesystemUsage(
		context.Background(),
		mounts,
		func(
			ctx context.Context,
			mount darwinMount,
		) (FilesystemUsage, error) {
			calls++

			// Simulate an external volume disappearing.
			if mount.MountPoint == "/Volumes/External" {
				return FilesystemUsage{},
					errDarwinMountChanged
			}

			return FilesystemUsage{
				MountPoint: mount.MountPoint,
			}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 3 || len(filesystems) != 2 {
		t.Fatal("unexpected filesystem collection count")
	}

	if filesystems[0].MountPoint != "/" ||
		filesystems[1].MountPoint != "/System/Volumes/Data" {
		t.Fatal("unexpected collected filesystems")
	}
}

func TestCollectDarwinFilesystemUsageFailure(t *testing.T) {
	expectedError := errors.New(
		"filesystem statistics unavailable",
	)

	mounts := []darwinMount{
		{MountPoint: "/"},
		{MountPoint: "/System/Volumes/Data"},
		{MountPoint: "/Volumes/External"},
	}

	calls := 0

	filesystems, err := collectDarwinFilesystemUsage(
		context.Background(),
		mounts,
		func(
			ctx context.Context,
			mount darwinMount,
		) (FilesystemUsage, error) {
			calls++

			if calls == 2 {
				return FilesystemUsage{}, expectedError
			}

			return FilesystemUsage{
				MountPoint: mount.MountPoint,
			}, nil
		},
	)

	if !errors.Is(err, expectedError) {
		t.Fatalf("expected collection error, got %v", err)
	}

	if filesystems != nil || calls != 2 {
		t.Fatal("failed collection returned partial results")
	}
}

func TestCollectDarwinFilesystemUsageEmpty(t *testing.T) {
	filesystems, err := collectDarwinFilesystemUsage(
		context.Background(),
		nil,
		func(
			ctx context.Context,
			mount darwinMount,
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

func TestCollectDarwinFilesystemUsageCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	calls := 0

	filesystems, err := collectDarwinFilesystemUsage(
		ctx,
		[]darwinMount{
			{MountPoint: "/"},
			{MountPoint: "/System/Volumes/Data"},
		},
		func(
			ctx context.Context,
			mount darwinMount,
		) (FilesystemUsage, error) {
			calls++
			cancel()

			return FilesystemUsage{
				MountPoint: mount.MountPoint,
			}, nil
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	if filesystems != nil || calls != 1 {
		t.Fatal("collection continued after cancellation")
	}
}

func TestReadDarwinFilesystemUsageNative(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	filesystems, err := readFilesystemUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(filesystems) == 0 {
		t.Fatal("no monitored filesystems collected")
	}

	t.Logf(
		"Collected %d macOS filesystems",
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
