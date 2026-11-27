package metrics

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func testDarwinMountStatistics(
	source string,
	mountPoint string,
	filesystemType string,
	flags uint32,
) unix.Statfs_t {
	var statistics unix.Statfs_t

	copy(statistics.Mntfromname[:], source)
	copy(statistics.Mntonname[:], mountPoint)
	copy(statistics.Fstypename[:], filesystemType)

	statistics.Flags = flags

	return statistics
}

func TestFilterDarwinMounts(t *testing.T) {
	statistics := []unix.Statfs_t{
		testDarwinMountStatistics(
			"/dev/disk4s1",
			"/Volumes/External Drive",
			"exfat",
			unix.MNT_LOCAL,
		),
		testDarwinMountStatistics(
			"/dev/disk3s1s1",
			"/",
			"apfs",
			unix.MNT_LOCAL|unix.MNT_RDONLY,
		),
		testDarwinMountStatistics(
			"/dev/disk3s5",
			"/System/Volumes/Data",
			"apfs",
			unix.MNT_LOCAL,
		),
		testDarwinMountStatistics(
			"devfs",
			"/dev",
			"devfs",
			unix.MNT_LOCAL,
		),
		testDarwinMountStatistics(
			"server:/backup",
			"/Volumes/Remote",
			"nfs",
			0,
		),
		testDarwinMountStatistics(
			"map auto_home",
			"/System/Volumes/Data/home",
			"autofs",
			0,
		),
	}

	statistics[1].Fsid = unix.Fsid{
		Val: [2]int32{123, 456},
	}

	mounts, err := filterDarwinMounts(statistics)
	if err != nil {
		t.Fatal(err)
	}

	expected := []darwinMount{
		{
			Source:         "/dev/disk3s1s1",
			MountPoint:     "/",
			FilesystemType: "apfs",
			ReadOnly:       true,
			FilesystemID: unix.Fsid{
				Val: [2]int32{123, 456},
			},
		},
		{
			Source:         "/dev/disk3s5",
			MountPoint:     "/System/Volumes/Data",
			FilesystemType: "apfs",
		},
		{
			Source:         "/dev/disk4s1",
			MountPoint:     "/Volumes/External Drive",
			FilesystemType: "exfat",
		},
	}

	if !reflect.DeepEqual(mounts, expected) {
		t.Fatalf(
			"expected %+v, got %+v",
			expected,
			mounts,
		)
	}
}

func TestIsMonitoredDarwinFilesystem(t *testing.T) {
	tests := []struct {
		name     string
		fsType   string
		flags    uint32
		expected bool
	}{
		{"APFS", "apfs", unix.MNT_LOCAL, true},
		{"HFS", "hfs", unix.MNT_LOCAL, true},
		{"FAT", "msdos", unix.MNT_LOCAL, true},
		{"exFAT", "exfat", unix.MNT_LOCAL, true},
		{"NTFS", "ntfs", unix.MNT_LOCAL, true},
		{"ZFS", "zfs", unix.MNT_LOCAL, true},
		{"UDF", "udf", unix.MNT_LOCAL, true},
		{
			"readonly APFS",
			"apfs",
			unix.MNT_LOCAL | unix.MNT_RDONLY,
			true,
		},
		{"nonlocal APFS", "apfs", 0, false},
		{"devfs", "devfs", unix.MNT_LOCAL, false},
		{"autofs", "autofs", unix.MNT_LOCAL, false},
		{"tmpfs", "tmpfs", unix.MNT_LOCAL, false},
		{"NFS", "nfs", 0, false},
		{"SMB", "smbfs", 0, false},
		{"unknown", "unknown", unix.MNT_LOCAL, false},
		{"empty type", "", unix.MNT_LOCAL, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := isMonitoredDarwinFilesystem(
				tt.fsType,
				tt.flags,
			)

			if actual != tt.expected {
				t.Fatalf(
					"expected %v, got %v",
					tt.expected,
					actual,
				)
			}
		})
	}
}

func TestFilterDarwinMountsInvalid(t *testing.T) {
	tests := []struct {
		name       string
		statistics unix.Statfs_t
	}{
		{
			name: "empty source",
			statistics: testDarwinMountStatistics(
				"",
				"/",
				"apfs",
				unix.MNT_LOCAL,
			),
		},
		{
			name: "empty mount point",
			statistics: testDarwinMountStatistics(
				"/dev/disk0",
				"",
				"apfs",
				unix.MNT_LOCAL,
			),
		},
		{
			name: "relative mount point",
			statistics: testDarwinMountStatistics(
				"/dev/disk0",
				"relative/path",
				"apfs",
				unix.MNT_LOCAL,
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mounts, err := filterDarwinMounts(
				[]unix.Statfs_t{tt.statistics},
			)

			if !errors.Is(err, errInvalidDarwinMountInfo) {
				t.Fatalf(
					"expected invalid mount information, got %v",
					err,
				)
			}

			if mounts != nil {
				t.Fatal("invalid statistics returned mounts")
			}
		})
	}
}

func TestFilterDarwinMountsEmpty(t *testing.T) {
	mounts, err := filterDarwinMounts(nil)
	if err != nil {
		t.Fatal(err)
	}

	if mounts == nil || len(mounts) != 0 {
		t.Fatal("expected initialized empty mount slice")
	}
}

