package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestCollectNetworkSnapshot(t *testing.T) {
	start := time.Date(
		2026, 1, 1, 12, 0, 0, 0,
		time.FixedZone("EST", -5*60*60),
	)

	previous, current := testNetworkRateSamples()

	base := Snapshot{
		CPU: CPUUsage{
			UsagePercent:         25,
			SampleIntervalMillis: 1000,
		},

		Memory: MemoryUsage{
			TotalBytes:     10000,
			AvailableBytes: 4000,
			UsedBytes:      6000,
			UsagePercent:   60,
		},

		Storage: &StorageUsage{
			Filesystems: []FilesystemUsage{},
			Disks:       []DiskUsage{},
			DiskRates:   []DiskRate{},
		},

		CollectedAt: start.Add(
			1100 * time.Millisecond,
		).UTC(),
	}

	var calls []string

	readCount := 0
	clockCount := 0

	times := []time.Time{
		start,
		start.Add(1250 * time.Millisecond),
		start.Add(1300 * time.Millisecond),
	}

	snapshot, err := collectWithNetworkReaders(
		context.Background(),
		func(context.Context) (Snapshot, error) {
			calls = append(calls, "performance")
			return base, nil
		},
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			readCount++

			switch readCount {
			case 1:
				calls = append(calls, "initial-network")

				return []NetworkInterfaceUsage{
					previous,
				}, nil

			case 2:
				calls = append(calls, "final-network")

				return []NetworkInterfaceUsage{
					current,
					{
						Name:  "new0",
						Index: 10,
					},
				}, nil

			default:
				t.Fatal("unexpected network read")
				return nil, nil
			}
		},
		func() time.Time {
			calls = append(calls, "clock")

			if clockCount >= len(times) {
				t.Fatal("unexpected clock read")
			}

			value := times[clockCount]
			clockCount++

			return value
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	expectedCalls := []string{
		"initial-network",
		"clock",
		"performance",
		"final-network",
		"clock",
		"clock",
	}

	if !reflect.DeepEqual(calls, expectedCalls) {
		t.Fatalf("unexpected collection order: %v", calls)
	}

	if snapshot.CPU != base.CPU {
		t.Fatal("CPU measurements changed")
	}

	if snapshot.Memory != base.Memory {
		t.Fatal("memory measurements changed")
	}

	if !reflect.DeepEqual(snapshot.Storage, base.Storage) {
		t.Fatal("storage measurements changed")
	}

	if base.Network != nil {
		t.Fatal("original snapshot was modified")
	}

	if snapshot.Network == nil {
		t.Fatal("missing network measurements")
	}

	expectedInterfaces := []NetworkInterfaceUsage{
		current,
		{
			Name:  "new0",
			Index: 10,
		},
	}

	if !reflect.DeepEqual(
		snapshot.Network.Interfaces,
		expectedInterfaces,
	) {
		t.Fatal("incorrect cumulative network measurements")
	}

	if len(snapshot.Network.InterfaceRates) != 1 {
		t.Fatal("incorrect network rate count")
	}

	expectedRate := NetworkInterfaceRate{
		Name:                     "eth0",
		Index:                    2,
		ReceiveBytesPerSecond:    4800,
		TransmitBytesPerSecond:   2400,
		ReceivePacketsPerSecond:  48,
		TransmitPacketsPerSecond: 24,
		SampleIntervalMillis:     1250,
	}

	requireNetworkRate(
		t,
		snapshot.Network.InterfaceRates[0],
		expectedRate,
	)

	expectedTime := start.Add(
		1300 * time.Millisecond,
	).UTC()

	if !snapshot.CollectedAt.Equal(expectedTime) {
		t.Fatal("incorrect snapshot completion time")
	}

	if snapshot.CollectedAt.Location() != time.UTC {
		t.Fatal("snapshot timestamp is not UTC")
	}

	// Verify that the complete unified snapshot
	// retains its values through JSON serialization.
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Snapshot

	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(snapshot, decoded) {
		t.Fatal("unified snapshot JSON round trip failed")
	}
}

func TestCollectNetworkFailures(t *testing.T) {
	expectedError := errors.New("collection failed")

	tests := []struct {
		name         string
		failStage    string
		wantReads    int
		wantPerfRuns int
	}{
		{
			name:      "initial network failure",
			failStage: "initial",
			wantReads: 1,
		},
		{
			name:         "performance collection failure",
			failStage:    "performance",
			wantReads:    1,
			wantPerfRuns: 1,
		},
		{
			name:         "final network failure",
			failStage:    "final",
			wantReads:    2,
			wantPerfRuns: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reads := 0
			performanceRuns := 0

			snapshot, err := collectWithNetworkReaders(
				context.Background(),
				func(context.Context) (Snapshot, error) {
					performanceRuns++

					if tt.failStage == "performance" {
						return Snapshot{}, expectedError
					}

					return Snapshot{
						CPU: CPUUsage{
							UsagePercent: 25,
						},
					}, nil
				},
				func(context.Context) ([]NetworkInterfaceUsage, error) {
					reads++

					if reads == 1 &&
						tt.failStage == "initial" {
						return nil, expectedError
					}

					if reads == 2 &&
						tt.failStage == "final" {
						return nil, expectedError
					}

					return []NetworkInterfaceUsage{
						{Name: "eth0", Index: 2},
					}, nil
				},
				time.Now,
			)

			if !errors.Is(err, expectedError) {
				t.Fatalf(
					"expected collection error, got %v",
					err,
				)
			}

			if snapshot != (Snapshot{}) {
				t.Fatal("failure returned partial snapshot")
			}

			if reads != tt.wantReads {
				t.Fatal("incorrect network read count")
			}

			if performanceRuns != tt.wantPerfRuns {
				t.Fatal("incorrect performance collection count")
			}
		})
	}
}

