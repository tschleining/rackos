package metrics

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestParseLinuxCPUCounters(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected cpuCounters
	}{
		{
			name:  "complete CPU statistics",
			input: "cpu 3200 100 1500 12000 400 50 150 100 200 20",
			expected: cpuCounters{
				Busy: 5100,
				Idle: 12400,
			},
		},
		{
			name:  "required counters only",
			input: "cpu 100 20 30 850",
			expected: cpuCounters{
				Busy: 150,
				Idle: 850,
			},
		},
		{
			name:  "optional counters partially present",
			input: "cpu 100 20 30 800 50",
			expected: cpuCounters{
				Busy: 150,
				Idle: 850,
			},
		},
		{
			name:  "guest counters are not double counted",
			input: "cpu 100 50 25 500 0 0 0 0 80 40",
			expected: cpuCounters{
				Busy: 175,
				Idle: 500,
			},
		},
		{
			name:  "steal time is busy",
			input: "cpu 0 0 0 900 0 0 0 100",
			expected: cpuCounters{
				Busy: 100,
				Idle: 900,
			},
		},
		{
			name:  "IO wait is idle",
			input: "cpu 100 0 0 700 200",
			expected: cpuCounters{
				Busy: 100,
				Idle: 900,
			},
		},
		{
			name:  "interrupt activity",
			input: "cpu 100 0 0 800 0 40 60",
			expected: cpuCounters{
				Busy: 200,
				Idle: 800,
			},
		},
		{
			name:  "whitespace normalization",
			input: "  cpu\t100   20\t30  850  ",
			expected: cpuCounters{
				Busy: 150,
				Idle: 850,
			},
		},
		{
			name:  "future trailing counters",
			input: "cpu 100 20 30 850 0 0 0 0 0 0 123 456",
			expected: cpuCounters{
				Busy: 150,
				Idle: 850,
			},
		},
		{
			name:  "idle activity only",
			input: "cpu 0 0 0 1000",
			expected: cpuCounters{
				Idle: 1000,
			},
		},
		{
			name:  "busy activity only",
			input: "cpu 1000 0 0 0",
			expected: cpuCounters{
				Busy: 1000,
			},
		},
		{
			name:  "maximum valid busy counter",
			input: "cpu 18446744073709551615 0 0 1",
			expected: cpuCounters{
				Busy: math.MaxUint64,
				Idle: 1,
			},
		},
		{
			name:  "maximum valid idle counter",
			input: "cpu 1 0 0 18446744073709551615",
			expected: cpuCounters{
				Busy: 1,
				Idle: math.MaxUint64,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counters, err := parseLinuxCPUCounters(
				tt.input,
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

func TestParseLinuxCPUCountersInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "empty input",
		},
		{
			name:  "whitespace only",
			input: "   ",
		},
		{
			name:  "missing CPU counters",
			input: "cpu",
		},
		{
			name:  "missing required counter",
			input: "cpu 100 20 30",
		},
		{
			name:  "individual CPU line",
			input: "cpu0 100 20 30 850",
		},
		{
			name:  "unrelated statistics",
			input: "intr 100 200 300 400",
		},
		{
			name:  "negative counter",
			input: "cpu -100 20 30 850",
		},
		{
			name:  "non-numeric counter",
			input: "cpu abc 20 30 850",
		},
		{
			name:  "fractional counter",
			input: "cpu 100.5 20 30 850",
		},
		{
			name:  "invalid optional counter",
			input: "cpu 100 20 30 850 invalid",
		},
		{
			name:  "invalid guest counter",
			input: "cpu 100 20 30 850 0 0 0 0 invalid",
		},
		{
			name:  "all counters zero",
			input: "cpu 0 0 0 0 0 0 0 0 0 0",
		},
		{
			name:  "individual counter overflow",
			input: "cpu 18446744073709551616 0 0 1",
		},
		{
			name:  "busy counter addition overflow",
			input: "cpu 18446744073709551615 1 0 1",
		},
		{
			name:  "idle counter addition overflow",
			input: "cpu 1 0 0 18446744073709551615 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counters, err := parseLinuxCPUCounters(
				tt.input,
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
		})
	}
}

func TestLinuxCPUUtilizationFromParsedCounters(t *testing.T) {
	first, err := parseLinuxCPUCounters(
		"cpu 100 0 50 800 50 0 0 0 0 0",
	)
	if err != nil {
		t.Fatal(err)
	}

	second, err := parseLinuxCPUCounters(
		"cpu 115 0 60 865 60 0 0 0 0 0",
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

func TestLinuxReadCPUCountersCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	counters, err := readCPUCounters(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if counters != (cpuCounters{}) {
		t.Fatal("canceled collection returned CPU counters")
	}
}