func TestReadDarwinMountStatistics(t *testing.T) {
	expected := testDarwinMountStatistics(
		"/dev/disk3s1s1",
		"/",
		"apfs",
		unix.MNT_LOCAL,
	)

	calls := 0

	statistics, err := readDarwinMountStatistics(
		context.Background(),
		func(
			buffer []unix.Statfs_t,
			flags int,
		) (int, error) {
			calls++

			if flags != unix.MNT_NOWAIT {
				t.Fatal("unexpected getfsstat flags")
			}

			if buffer == nil {
				return 1, nil
			}

			if len(buffer) <= 1 {
				t.Fatal("mount buffer lacks extra capacity")
			}

			buffer[0] = expected

			return 1, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 2 {
		t.Fatalf("unexpected call count: %d", calls)
	}

	if len(statistics) != 1 ||
		statistics[0] != expected {
		t.Fatal("unexpected filesystem statistics")
	}
}

func TestReadDarwinMountStatisticsRetries(t *testing.T) {
	calls := 0
	previousSize := 0

	statistics, err := readDarwinMountStatistics(
		context.Background(),
		func(
			buffer []unix.Statfs_t,
			flags int,
		) (int, error) {
			calls++

			switch calls {
			case 1:
				return 1, nil

			case 2:
				previousSize = len(buffer)

				// Simulate a full buffer.
				return len(buffer), nil

			case 3:
				if len(buffer) <= previousSize {
					t.Fatal("mount buffer did not grow")
				}

				return 2, nil

			default:
				t.Fatal("unexpected getfsstat invocation")
				return 0, nil
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 3 || len(statistics) != 2 {
		t.Fatal("unexpected retry results")
	}
}

func TestReadDarwinMountStatisticsRetryLimit(t *testing.T) {
	calls := 0

	statistics, err := readDarwinMountStatistics(
		context.Background(),
		func(
			buffer []unix.Statfs_t,
			flags int,
		) (int, error) {
			calls++

			if buffer == nil {
				return 1, nil
			}

			// Every allocated buffer is full.
			return len(buffer), nil
		},
	)

	if !errors.Is(err, errInvalidDarwinMountInfo) {
		t.Fatalf(
			"expected retry limit error, got %v",
			err,
		)
	}

	if statistics != nil {
		t.Fatal("truncated mount table returned")
	}

	if calls != darwinMountAttempts+1 {
		t.Fatalf("unexpected call count: %d", calls)
	}
}

func TestReadDarwinMountStatisticsInvalidCount(t *testing.T) {
	for _, count := range []int{
		-1,
		darwinMaxMounts + 1,
	} {
		statistics, err := readDarwinMountStatistics(
			context.Background(),
			func(
				buffer []unix.Statfs_t,
				flags int,
			) (int, error) {
				return count, nil
			},
		)

		if !errors.Is(err, errInvalidDarwinMountInfo) {
			t.Fatalf(
				"expected invalid count error, got %v",
				err,
			)
		}

		if statistics != nil {
			t.Fatal("invalid count returned statistics")
		}
	}
}

func TestReadDarwinMountStatisticsFailure(t *testing.T) {
	expectedError := errors.New("getfsstat failed")

	for _, failureCall := range []int{1, 2} {
		calls := 0

		statistics, err := readDarwinMountStatistics(
			context.Background(),
			func(
				buffer []unix.Statfs_t,
				flags int,
			) (int, error) {
				calls++

				if calls == failureCall {
					return 0, expectedError
				}

				return 1, nil
			},
		)

		if !errors.Is(err, expectedError) {
			t.Fatalf(
				"expected native error, got %v",
				err,
			)
		}

		if statistics != nil {
			t.Fatal("failed read returned statistics")
		}
	}
}

func TestReadDarwinMountStatisticsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	calls := 0

	statistics, err := readDarwinMountStatistics(
		ctx,
		func(
			buffer []unix.Statfs_t,
			flags int,
		) (int, error) {
			calls++

			if buffer == nil {
				return 1, nil
			}

			cancel()

			return 1, nil
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if statistics != nil || calls != 2 {
		t.Fatal("unexpected canceled collection result")
	}
}

func TestDiscoverDarwinMountsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	mounts, err := discoverDarwinMounts(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if mounts != nil {
		t.Fatal("canceled discovery returned mounts")
	}
}

func TestDiscoverDarwinMountsNative(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	mounts, err := discoverDarwinMounts(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if mounts == nil {
		t.Fatal("mount slice is nil")
	}

	if len(mounts) == 0 {
		t.Fatal("no monitored filesystems discovered")
	}

	t.Logf(
		"Discovered %d monitored macOS filesystems",
		len(mounts),
	)

	for _, mount := range mounts {
		if mount.Source == "" ||
			mount.MountPoint == "" ||
			mount.FilesystemType == "" {
			t.Fatal("incomplete mount metadata")
		}

		t.Logf(
			"mount=%q source=%q type=%q readonly=%v fsid=%v",
			mount.MountPoint,
			mount.Source,
			mount.FilesystemType,
			mount.ReadOnly,
			mount.FilesystemID.Val,
		)
	}
}
