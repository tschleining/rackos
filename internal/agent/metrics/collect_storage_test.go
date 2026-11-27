package metrics

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestCollectWithStorageReaders(t *testing.T) {
	start := time.Date(
		2026, 11, 26, 18, 0, 0, 0,
		time.FixedZone("EST", -5*60*60),
	)

	performance := Snapshot{
		CPU: CPUUsage{
			UsagePercent:         25,
			SampleIntervalMillis: 1000,
		},
		Memory: MemoryUsage{
			TotalBytes:     1000,
			AvailableBytes: 400,
			UsedBytes:      600,
			UsagePercent:   60,
		},
		CollectedAt: start.Add(time.Second).UTC(),
	}

	previousDisks := []DiskUsage{
		{
			Device:          "sda",
			ReadBytes:       1000,
			WriteBytes:      2000,
			ReadOperations:  10,
			WriteOperations: 20,
		},
	}

	currentDisks := []DiskUsage{
		{
			Device:          "sda",
			ReadBytes:       6000,
			WriteBytes:      4500,
			ReadOperations:  15,
			WriteOperations: 30,
		},
		{
			// A newly attached disk has no baseline.
			Device:    "sdb",
			ReadBytes: 500,
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

	filesystems := []FilesystemUsage{
		{
			Source:         "/dev/sda1",
			MountPoint:     "/",
			FilesystemType: "ext4",
			Capacity:       capacity,
		},
	}

	timestamps := []time.Time{
		start,
		start.Add(1250 * time.Millisecond),
		start.Add(1400 * time.Millisecond),
	}

	var calls []string

	diskReads := 0
	clockReads := 0

	snapshot, err := collectWithStorageReaders(
		context.Background(),
		func(context.Context) (Snapshot, error) {
			calls = append(calls, "performance")

			return performance, nil
		},
		func(context.Context) ([]DiskUsage, error) {
			diskReads++

			switch diskReads {
			case 1:
				calls = append(calls, "initial disks")
				return previousDisks, nil

			case 2:
				calls = append(calls, "final disks")
				return currentDisks, nil

			default:
				t.Fatal("unexpected disk reader invocation")
				return nil, nil
			}
		},
		func(context.Context) ([]FilesystemUsage, error) {
			calls = append(calls, "filesystems")

			return filesystems, nil
		},
		func() time.Time {
			if clockReads >= len(timestamps) {
				t.Fatal("unexpected clock invocation")
			}

			value := timestamps[clockReads]
			clockReads++

			return value
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	expectedCalls := []string{
		"initial disks",
		"performance",
		"final disks",
		"filesystems",
	}

	if !reflect.DeepEqual(calls, expectedCalls) {
		t.Fatalf("unexpected collection order: %v", calls)
	}

	if diskReads != 2 || clockReads != 3 {
		t.Fatal("unexpected reader invocation count")
	}

	if snapshot.CPU != performance.CPU {
		t.Fatal("CPU measurement was modified")
	}

	if snapshot.Memory != performance.Memory {
		t.Fatal("memory measurement was modified")
	}

	expectedStorage := &StorageUsage{
		Filesystems: filesystems,
		Disks:       currentDisks,
		DiskRates: []DiskRate{
			{
				Device:                   "sda",
				ReadBytesPerSecond:       4000,
				WriteBytesPerSecond:      2000,
				ReadOperationsPerSecond:  4,
				WriteOperationsPerSecond: 8,
				SampleIntervalMillis:     1250,
			},
		},
	}

	if !reflect.DeepEqual(
		snapshot.Storage,
		expectedStorage,
	) {
		t.Fatalf(
			"unexpected storage measurements: %+v",
			snapshot.Storage,
		)
	}

	expectedTimestamp := timestamps[2].UTC()

	if snapshot.CollectedAt != expectedTimestamp {
		t.Fatalf(
			"expected timestamp %v, got %v",
			expectedTimestamp,
			snapshot.CollectedAt,
		)
	}

	if snapshot.CollectedAt.Location() != time.UTC {
		t.Fatal("snapshot timestamp is not UTC")
	}
}

func TestCollectWithStorageReadersFailures(t *testing.T) {
	expectedError := errors.New("collection failed")

	for _, failureStage := range []int{1, 2, 3, 4} {
		t.Run(
			[]string{
				"initial disks",
				"performance",
				"final disks",
				"filesystems",
			}[failureStage-1],
			func(t *testing.T) {
				calls := 0

				checkFailure := func() error {
					calls++

					if calls == failureStage {
						return expectedError
					}

					return nil
				}

				snapshot, err := collectWithStorageReaders(
					context.Background(),
					func(context.Context) (Snapshot, error) {
						return Snapshot{}, checkFailure()
					},
					func(context.Context) ([]DiskUsage, error) {
						return nil, checkFailure()
					},
					func(context.Context) ([]FilesystemUsage, error) {
						return nil, checkFailure()
					},
					testStorageClock(),
				)

				if !errors.Is(err, expectedError) {
					t.Fatalf(
						"expected collection failure, got %v",
						err,
					)
				}

				if snapshot != (Snapshot{}) {
					t.Fatal("failed collection returned snapshot")
				}

				if calls != failureStage {
					t.Fatal("collection continued after failure")
				}
			},
		)
	}
}

func TestCollectWithStorageReadersCancellation(t *testing.T) {
	for _, cancelStage := range []int{0, 1, 2, 3, 4} {
		t.Run(
			[]string{
				"before collection",
				"initial disks",
				"performance",
				"final disks",
				"filesystems",
			}[cancelStage],
			func(t *testing.T) {
				ctx, cancel := context.WithCancel(
					context.Background(),
				)
				defer cancel()

				calls := 0

				if cancelStage == 0 {
					cancel()
				}

				checkCancellation := func() {
					calls++

					if calls == cancelStage {
						cancel()
					}
				}

				snapshot, err := collectWithStorageReaders(
					ctx,
					func(context.Context) (Snapshot, error) {
						checkCancellation()
						return Snapshot{}, nil
					},
					func(context.Context) ([]DiskUsage, error) {
						checkCancellation()
						return nil, nil
					},
					func(context.Context) ([]FilesystemUsage, error) {
						checkCancellation()
						return nil, nil
					},
					testStorageClock(),
				)

				if !errors.Is(err, context.Canceled) {
					t.Fatalf(
						"expected cancellation, got %v",
						err,
					)
				}

				if snapshot != (Snapshot{}) {
					t.Fatal("canceled collection returned snapshot")
				}

				if calls != cancelStage {
					t.Fatal("collection continued after cancellation")
				}
			},
		)
	}
}

func TestCollectWithStorageReadersInvalidInterval(t *testing.T) {
	fixedTime := time.Now()

	snapshot, err := collectWithStorageReaders(
		context.Background(),
		func(context.Context) (Snapshot, error) {
			return Snapshot{}, nil
		},
		func(context.Context) ([]DiskUsage, error) {
			return nil, nil
		},
		func(context.Context) ([]FilesystemUsage, error) {
			t.Fatal("filesystem reader invoked after invalid interval")
			return nil, nil
		},
		func() time.Time {
			return fixedTime
		},
	)

	if !errors.Is(err, ErrInvalidDiskSample) {
		t.Fatalf(
			"expected invalid disk sample, got %v",
			err,
		)
	}

	if snapshot != (Snapshot{}) {
		t.Fatal("invalid interval returned snapshot")
	}
}

func TestCollectWithStorageReadersInvalidDiskSample(t *testing.T) {
	snapshot, err := collectWithStorageReaders(
		context.Background(),
		func(context.Context) (Snapshot, error) {
			return Snapshot{}, nil
		},
		func(context.Context) ([]DiskUsage, error) {
			return []DiskUsage{
				{Device: "sda"},
				{Device: "sda"},
			}, nil
		},
		func(context.Context) ([]FilesystemUsage, error) {
			t.Fatal("filesystem reader invoked after invalid disk sample")
			return nil, nil
		},
		testStorageClock(),
	)

	if !errors.Is(err, ErrInvalidDiskSample) {
		t.Fatalf(
			"expected invalid disk sample, got %v",
			err,
		)
	}

	if snapshot != (Snapshot{}) {
		t.Fatal("invalid disk sample returned snapshot")
	}
}

func TestCollectWithStorageReadersEmpty(t *testing.T) {
	snapshot, err := collectWithStorageReaders(
		context.Background(),
		func(context.Context) (Snapshot, error) {
			return Snapshot{}, nil
		},
		func(context.Context) ([]DiskUsage, error) {
			return nil, nil
		},
		func(context.Context) ([]FilesystemUsage, error) {
			return nil, nil
		},
		testStorageClock(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.Storage == nil {
		t.Fatal("storage measurements are missing")
	}

	if snapshot.Storage.Filesystems == nil ||
		len(snapshot.Storage.Filesystems) != 0 {
		t.Fatal("expected initialized empty filesystem slice")
	}

	if snapshot.Storage.Disks == nil ||
		len(snapshot.Storage.Disks) != 0 {
		t.Fatal("expected initialized empty disk slice")
	}

	if snapshot.Storage.DiskRates == nil ||
		len(snapshot.Storage.DiskRates) != 0 {
		t.Fatal("expected initialized empty disk rate slice")
	}
}

// testStorageClock provides deterministic timestamps
// without sleeping or accessing the system clock.
func testStorageClock() func() time.Time {
	current := time.Date(
		2026, 11, 26, 23, 0, 0, 0,
		time.UTC,
	)

	return func() time.Time {
		value := current
		current = current.Add(time.Second)
		return value
	}
}
