package metrics

import (
	"errors"
	"math"
	"testing"
)

func TestNormalizeDarwinCPUCounters(t *testing.T) {
	tests := []struct {
		name     string
		ticks    [darwinCPUStateMax]uint32
		expected cpuCounters
	}{
		{
			name:  "normal CPU activity",
			ticks: [darwinCPUStateMax]uint32{100, 50, 800, 50},
			expected: cpuCounters{
				Busy: 200,
				Idle: 800,
			},
		},
		{
			name:  "user activity only",
			ticks: [darwinCPUStateMax]uint32{1000, 0, 0, 0},
			expected: cpuCounters{
				Busy: 1000,
			},
		},
		{
			name:  "system activity only",
			ticks: [darwinCPUStateMax]uint32{0, 1000, 0, 0},
			expected: cpuCounters{
				Busy: 1000,
			},
		},
		{
			name:  "idle activity only",
			ticks: [darwinCPUStateMax]uint32{0, 0, 1000, 0},
			expected: cpuCounters{
				Idle: 1000,
			},
		},
		{
			name:  "nice activity only",
			ticks: [darwinCPUStateMax]uint32{0, 0, 0, 1000},
			expected: cpuCounters{
				Busy: 1000,
			},
		},
		{
			name: "busy sum exceeds uint32",
			ticks: [darwinCPUStateMax]uint32{
				math.MaxUint32,
				1,
				100,
				0,
			},
			expected: cpuCounters{
				Busy: uint64(math.MaxUint32) + 1,
				Idle: 100,
			},
		},
		{
			name: "maximum native counters",
			ticks: [darwinCPUStateMax]uint32{
				math.MaxUint32,
				math.MaxUint32,
				math.MaxUint32,
				math.MaxUint32,
			},
			expected: cpuCounters{
				Busy: uint64(math.MaxUint32) * 3,
				Idle: uint64(math.MaxUint32),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counters, err := normalizeDarwinCPUCounters(
				tt.ticks,
			)
			if err != nil {
				t.Fatal(err)
			}

			if counters != tt.expected {
				t.Fatalf(
					"expected %+v, got %+v",
					tt.expected,
					counters,
				)
			}
		})
	}
}

func TestNormalizeDarwinCPUCountersZero(t *testing.T) {
	counters, err := normalizeDarwinCPUCounters(
		[darwinCPUStateMax]uint32{},
	)

	if !errors.Is(err, ErrInvalidCPUCounters) {
		t.Fatalf(
			"expected ErrInvalidCPUCounters, got %v",
			err,
		)
	}

	if counters != (cpuCounters{}) {
		t.Fatal("invalid input returned CPU counters")
	}
}

func TestDarwinCPUUtilizationFromNativeCounters(t *testing.T) {
	first, err := normalizeDarwinCPUCounters(
		[darwinCPUStateMax]uint32{
			100, 50, 800, 50,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	second, err := normalizeDarwinCPUCounters(
		[darwinCPUStateMax]uint32{
			110, 60, 875, 55,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	usage, err := calculateCPUUsage(
		first,
		second,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireMetricPercent(t, usage, 25)
}

func TestDarwinCPUCounterRollover(t *testing.T) {
	first, err := normalizeDarwinCPUCounters(
		[darwinCPUStateMax]uint32{
			math.MaxUint32,
			100,
			1000,
			0,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	second, err := normalizeDarwinCPUCounters(
		[darwinCPUStateMax]uint32{
			1,
			110,
			1100,
			0,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = calculateCPUUsage(first, second)

	if !errors.Is(err, ErrInvalidCPUCounters) {
		t.Fatalf(
			"expected ErrInvalidCPUCounters, got %v",
			err,
		)
	}
}
