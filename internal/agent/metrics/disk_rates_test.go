package metrics

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestCalculateDiskRate(t *testing.T) {
	previous := DiskUsage{
		Device:          "disk0",
		ReadBytes:       1000000,
		WriteBytes:      2000000,
		ReadOperations:  1000,
		WriteOperations: 2000,
	}

	current := DiskUsage{
		Device:          "disk0",
		ReadBytes:       5000000,
		WriteBytes:      4000000,
		ReadOperations:  1600,
		WriteOperations: 2240,
	}

	tests := []struct {
		name     string
		interval time.Duration

		readBytes       float64
		writeBytes      float64
		readOperations  float64
		writeOperations float64
	}{
		{
			name:            "one second",
			interval:        time.Second,
			readBytes:       4000000,
			writeBytes:      2000000,
			readOperations:  600,
			writeOperations: 240,
		},
		{
			name:            "two seconds",
			interval:        2 * time.Second,
			readBytes:       2000000,
			writeBytes:      1000000,
			readOperations:  300,
			writeOperations: 120,
		},
		{
			name:            "half second",
			interval:        500 * time.Millisecond,
			readBytes:       8000000,
			writeBytes:      4000000,
			readOperations:  1200,
			writeOperations: 480,
		},
		{
			name:            "fractional interval",
			interval:        1500 * time.Millisecond,
			readBytes:       4000000.0 / 1.5,
			writeBytes:      2000000.0 / 1.5,
			readOperations:  400,
			writeOperations: 160,
		},
		{
			name:            "one millisecond",
			interval:        time.Millisecond,
			readBytes:       4000000000,
			writeBytes:      2000000000,
			readOperations:  600000,
			writeOperations: 240000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate, err := calculateDiskRate(
				previous,
				current,
				tt.interval,
			)
			if err != nil {
				t.Fatal(err)
			}

			if rate.Device != "disk0" {
				t.Fatal("incorrect device name")
			}

			if rate.SampleIntervalMillis !=
				tt.interval.Milliseconds() {
				t.Fatal("incorrect sampling interval")
			}

			requireDiskRateValue(
				t,
				rate.ReadBytesPerSecond,
				tt.readBytes,
			)

			requireDiskRateValue(
				t,
				rate.WriteBytesPerSecond,
				tt.writeBytes,
			)

			requireDiskRateValue(
				t,
				rate.ReadOperationsPerSecond,
				tt.readOperations,
			)

			requireDiskRateValue(
				t,
				rate.WriteOperationsPerSecond,
				tt.writeOperations,
			)
		})
	}
}

