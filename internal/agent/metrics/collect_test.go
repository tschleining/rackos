package metrics

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type collectionFixture struct {
	readings []cpuCounters

	cpuCalls    int
	memoryCalls int
	waitCalls   int

	cpuErrorAt int
	cpuError   error

	memoryError error
	waitError   error

	memory MemoryUsage

	currentTime time.Time
	elapsed     time.Duration

	requestedInterval time.Duration

	calls []string
}

func newCollectionFixture() *collectionFixture {
	return &collectionFixture{
		readings: []cpuCounters{
			{Busy: 100, Idle: 200},
			{Busy: 125, Idle: 275},
		},
		memory: MemoryUsage{
			TotalBytes:     1000,
			AvailableBytes: 400,
			UsedBytes:      600,
			UsagePercent:   60,
		},
		currentTime: time.Date(
			2026, time.November, 26,
			18, 0, 0, 0,
			time.FixedZone("test", -5*60*60),
		),
		elapsed: 1025 * time.Millisecond,
	}
}

func (f *collectionFixture) readCPU(
	ctx context.Context,
) (cpuCounters, error) {
	f.cpuCalls++
	f.calls = append(f.calls, "cpu")

	if err := ctx.Err(); err != nil {
		return cpuCounters{}, err
	}

	if f.cpuCalls == f.cpuErrorAt {
		return cpuCounters{}, f.cpuError
	}

	if f.cpuCalls > len(f.readings) {
		return cpuCounters{}, errors.New(
			"unexpected CPU reading",
		)
	}

	return f.readings[f.cpuCalls-1], nil
}

func (f *collectionFixture) readMemory(
	ctx context.Context,
) (MemoryUsage, error) {
	f.memoryCalls++
	f.calls = append(f.calls, "memory")

	if err := ctx.Err(); err != nil {
		return MemoryUsage{}, err
	}

	if f.memoryError != nil {
		return MemoryUsage{}, f.memoryError
	}

	// Simulate time spent collecting memory.
	f.currentTime = f.currentTime.Add(
		5 * time.Millisecond,
	)

	return f.memory, nil
}

func (f *collectionFixture) wait(
	ctx context.Context,
	interval time.Duration,
) error {
	f.waitCalls++
	f.calls = append(f.calls, "wait")
	f.requestedInterval = interval

	if err := ctx.Err(); err != nil {
		return err
	}

	if f.waitError != nil {
		return f.waitError
	}

	f.currentTime = f.currentTime.Add(
		f.elapsed,
	)

	return nil
}

func (f *collectionFixture) now() time.Time {
	return f.currentTime
}

func (f *collectionFixture) collect(
	ctx context.Context,
) (Snapshot, error) {
	return collectWithReaders(
		ctx,
		f.readCPU,
		f.readMemory,
		f.wait,
		f.now,
	)
}

func TestCollectSnapshot(t *testing.T) {
	fixture := newCollectionFixture()

	startedAt := fixture.currentTime

	snapshot, err := fixture.collect(
		context.Background(),
	)
	if err != nil {
		t.Fatal(err)
	}

	requireMetricPercent(
		t,
		snapshot.CPU.UsagePercent,
		25,
	)

	if snapshot.CPU.SampleIntervalMillis != 1025 {
		t.Fatalf(
			"unexpected sample interval: %d",
			snapshot.CPU.SampleIntervalMillis,
		)
	}

	if snapshot.Memory != fixture.memory {
		t.Fatal("unexpected memory measurement")
	}

	expectedTime := startedAt.Add(
		1030 * time.Millisecond,
	)

	if !snapshot.CollectedAt.Equal(expectedTime) {
		t.Fatalf(
			"unexpected collection timestamp: %v",
			snapshot.CollectedAt,
		)
	}

	if snapshot.CollectedAt.Location() != time.UTC {
		t.Fatal("collection timestamp is not UTC")
	}

	if fixture.requestedInterval != time.Second {
		t.Fatal("unexpected requested sampling interval")
	}

	if fixture.cpuCalls != 2 ||
		fixture.memoryCalls != 1 ||
		fixture.waitCalls != 1 {
		t.Fatal("unexpected collector invocation count")
	}

	expectedCalls := []string{
		"cpu",
		"wait",
		"cpu",
		"memory",
	}

	if !reflect.DeepEqual(
		fixture.calls,
		expectedCalls,
	) {
		t.Fatalf(
			"unexpected collection order: %v",
			fixture.calls,
		)
	}
}