func TestCollectNetworkCanceled(t *testing.T) {
	for _, stage := range []string{
		"before",
		"initial",
		"performance",
		"final",
	} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(
				context.Background(),
			)
			defer cancel()

			if stage == "before" {
				cancel()
			}

			reads := 0
			performanceRuns := 0

			snapshot, err := collectWithNetworkReaders(
				ctx,
				func(context.Context) (Snapshot, error) {
					performanceRuns++

					if stage == "performance" {
						cancel()
					}

					return Snapshot{}, nil
				},
				func(context.Context) ([]NetworkInterfaceUsage, error) {
					reads++

					if reads == 1 && stage == "initial" {
						cancel()
					}

					if reads == 2 && stage == "final" {
						cancel()
					}

					return []NetworkInterfaceUsage{
						{Name: "eth0", Index: 2},
					}, nil
				},
				time.Now,
			)

			if !errors.Is(err, context.Canceled) {
				t.Fatalf(
					"expected cancellation, got %v",
					err,
				)
			}

			if snapshot != (Snapshot{}) {
				t.Fatal("cancellation returned partial snapshot")
			}

			expectedReads := map[string]int{
				"before":      0,
				"initial":     1,
				"performance": 1,
				"final":       2,
			}

			expectedPerfRuns := map[string]int{
				"before":      0,
				"initial":     0,
				"performance": 1,
				"final":       1,
			}

			if reads != expectedReads[stage] {
				t.Fatal("collection continued after cancellation")
			}

			if performanceRuns != expectedPerfRuns[stage] {
				t.Fatal("unexpected performance collection")
			}
		})
	}
}

func TestCollectNetworkInvalidInterval(t *testing.T) {
	start := time.Now()

	for _, interval := range []time.Duration{
		0,
		-time.Second,
		999 * time.Microsecond,
	} {
		clockReads := 0

		snapshot, err := collectWithNetworkReaders(
			context.Background(),
			func(context.Context) (Snapshot, error) {
				return Snapshot{}, nil
			},
			func(context.Context) ([]NetworkInterfaceUsage, error) {
				return []NetworkInterfaceUsage{
					{Name: "eth0", Index: 2},
				}, nil
			},
			func() time.Time {
				clockReads++

				if clockReads == 1 {
					return start
				}

				return start.Add(interval)
			},
		)

		if !errors.Is(err, ErrInvalidNetworkSample) {
			t.Fatalf(
				"interval %s: expected invalid sample, got %v",
				interval,
				err,
			)
		}

		if snapshot != (Snapshot{}) {
			t.Fatal("invalid interval returned snapshot")
		}
	}
}

