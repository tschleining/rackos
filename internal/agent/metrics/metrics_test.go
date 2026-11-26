package metrics

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestCalculateCPUUsage(t *testing.T) {
	tests := []struct {
		name     string
		previous cpuCounters
		current  cpuCounters
		expected float64
	}{
		{
			name:     "fully idle",
			previous: cpuCounters{Busy: 100, Idle: 100},
			current:  cpuCounters{Busy: 100, Idle: 200},
			expected: 0,
		},
		{
			name:     "fully utilized",
			previous: cpuCounters{Busy: 100, Idle: 100},
			current:  cpuCounters{Busy: 200, Idle: 100},
			expected: 100,
		},
		{
			name:     "half utilized",
			previous: cpuCounters{Busy: 100, Idle: 100},
			current:  cpuCounters{Busy: 150, Idle: 150},
			expected: 50,
		},
		{
			name:     "quarter utilized",
			previous: cpuCounters{Busy: 100, Idle: 100},
			current:  cpuCounters{Busy: 125, Idle: 175},
			expected: 25,
		},
		{
			name:     "fractional utilization",
			previous: cpuCounters{},
			current:  cpuCounters{Busy: 1, Idle: 2},
			expected: 100.0 / 3.0,
		},
		{
			name: "large cumulative counters",
			previous: cpuCounters{
				Busy: 1000000000000,
				Idle: 2000000000000,
			},
			current: cpuCounters{
				Busy: 1000000000200,
				Idle: 2000000000800,
			},
			expected: 20,
		},
		{
			name:     "combined counter overflow",
			previous: cpuCounters{},
			current: cpuCounters{
				Busy: math.MaxUint64,
				Idle: math.MaxUint64,
			},
			expected: 50,
		},
		{
			name: "small delta near integer limit",
			previous: cpuCounters{
				Busy: math.MaxUint64 - 25,
				Idle: math.MaxUint64 - 75,
			},
			current: cpuCounters{
				Busy: math.MaxUint64,
				Idle: math.MaxUint64,
			},
			expected: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := calculateCPUUsage(
				tt.previous,
				tt.current,
			)
			if err != nil {
				t.Fatal(err)
			}

			requireMetricPercent(
				t,
				actual,
				tt.expected,
			)
		})
	}
}

func TestCalculateCPUUsageInvalidCounters(t *testing.T) {
	tests := []struct {
		name     string
		previous cpuCounters
		current  cpuCounters
	}{
		{
			name: "empty counters",
		},
		{
			name:     "unchanged counters",
			previous: cpuCounters{Busy: 100, Idle: 200},
			current:  cpuCounters{Busy: 100, Idle: 200},
		},
		{
			name:     "busy counter regression",
			previous: cpuCounters{Busy: 100, Idle: 200},
			current:  cpuCounters{Busy: 99, Idle: 300},
		},
		{
			name:     "idle counter regression",
			previous: cpuCounters{Busy: 100, Idle: 200},
			current:  cpuCounters{Busy: 200, Idle: 199},
		},
		{
			name:     "complete counter reset",
			previous: cpuCounters{Busy: 100, Idle: 200},
			current:  cpuCounters{},
		},
		{
			name: "counter rollover",
			previous: cpuCounters{
				Busy: math.MaxUint64,
				Idle: math.MaxUint64,
			},
			current: cpuCounters{
				Busy: 1,
				Idle: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := calculateCPUUsage(
				tt.previous,
				tt.current,
			)

			if !errors.Is(err, ErrInvalidCPUCounters) {
				t.Fatalf(
					"expected ErrInvalidCPUCounters, got %v",
					err,
				)
			}

			if actual != 0 {
				t.Fatalf(
					"unexpected utilization: %v",
					actual,
				)
			}
		})
	}
}