func TestCollectFailures(t *testing.T) {
	readError := errors.New("native collection failed")

	tests := []struct {
		name          string
		configure     func(*collectionFixture)
		expectedError error
		cpuCalls      int
		memoryCalls   int
		waitCalls     int
	}{
		{
			name: "initial CPU reading fails",
			configure: func(f *collectionFixture) {
				f.cpuErrorAt = 1
				f.cpuError = readError
			},
			expectedError: readError,
			cpuCalls:      1,
		},
		{
			name: "unsupported platform",
			configure: func(f *collectionFixture) {
				f.cpuErrorAt = 1
				f.cpuError = ErrUnsupportedPlatform
			},
			expectedError: ErrUnsupportedPlatform,
			cpuCalls:      1,
		},
		{
			name: "sampling wait fails",
			configure: func(f *collectionFixture) {
				f.waitError = context.DeadlineExceeded
			},
			expectedError: context.DeadlineExceeded,
			cpuCalls:      1,
			waitCalls:     1,
		},
		{
			name: "final CPU reading fails",
			configure: func(f *collectionFixture) {
				f.cpuErrorAt = 2
				f.cpuError = readError
			},
			expectedError: readError,
			cpuCalls:      2,
			waitCalls:     1,
		},
		{
			name: "CPU counters regress",
			configure: func(f *collectionFixture) {
				f.readings[1] = cpuCounters{
					Busy: 50,
					Idle: 100,
				}
			},
			expectedError: ErrInvalidCPUCounters,
			cpuCalls:      2,
			waitCalls:     1,
		},
		{
			name: "CPU counters unchanged",
			configure: func(f *collectionFixture) {
				f.readings[1] = f.readings[0]
			},
			expectedError: ErrInvalidCPUCounters,
			cpuCalls:      2,
			waitCalls:     1,
		},
		{
			name: "zero sampling interval",
			configure: func(f *collectionFixture) {
				f.elapsed = 0
			},
			expectedError: ErrInvalidCPUCounters,
			cpuCalls:      2,
			waitCalls:     1,
		},
		{
			name: "negative sampling interval",
			configure: func(f *collectionFixture) {
				f.elapsed = -time.Second
			},
			expectedError: ErrInvalidCPUCounters,
			cpuCalls:      2,
			waitCalls:     1,
		},
		{
			name: "memory reading fails",
			configure: func(f *collectionFixture) {
				f.memoryError = readError
			},
			expectedError: readError,
			cpuCalls:      2,
			memoryCalls:   1,
			waitCalls:     1,
		},
		{
			name: "invalid memory information",
			configure: func(f *collectionFixture) {
				f.memoryError = ErrInvalidMemory
			},
			expectedError: ErrInvalidMemory,
			cpuCalls:      2,
			memoryCalls:   1,
			waitCalls:     1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newCollectionFixture()
			tt.configure(fixture)

			snapshot, err := fixture.collect(
				context.Background(),
			)

			if !errors.Is(err, tt.expectedError) {
				t.Fatalf(
					"expected %v, got %v",
					tt.expectedError,
					err,
				)
			}

			if snapshot != (Snapshot{}) {
				t.Fatal("failed collection returned a snapshot")
			}

			if fixture.cpuCalls != tt.cpuCalls {
				t.Fatalf(
					"unexpected CPU calls: %d",
					fixture.cpuCalls,
				)
			}

			if fixture.memoryCalls != tt.memoryCalls {
				t.Fatalf(
					"unexpected memory calls: %d",
					fixture.memoryCalls,
				)
			}

			if fixture.waitCalls != tt.waitCalls {
				t.Fatalf(
					"unexpected wait calls: %d",
					fixture.waitCalls,
				)
			}
		})
	}
}

func TestCollectCanceledBeforeStart(t *testing.T) {
	fixture := newCollectionFixture()

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	snapshot, err := fixture.collect(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if snapshot != (Snapshot{}) {
		t.Fatal("canceled collection returned a snapshot")
	}

	if len(fixture.calls) != 0 {
		t.Fatal("collectors invoked after cancellation")
	}
}

func TestCollectCanceledDuringWait(t *testing.T) {
	fixture := newCollectionFixture()

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	snapshot, err := collectWithReaders(
		ctx,
		fixture.readCPU,
		fixture.readMemory,
		func(
			ctx context.Context,
			interval time.Duration,
		) error {
			cancel()
			return ctx.Err()
		},
		fixture.now,
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if snapshot != (Snapshot{}) {
		t.Fatal("canceled collection returned a snapshot")
	}

	if fixture.cpuCalls != 1 ||
		fixture.memoryCalls != 0 {
		t.Fatal("collection continued after cancellation")
	}
}

func TestCollectCanceledDuringMemoryRead(t *testing.T) {
	fixture := newCollectionFixture()

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	snapshot, err := collectWithReaders(
		ctx,
		fixture.readCPU,
		func(ctx context.Context) (MemoryUsage, error) {
			cancel()

			// Simulate a native call that completed
			// despite context cancellation.
			return fixture.memory, nil
		},
		fixture.wait,
		fixture.now,
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if snapshot != (Snapshot{}) {
		t.Fatal("canceled collection returned a snapshot")
	}
}

func TestWaitForCPUSampleElapsed(t *testing.T) {
	interval := 10 * time.Millisecond

	startedAt := time.Now()

	err := waitForCPUSample(
		context.Background(),
		interval,
	)
	if err != nil {
		t.Fatal(err)
	}

	if time.Since(startedAt) < interval {
		t.Fatal("sampling timer returned prematurely")
	}
}

func TestWaitForCPUSampleCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	err := waitForCPUSample(
		ctx,
		time.Hour,
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}
}

func TestWaitForCPUSampleDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		20*time.Millisecond,
	)
	defer cancel()

	err := waitForCPUSample(
		ctx,
		time.Hour,
	)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf(
			"expected context.DeadlineExceeded, got %v",
			err,
		)
	}
}