func TestCollectNetworkInvalidInterfaces(t *testing.T) {
	for _, invalidRead := range []int{1, 2} {
		reads := 0
		clockReads := 0
		start := time.Now()

		snapshot, err := collectWithNetworkReaders(
			context.Background(),
			func(context.Context) (Snapshot, error) {
				return Snapshot{}, nil
			},
			func(context.Context) ([]NetworkInterfaceUsage, error) {
				reads++

				if reads == invalidRead {
					return []NetworkInterfaceUsage{
						{Name: "eth0", Index: 2},
						{Name: "eth0", Index: 3},
					}, nil
				}

				return []NetworkInterfaceUsage{
					{Name: "eth0", Index: 2},
				}, nil
			},
			func() time.Time {
				value := start.Add(
					time.Duration(clockReads) * time.Second,
				)

				clockReads++

				return value
			},
		)

		if !errors.Is(err, ErrInvalidNetworkSample) {
			t.Fatalf("expected invalid sample, got %v", err)
		}

		if !errors.Is(err, ErrInvalidNetworkStatistics) {
			t.Fatal("underlying validation error was lost")
		}

		if snapshot != (Snapshot{}) {
			t.Fatal("invalid interfaces returned snapshot")
		}
	}
}

func TestCollectNetworkEmpty(t *testing.T) {
	start := time.Now()
	clockReads := 0

	snapshot, err := collectWithNetworkReaders(
		context.Background(),
		func(context.Context) (Snapshot, error) {
			return Snapshot{}, nil
		},
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			return nil, nil
		},
		func() time.Time {
			value := start.Add(
				time.Duration(clockReads) * time.Second,
			)

			clockReads++

			return value
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.Network == nil {
		t.Fatal("missing network object")
	}

	if snapshot.Network.Interfaces == nil ||
		len(snapshot.Network.Interfaces) != 0 {
		t.Fatal("expected initialized empty interface slice")
	}

	if snapshot.Network.InterfaceRates == nil ||
		len(snapshot.Network.InterfaceRates) != 0 {
		t.Fatal("expected initialized empty rate slice")
	}

	data, err := json.Marshal(snapshot.Network)
	if err != nil {
		t.Fatal(err)
	}

	const expected = `{"interfaces":[],"interface_rates":[]}`

	if string(data) != expected {
		t.Fatalf("unexpected empty network JSON: %s", data)
	}
}

func TestCollectNetworkCounterReset(t *testing.T) {
	previous := []NetworkInterfaceUsage{
		{Name: "eth0", Index: 2, ReceivedBytes: 10000},
		{Name: "eth1", Index: 3, ReceivedBytes: 5000},
	}

	current := []NetworkInterfaceUsage{
		{Name: "eth0", Index: 2, ReceivedBytes: 100},
		{Name: "eth1", Index: 3, ReceivedBytes: 6000},
	}

	start := time.Now()
	reads := 0
	clockReads := 0

	snapshot, err := collectWithNetworkReaders(
		context.Background(),
		func(context.Context) (Snapshot, error) {
			return Snapshot{}, nil
		},
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			reads++

			if reads == 1 {
				return previous, nil
			}

			return current, nil
		},
		func() time.Time {
			value := start.Add(
				time.Duration(clockReads) * time.Second,
			)

			clockReads++

			return value
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.Network == nil {
		t.Fatal("missing network measurements")
	}

	if !reflect.DeepEqual(
		snapshot.Network.Interfaces,
		current,
	) {
		t.Fatal("current cumulative counters were lost")
	}

	if len(snapshot.Network.InterfaceRates) != 1 {
		t.Fatal("incorrect number of valid rates")
	}

	rate := snapshot.Network.InterfaceRates[0]

	if rate.Name != "eth1" ||
		rate.ReceiveBytesPerSecond != 1000 {
		t.Fatal("valid interface rate was not preserved")
	}
}