func TestCalculateCPUUsageUsesDeltas(t *testing.T) {
	previous := cpuCounters{
		Busy: 9000,
		Idle: 1000,
	}

	current := cpuCounters{
		Busy: 9010,
		Idle: 1090,
	}

	actual, err := calculateCPUUsage(
		previous,
		current,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Historical utilization is approximately 90%,
	// but utilization during this interval is 10%.
	requireMetricPercent(t, actual, 10)
}

func TestCalculateMemoryUsage(t *testing.T) {
	const gib = uint64(1024 * 1024 * 1024)

	tests := []struct {
		name      string
		total     uint64
		available uint64
		used      uint64
		expected  float64
	}{
		{
			name:      "all memory available",
			total:     24 * gib,
			available: 24 * gib,
			used:      0,
			expected:  0,
		},
		{
			name:      "no memory available",
			total:     24 * gib,
			available: 0,
			used:      24 * gib,
			expected:  100,
		},
		{
			name:      "half utilized",
			total:     24 * gib,
			available: 12 * gib,
			used:      12 * gib,
			expected:  50,
		},
		{
			name:      "normal workstation",
			total:     24 * gib,
			available: 9 * gib,
			used:      15 * gib,
			expected:  62.5,
		},
		{
			name:      "large server",
			total:     1024 * gib,
			available: 256 * gib,
			used:      768 * gib,
			expected:  75,
		},
		{
			name:      "fractional utilization",
			total:     3,
			available: 2,
			used:      1,
			expected:  100.0 / 3.0,
		},
		{
			name:      "maximum integer capacity",
			total:     math.MaxUint64,
			available: 0,
			used:      math.MaxUint64,
			expected:  100,
		},
		{
			name:      "maximum available memory",
			total:     math.MaxUint64,
			available: math.MaxUint64,
			used:      0,
			expected:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := calculateMemoryUsage(
				tt.total,
				tt.available,
			)
			if err != nil {
				t.Fatal(err)
			}

			if actual.TotalBytes != tt.total {
				t.Fatal("incorrect total memory")
			}

			if actual.AvailableBytes != tt.available {
				t.Fatal("incorrect available memory")
			}

			if actual.UsedBytes != tt.used {
				t.Fatalf(
					"expected %d used bytes, got %d",
					tt.used,
					actual.UsedBytes,
				)
			}

			if actual.UsedBytes+
				actual.AvailableBytes != actual.TotalBytes {
				t.Fatal("memory accounting is inconsistent")
			}

			requireMetricPercent(
				t,
				actual.UsagePercent,
				tt.expected,
			)
		})
	}
}

func TestCalculateMemoryUsageInvalidValues(t *testing.T) {
	tests := []struct {
		name      string
		total     uint64
		available uint64
	}{
		{
			name: "zero total memory",
		},
		{
			name:      "available memory without capacity",
			total:     0,
			available: 100,
		},
		{
			name:      "available exceeds total",
			total:     100,
			available: 101,
		},
		{
			name:      "large invalid available value",
			total:     1024,
			available: math.MaxUint64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := calculateMemoryUsage(
				tt.total,
				tt.available,
			)

			if !errors.Is(err, ErrInvalidMemory) {
				t.Fatalf(
					"expected ErrInvalidMemory, got %v",
					err,
				)
			}

			if actual != (MemoryUsage{}) {
				t.Fatal("invalid memory returned a measurement")
			}
		})
	}
}

func TestMetricsSnapshotJSON(t *testing.T) {
	snapshot := Snapshot{
		CPU: CPUUsage{
			UsagePercent:         23.8,
			SampleIntervalMillis: 1000,
		},
		Memory: MemoryUsage{
			TotalBytes:     25769803776,
			AvailableBytes: 9663676416,
			UsedBytes:      16106127360,
			UsagePercent:   62.5,
		},
		CollectedAt: time.Date(
			2026, time.November, 26,
			22, 0, 0, 0,
			time.UTC,
		),
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	const expected = `{
		"cpu": {
			"usage_percent": 23.8,
			"sample_interval_millis": 1000
		},
		"memory": {
			"total_bytes": 25769803776,
			"available_bytes": 9663676416,
			"used_bytes": 16106127360,
			"usage_percent": 62.5
		},
		"collected_at": "2026-11-26T22:00:00Z"
	}`

	var actualJSON map[string]any
	var expectedJSON map[string]any

	if err := json.Unmarshal(data, &actualJSON); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(
		[]byte(expected),
		&expectedJSON,
	); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(actualJSON, expectedJSON) {
		t.Fatalf(
			"unexpected metrics JSON: %s",
			data,
		)
	}

	var decoded Snapshot

	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(snapshot, decoded) {
		t.Fatal("metrics JSON round trip failed")
	}
}

func TestDefaultCPUSampleInterval(t *testing.T) {
	if DefaultCPUSampleInterval != time.Second {
		t.Fatal("unexpected default CPU sample interval")
	}
}

func requireMetricPercent(
	t *testing.T,
	actual float64,
	expected float64,
) {
	t.Helper()

	if math.IsNaN(actual) || math.IsInf(actual, 0) {
		t.Fatalf(
			"non-finite utilization: %v",
			actual,
		)
	}

	if actual < 0 || actual > 100 {
		t.Fatalf(
			"utilization outside valid range: %v",
			actual,
		)
	}

	const tolerance = 0.000000001

	if math.Abs(actual-expected) > tolerance {
		t.Fatalf(
			"expected %.10f, got %.10f",
			expected,
			actual,
		)
	}
}