func TestCalculateDiskRateIdle(t *testing.T) {
	disk := DiskUsage{
		Device:          "sda",
		ReadBytes:       1000000,
		WriteBytes:      2000000,
		ReadOperations:  100,
		WriteOperations: 200,
	}

	rate, err := calculateDiskRate(
		disk,
		disk,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := DiskRate{
		Device:               "sda",
		SampleIntervalMillis: 1000,
	}

	if rate != expected {
		t.Fatalf(
			"expected idle disk rate, got %+v",
			rate,
		)
	}
}

func TestCalculateDiskRateFractionalOperations(t *testing.T) {
	previous := DiskUsage{
		Device: "sda",
	}

	current := DiskUsage{
		Device:          "sda",
		ReadOperations:  1,
		WriteOperations: 3,
	}

	rate, err := calculateDiskRate(
		previous,
		current,
		4*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireDiskRateValue(
		t,
		rate.ReadOperationsPerSecond,
		0.25,
	)

	requireDiskRateValue(
		t,
		rate.WriteOperationsPerSecond,
		0.75,
	)
}

func TestCalculateDiskRateSubMillisecondPrecision(t *testing.T) {
	previous := DiskUsage{
		Device: "sda",
	}

	current := DiskUsage{
		Device:    "sda",
		ReadBytes: 1000,
	}

	interval := 1500 * time.Microsecond

	rate, err := calculateDiskRate(
		previous,
		current,
		interval,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Milliseconds are truncated for presentation,
	// but the rate uses the full elapsed duration.
	if rate.SampleIntervalMillis != 1 {
		t.Fatal("incorrect interval milliseconds")
	}

	requireDiskRateValue(
		t,
		rate.ReadBytesPerSecond,
		1000.0/0.0015,
	)
}

func TestCalculateDiskRateLargeCounters(t *testing.T) {
	previous := DiskUsage{
		Device:          "nvme0n1",
		ReadBytes:       math.MaxUint64 - 10,
		WriteBytes:      math.MaxUint64 - 20,
		ReadOperations:  math.MaxUint64 - 2,
		WriteOperations: math.MaxUint64 - 4,
	}

	current := DiskUsage{
		Device:          "nvme0n1",
		ReadBytes:       math.MaxUint64,
		WriteBytes:      math.MaxUint64,
		ReadOperations:  math.MaxUint64,
		WriteOperations: math.MaxUint64,
	}

	rate, err := calculateDiskRate(
		previous,
		current,
		2*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	requireDiskRateValue(
		t,
		rate.ReadBytesPerSecond,
		5,
	)

	requireDiskRateValue(
		t,
		rate.WriteBytesPerSecond,
		10,
	)

	requireDiskRateValue(
		t,
		rate.ReadOperationsPerSecond,
		1,
	)

	requireDiskRateValue(
		t,
		rate.WriteOperationsPerSecond,
		2,
	)
}

func TestCalculateDiskRateMaximumDelta(t *testing.T) {
	previous := DiskUsage{
		Device: "sda",
	}

	current := DiskUsage{
		Device:          "sda",
		ReadBytes:       math.MaxUint64,
		WriteBytes:      math.MaxUint64,
		ReadOperations:  math.MaxUint64,
		WriteOperations: math.MaxUint64,
	}

	rate, err := calculateDiskRate(
		previous,
		current,
		time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := float64(
		uint64(math.MaxUint64),
	) / 0.001

	requireDiskRateValue(
		t,
		rate.ReadBytesPerSecond,
		expected,
	)

	requireDiskRateValue(
		t,
		rate.WriteBytesPerSecond,
		expected,
	)

	requireDiskRateValue(
		t,
		rate.ReadOperationsPerSecond,
		expected,
	)

	requireDiskRateValue(
		t,
		rate.WriteOperationsPerSecond,
		expected,
	)
}

func TestCalculateDiskRateInvalidInterval(t *testing.T) {
	disk := DiskUsage{
		Device: "sda",
	}

	for _, interval := range []time.Duration{
		0,
		-time.Second,
		-time.Nanosecond,
		time.Nanosecond,
		999 * time.Microsecond,
	} {
		rate, err := calculateDiskRate(
			disk,
			disk,
			interval,
		)

		if !errors.Is(err, ErrInvalidDiskSample) {
			t.Fatalf(
				"interval %s: expected invalid sample, got %v",
				interval,
				err,
			)
		}

		if rate != (DiskRate{}) {
			t.Fatal("invalid interval returned rate")
		}
	}
}

func TestCalculateDiskRateInvalidDevice(t *testing.T) {
	tests := []struct {
		previous string
		current  string
	}{
		{"", ""},
		{"", "sda"},
		{"sda", ""},
		{"sda", "sdb"},
	}

	for _, tt := range tests {
		rate, err := calculateDiskRate(
			DiskUsage{Device: tt.previous},
			DiskUsage{Device: tt.current},
			time.Second,
		)

		if !errors.Is(err, ErrInvalidDiskSample) {
			t.Fatalf(
				"expected invalid device error, got %v",
				err,
			)
		}

		if rate != (DiskRate{}) {
			t.Fatal("invalid device returned rate")
		}
	}
}

func TestCalculateDiskRateCounterRegression(t *testing.T) {
	previous := DiskUsage{
		Device:          "sda",
		ReadBytes:       1000,
		WriteBytes:      2000,
		ReadOperations:  100,
		WriteOperations: 200,
	}

	tests := []struct {
		name   string
		change func(*DiskUsage)
	}{
		{
			name: "read bytes decreased",
			change: func(disk *DiskUsage) {
				disk.ReadBytes--
			},
		},
		{
			name: "write bytes decreased",
			change: func(disk *DiskUsage) {
				disk.WriteBytes--
			},
		},
		{
			name: "read operations decreased",
			change: func(disk *DiskUsage) {
				disk.ReadOperations--
			},
		},
		{
			name: "write operations decreased",
			change: func(disk *DiskUsage) {
				disk.WriteOperations--
			},
		},
		{
			name: "complete counter reset",
			change: func(disk *DiskUsage) {
				*disk = DiskUsage{
					Device: "sda",
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := previous

			tt.change(&current)

			rate, err := calculateDiskRate(
				previous,
				current,
				time.Second,
			)

			if !errors.Is(
				err,
				ErrDiskCounterRegression,
			) {
				t.Fatalf(
					"expected counter regression, got %v",
					err,
				)
			}

			if rate != (DiskRate{}) {
				t.Fatal("regressed counters returned rate")
			}
		})
	}
}

func TestCalculateDiskRates(t *testing.T) {
	previous := []DiskUsage{
		{
			Device:    "sdb",
			ReadBytes: 2000,
		},
		{
			Device:    "sda",
			ReadBytes: 1000,
		},
	}

	current := []DiskUsage{
		{
			Device:    "sdb",
			ReadBytes: 6000,
		},
		{
			Device:    "sda",
			ReadBytes: 3000,
		},
	}

	rates, err := calculateDiskRates(
		previous,
		current,
		2*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := []DiskRate{
		{
			Device:               "sda",
			ReadBytesPerSecond:   1000,
			SampleIntervalMillis: 2000,
		},
		{
			Device:               "sdb",
			ReadBytesPerSecond:   2000,
			SampleIntervalMillis: 2000,
		},
	}

	if !reflect.DeepEqual(rates, expected) {
		t.Fatalf(
			"expected %+v, got %+v",
			expected,
			rates,
		)
	}
}

func TestCalculateDiskRatesDeviceChanges(t *testing.T) {
	previous := []DiskUsage{
		{
			Device:    "sda",
			ReadBytes: 1000,
		},
		{
			Device:    "sdb",
			ReadBytes: 2000,
		},
		{
			Device:    "sdc",
			ReadBytes: 3000,
		},
	}

	current := []DiskUsage{
		{
			// Existing device.
			Device:    "sda",
			ReadBytes: 5000,
		},
		{
			// Newly connected device.
			Device:    "sdd",
			ReadBytes: 500,
		},
		{
			// Existing device with reset counters.
			Device:    "sdc",
			ReadBytes: 100,
		},
		// sdb has disconnected.
	}

	rates, err := calculateDiskRates(
		previous,
		current,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := []DiskRate{
		{
			Device:               "sda",
			ReadBytesPerSecond:   4000,
			SampleIntervalMillis: 1000,
		},
	}

	if !reflect.DeepEqual(rates, expected) {
		t.Fatalf(
			"unexpected disk rates: %+v",
			rates,
		)
	}
}

func TestCalculateDiskRatesReestablishesBaseline(t *testing.T) {
	initial := []DiskUsage{
		{
			Device:    "sda",
			ReadBytes: 10000,
		},
	}

	reset := []DiskUsage{
		{
			Device:    "sda",
			ReadBytes: 100,
		},
	}

	next := []DiskUsage{
		{
			Device:    "sda",
			ReadBytes: 1100,
		},
	}

	rates, err := calculateDiskRates(
		initial,
		reset,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(rates) != 0 {
		t.Fatal("reset device returned rate")
	}

	rates, err = calculateDiskRates(
		reset,
		next,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(rates) != 1 {
		t.Fatal("device did not reestablish baseline")
	}

	requireDiskRateValue(
		t,
		rates[0].ReadBytesPerSecond,
		1000,
	)
}

func TestCalculateDiskRatesEmpty(t *testing.T) {
	tests := []struct {
		previous []DiskUsage
		current  []DiskUsage
	}{
		{},
		{
			current: []DiskUsage{
				{Device: "sda"},
			},
		},
		{
			previous: []DiskUsage{
				{Device: "sda"},
			},
		},
		{
			previous: []DiskUsage{
				{Device: "sda"},
			},
			current: []DiskUsage{
				{Device: "sdb"},
			},
		},
	}

	for _, tt := range tests {
		rates, err := calculateDiskRates(
			tt.previous,
			tt.current,
			time.Second,
		)
		if err != nil {
			t.Fatal(err)
		}

		if rates == nil || len(rates) != 0 {
			t.Fatal("expected initialized empty rate slice")
		}
	}
}

func TestCalculateDiskRatesInvalidSamples(t *testing.T) {
	tests := []struct {
		name     string
		previous []DiskUsage
		current  []DiskUsage
	}{
		{
			name: "duplicate previous devices",
			previous: []DiskUsage{
				{Device: "sda"},
				{Device: "sda"},
			},
		},
		{
			name: "duplicate current devices",
			current: []DiskUsage{
				{Device: "sda"},
				{Device: "sda"},
			},
		},
		{
			name: "empty previous device",
			previous: []DiskUsage{
				{},
			},
		},
		{
			name: "empty current device",
			current: []DiskUsage{
				{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rates, err := calculateDiskRates(
				tt.previous,
				tt.current,
				time.Second,
			)

			if !errors.Is(err, ErrInvalidDiskSample) {
				t.Fatalf(
					"expected invalid sample, got %v",
					err,
				)
			}

			if rates != nil {
				t.Fatal("invalid sample returned rates")
			}
		})
	}
}

func TestCalculateDiskRatesInvalidInterval(t *testing.T) {
	rates, err := calculateDiskRates(
		nil,
		nil,
		0,
	)

	if !errors.Is(err, ErrInvalidDiskSample) {
		t.Fatalf("expected invalid interval, got %v", err)
	}

	if rates != nil {
		t.Fatal("invalid interval returned rates")
	}
}

func TestDiskRateJSON(t *testing.T) {
	rate := DiskRate{
		Device:                   "disk0",
		ReadBytesPerSecond:       4096000,
		WriteBytesPerSecond:      2048000,
		ReadOperationsPerSecond:  100.5,
		WriteOperationsPerSecond: 50.25,
		SampleIntervalMillis:     1000,
	}

	data, err := json.Marshal(rate)
	if err != nil {
		t.Fatal(err)
	}

	const expected = `{"device":"disk0",` +
		`"read_bytes_per_second":4096000,` +
		`"write_bytes_per_second":2048000,` +
		`"read_operations_per_second":100.5,` +
		`"write_operations_per_second":50.25,` +
		`"sample_interval_millis":1000}`

	if string(data) != expected {
		t.Fatalf("unexpected JSON: %s", data)
	}

	var decoded DiskRate

	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded != rate {
		t.Fatal("disk rate JSON round trip failed")
	}
}

// requireDiskRateValue validates a finite,
// nonnegative floating-point rate measurement.
func requireDiskRateValue(
	t *testing.T,
	actual float64,
	expected float64,
) {
	t.Helper()

	if math.IsNaN(actual) ||
		math.IsInf(actual, 0) {
		t.Fatalf("invalid rate: %v", actual)
	}

	if actual < 0 {
		t.Fatalf("negative rate: %v", actual)
	}

	tolerance := 1e-9 * math.Max(
		1,
		math.Abs(expected),
	)

	if math.Abs(actual-expected) > tolerance {
		t.Fatalf(
			"expected %v, got %v",
			expected,
			actual,
		)
	}
}
