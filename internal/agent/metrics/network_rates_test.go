package metrics

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestCalculateNetworkInterfaceRate(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		expected NetworkInterfaceRate
	}{
		{
			name:     "one second",
			interval: time.Second,
			expected: NetworkInterfaceRate{
				ReceiveBytesPerSecond:    6000,
				TransmitBytesPerSecond:   3000,
				ReceivePacketsPerSecond:  60,
				TransmitPacketsPerSecond: 30,
				SampleIntervalMillis:     1000,
			},
		},
		{
			name:     "two seconds",
			interval: 2 * time.Second,
			expected: NetworkInterfaceRate{
				ReceiveBytesPerSecond:    3000,
				TransmitBytesPerSecond:   1500,
				ReceivePacketsPerSecond:  30,
				TransmitPacketsPerSecond: 15,
				SampleIntervalMillis:     2000,
			},
		},
		{
			name:     "half second",
			interval: 500 * time.Millisecond,
			expected: NetworkInterfaceRate{
				ReceiveBytesPerSecond:    12000,
				TransmitBytesPerSecond:   6000,
				ReceivePacketsPerSecond:  120,
				TransmitPacketsPerSecond: 60,
				SampleIntervalMillis:     500,
			},
		},
		{
			name:     "fractional seconds",
			interval: 1500 * time.Millisecond,
			expected: NetworkInterfaceRate{
				ReceiveBytesPerSecond:    4000,
				TransmitBytesPerSecond:   2000,
				ReceivePacketsPerSecond:  40,
				TransmitPacketsPerSecond: 20,
				SampleIntervalMillis:     1500,
			},
		},
		{
			name:     "fractional packet rates",
			interval: 4 * time.Second,
			expected: NetworkInterfaceRate{
				ReceiveBytesPerSecond:    1500,
				TransmitBytesPerSecond:   750,
				ReceivePacketsPerSecond:  15,
				TransmitPacketsPerSecond: 7.5,
				SampleIntervalMillis:     4000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous, current := testNetworkRateSamples()

			actual, err := calculateNetworkInterfaceRate(
				previous,
				current,
				tt.interval,
			)
			if err != nil {
				t.Fatal(err)
			}

			expected := tt.expected
			expected.Name = "eth0"
			expected.Index = 2

			requireNetworkRate(t, actual, expected)
		})
	}
}

func TestCalculateNetworkInterfaceRateFullDuration(t *testing.T) {
	previous, current := testNetworkRateSamples()

	interval := time.Second +
		500*time.Microsecond

	rate, err := calculateNetworkInterfaceRate(
		previous,
		current,
		interval,
	)
	if err != nil {
		t.Fatal(err)
	}

	if rate.SampleIntervalMillis != 1000 {
		t.Fatal("incorrect reported interval")
	}

	requireNetworkRateValue(
		t,
		rate.ReceiveBytesPerSecond,
		6000/interval.Seconds(),
	)

	requireNetworkRateValue(
		t,
		rate.TransmitBytesPerSecond,
		3000/interval.Seconds(),
	)

	if rate.ReceiveBytesPerSecond == 6000 {
		t.Fatal("calculation used truncated milliseconds")
	}
}

func TestCalculateNetworkInterfaceRateZero(t *testing.T) {
	previous, _ := testNetworkRateSamples()

	rate, err := calculateNetworkInterfaceRate(
		previous,
		previous,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := NetworkInterfaceRate{
		Name:                 previous.Name,
		Index:                previous.Index,
		SampleIntervalMillis: 1000,
	}

	requireNetworkRate(t, rate, expected)
}

func TestCalculateNetworkInterfaceRateLargeCounters(t *testing.T) {
	previous := NetworkInterfaceUsage{
		Name:  "eth0",
		Index: 2,

		ReceivedBytes:      math.MaxUint64 - 100,
		TransmittedBytes:   math.MaxUint64 - 50,
		ReceivedPackets:    math.MaxUint64 - 10,
		TransmittedPackets: math.MaxUint64 - 5,
	}

	current := NetworkInterfaceUsage{
		Name:  "eth0",
		Index: 2,

		ReceivedBytes:      math.MaxUint64,
		TransmittedBytes:   math.MaxUint64,
		ReceivedPackets:    math.MaxUint64,
		TransmittedPackets: math.MaxUint64,
	}

	rate, err := calculateNetworkInterfaceRate(
		previous,
		current,
		2*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := NetworkInterfaceRate{
		Name:                     "eth0",
		Index:                    2,
		ReceiveBytesPerSecond:    50,
		TransmitBytesPerSecond:   25,
		ReceivePacketsPerSecond:  5,
		TransmitPacketsPerSecond: 2.5,
		SampleIntervalMillis:     2000,
	}

	requireNetworkRate(t, rate, expected)
}

func TestCalculateNetworkInterfaceRateMaximumDelta(t *testing.T) {
	previous := NetworkInterfaceUsage{
		Name:  "eth0",
		Index: 2,
	}

	current := previous

	current.ReceivedBytes = math.MaxUint64
	current.TransmittedBytes = math.MaxUint64
	current.ReceivedPackets = math.MaxUint64
	current.TransmittedPackets = math.MaxUint64

	rate, err := calculateNetworkInterfaceRate(
		previous,
		current,
		time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}

	value := float64(uint64(math.MaxUint64)) /
		time.Millisecond.Seconds()

	expected := NetworkInterfaceRate{
		Name:                     "eth0",
		Index:                    2,
		ReceiveBytesPerSecond:    value,
		TransmitBytesPerSecond:   value,
		ReceivePacketsPerSecond:  value,
		TransmitPacketsPerSecond: value,
		SampleIntervalMillis:     1,
	}

	requireNetworkRate(t, rate, expected)
}

func TestCalculateNetworkInterfaceRateInvalidInterval(t *testing.T) {
	previous, current := testNetworkRateSamples()

	for _, interval := range []time.Duration{
		-time.Second,
		-time.Nanosecond,
		0,
		time.Nanosecond,
		999 * time.Microsecond,
	} {
		rate, err := calculateNetworkInterfaceRate(
			previous,
			current,
			interval,
		)

		if !errors.Is(err, ErrInvalidNetworkSample) {
			t.Fatalf(
				"interval %s: expected invalid sample, got %v",
				interval,
				err,
			)
		}

		if rate != (NetworkInterfaceRate{}) {
			t.Fatal("invalid interval returned rate")
		}
	}
}

func TestCalculateNetworkInterfaceRateInvalidIdentity(t *testing.T) {
	tests := []struct {
		name   string
		change func(
			*NetworkInterfaceUsage,
			*NetworkInterfaceUsage,
		)
	}{
		{
			name: "renamed interface",
			change: func(p, c *NetworkInterfaceUsage) {
				c.Name = "eth1"
			},
		},
		{
			name: "changed interface index",
			change: func(p, c *NetworkInterfaceUsage) {
				c.Index = 3
			},
		},
		{
			name: "empty previous name",
			change: func(p, c *NetworkInterfaceUsage) {
				p.Name = ""
			},
		},
		{
			name: "empty current name",
			change: func(p, c *NetworkInterfaceUsage) {
				c.Name = ""
			},
		},
		{
			name: "invalid previous index",
			change: func(p, c *NetworkInterfaceUsage) {
				p.Index = 0
			},
		},
		{
			name: "invalid current index",
			change: func(p, c *NetworkInterfaceUsage) {
				c.Index = 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous, current := testNetworkRateSamples()

			tt.change(&previous, &current)

			rate, err := calculateNetworkInterfaceRate(
				previous,
				current,
				time.Second,
			)

			if !errors.Is(err, ErrInvalidNetworkSample) {
				t.Fatalf(
					"expected invalid sample, got %v",
					err,
				)
			}

			if rate != (NetworkInterfaceRate{}) {
				t.Fatal("invalid identity returned rate")
			}
		})
	}
}

func TestCalculateNetworkInterfaceRateRegression(t *testing.T) {
	tests := []struct {
		name   string
		change func(*NetworkInterfaceUsage)
	}{
		{
			name: "receive bytes decreased",
			change: func(c *NetworkInterfaceUsage) {
				c.ReceivedBytes = 0
			},
		},
		{
			name: "transmit bytes decreased",
			change: func(c *NetworkInterfaceUsage) {
				c.TransmittedBytes = 0
			},
		},
		{
			name: "receive packets decreased",
			change: func(c *NetworkInterfaceUsage) {
				c.ReceivedPackets = 0
			},
		},
		{
			name: "transmit packets decreased",
			change: func(c *NetworkInterfaceUsage) {
				c.TransmittedPackets = 0
			},
		},
		{
			name: "receive errors decreased",
			change: func(c *NetworkInterfaceUsage) {
				c.ReceiveErrors = 0
			},
		},
		{
			name: "transmit errors decreased",
			change: func(c *NetworkInterfaceUsage) {
				c.TransmitErrors = 0
			},
		},
		{
			name: "receive drops decreased",
			change: func(c *NetworkInterfaceUsage) {
				c.ReceiveDropped = networkUint64Ptr(0)
			},
		},
		{
			name: "transmit drops decreased",
			change: func(c *NetworkInterfaceUsage) {
				c.TransmitDropped = networkUint64Ptr(0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous, current := testNetworkRateSamples()

			tt.change(&current)

			rate, err := calculateNetworkInterfaceRate(
				previous,
				current,
				time.Second,
			)

			if !errors.Is(err, ErrNetworkCounterRegression) {
				t.Fatalf(
					"expected counter regression, got %v",
					err,
				)
			}

			if rate != (NetworkInterfaceRate{}) {
				t.Fatal("regressed counters returned rate")
			}
		})
	}
}

func TestOptionalNetworkCounterRegressed(t *testing.T) {
	tests := []struct {
		name     string
		previous *uint64
		current  *uint64
		expected bool
	}{
		{
			name: "both unavailable",
		},
		{
			name:    "became available",
			current: networkUint64Ptr(0),
		},
		{
			name:     "became unavailable",
			previous: networkUint64Ptr(100),
		},
		{
			name:     "unchanged",
			previous: networkUint64Ptr(10),
			current:  networkUint64Ptr(10),
		},
		{
			name:     "increased",
			previous: networkUint64Ptr(10),
			current:  networkUint64Ptr(20),
		},
		{
			name:     "decreased",
			previous: networkUint64Ptr(20),
			current:  networkUint64Ptr(10),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := optionalNetworkCounterRegressed(
				tt.previous,
				tt.current,
			)

			if actual != tt.expected {
				t.Fatalf(
					"expected %t, got %t",
					tt.expected,
					actual,
				)
			}
		})
	}
}

func TestCalculateNetworkInterfaceRateOptionalCounters(t *testing.T) {
	previous, current := testNetworkRateSamples()

	previous.ReceiveDropped = nil
	current.TransmitDropped = nil

	rate, err := calculateNetworkInterfaceRate(
		previous,
		current,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	if rate.ReceiveBytesPerSecond != 6000 ||
		rate.TransmitBytesPerSecond != 3000 {
		t.Fatal("optional counters affected throughput")
	}
}

func TestCalculateNetworkInterfaceRateMetadataChanges(t *testing.T) {
	previous, current := testNetworkRateSamples()

	current.Up = false
	current.MTU = 1280
	current.LinkSpeedBitsPerSecond = nil

	rate, err := calculateNetworkInterfaceRate(
		previous,
		current,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	if rate.ReceiveBytesPerSecond != 6000 {
		t.Fatal("metadata changes affected throughput")
	}
}

func TestCalculateNetworkRates(t *testing.T) {
	previous, current := testNetworkRateSamples()

	previousLoopback := NetworkInterfaceUsage{
		Name:     "lo",
		Index:    1,
		Up:       true,
		Loopback: true,
	}

	currentLoopback := previousLoopback
	currentLoopback.ReceivedBytes = 1000
	currentLoopback.TransmittedBytes = 1000

	rates, err := calculateNetworkRates(
		[]NetworkInterfaceUsage{
			previousLoopback,
			previous,
		},
		[]NetworkInterfaceUsage{
			currentLoopback,
			current,
		},
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := []NetworkInterfaceRate{
		{
			Name:                     "eth0",
			Index:                    2,
			ReceiveBytesPerSecond:    6000,
			TransmitBytesPerSecond:   3000,
			ReceivePacketsPerSecond:  60,
			TransmitPacketsPerSecond: 30,
			SampleIntervalMillis:     1000,
		},
		{
			Name:                   "lo",
			Index:                  1,
			ReceiveBytesPerSecond:  1000,
			TransmitBytesPerSecond: 1000,
			SampleIntervalMillis:   1000,
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

func TestCalculateNetworkRatesInterfaceChanges(t *testing.T) {
	previous := []NetworkInterfaceUsage{
		{Name: "eth0", Index: 2, ReceivedBytes: 1000},
		{Name: "eth1", Index: 3, ReceivedBytes: 1000},
		{Name: "eth2", Index: 4, ReceivedBytes: 1000},
		{Name: "eth3", Index: 5, ReceivedBytes: 1000},
		{Name: "eth4", Index: 6, ReceivedBytes: 1000},
		{Name: "eth5", Index: 7, ReceivedBytes: 1000},
	}

	current := []NetworkInterfaceUsage{
		// Valid existing interface.
		{Name: "eth0", Index: 2, ReceivedBytes: 2000},

		// Same name, different index.
		{Name: "eth1", Index: 10, ReceivedBytes: 2000},

		// eth2 removed.

		// eth3 renamed.
		{Name: "renamed0", Index: 5, ReceivedBytes: 2000},

		// eth4 counters reset.
		{Name: "eth4", Index: 6, ReceivedBytes: 10},

		// Unchanged valid interface.
		{Name: "eth5", Index: 7, ReceivedBytes: 1000},

		// Newly discovered interface.
		{Name: "eth6", Index: 8, ReceivedBytes: 5000},
	}

	rates, err := calculateNetworkRates(
		previous,
		current,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := []NetworkInterfaceRate{
		{
			Name:                  "eth0",
			Index:                 2,
			ReceiveBytesPerSecond: 1000,
			SampleIntervalMillis:  1000,
		},
		{
			Name:                 "eth5",
			Index:                7,
			SampleIntervalMillis: 1000,
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

func TestCalculateNetworkRatesResetRecovery(t *testing.T) {
	previous := []NetworkInterfaceUsage{
		{Name: "eth0", Index: 2, ReceivedBytes: 10000},
	}

	reset := []NetworkInterfaceUsage{
		{Name: "eth0", Index: 2, ReceivedBytes: 100},
	}

	next := []NetworkInterfaceUsage{
		{Name: "eth0", Index: 2, ReceivedBytes: 600},
	}

	rates, err := calculateNetworkRates(
		previous,
		reset,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	if rates == nil || len(rates) != 0 {
		t.Fatal("reset interface should not produce rates")
	}

	rates, err = calculateNetworkRates(
		reset,
		next,
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(rates) != 1 ||
		rates[0].ReceiveBytesPerSecond != 500 {
		t.Fatal("interface did not recover after reset")
	}
}

func TestCalculateNetworkRatesInvalidCollections(t *testing.T) {
	valid := []NetworkInterfaceUsage{
		{Name: "eth0", Index: 2},
	}

	invalidCollections := [][]NetworkInterfaceUsage{
		{
			{Name: "eth0", Index: 2},
			{Name: "eth0", Index: 3},
		},
		{
			{Name: "eth0", Index: 2},
			{Name: "eth1", Index: 2},
		},
		{
			{Name: "", Index: 2},
		},
		{
			{Name: "eth0", Index: 0},
		},
	}

	for _, invalid := range invalidCollections {
		for _, samples := range []struct {
			previous []NetworkInterfaceUsage
			current  []NetworkInterfaceUsage
		}{
			{previous: invalid, current: valid},
			{previous: valid, current: invalid},
		} {
			rates, err := calculateNetworkRates(
				samples.previous,
				samples.current,
				time.Second,
			)

			if !errors.Is(err, ErrInvalidNetworkSample) {
				t.Fatalf(
					"expected invalid sample, got %v",
					err,
				)
			}

			if !errors.Is(err, ErrInvalidNetworkStatistics) {
				t.Fatal("underlying validation error was lost")
			}

			if rates != nil {
				t.Fatal("invalid sample returned rates")
			}
		}
	}
}

func TestCalculateNetworkRatesEmpty(t *testing.T) {
	ifaces := []NetworkInterfaceUsage{
		{Name: "eth0", Index: 2},
	}

	tests := []struct {
		previous []NetworkInterfaceUsage
		current  []NetworkInterfaceUsage
	}{
		{},
		{previous: ifaces},
		{current: ifaces},
		{
			previous: []NetworkInterfaceUsage{},
			current:  []NetworkInterfaceUsage{},
		},
	}

	for _, tt := range tests {
		rates, err := calculateNetworkRates(
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

func TestCalculateNetworkRatesInvalidInterval(t *testing.T) {
	for _, interval := range []time.Duration{
		0,
		-time.Second,
		999 * time.Microsecond,
	} {
		rates, err := calculateNetworkRates(
			nil,
			nil,
			interval,
		)

		if !errors.Is(err, ErrInvalidNetworkSample) {
			t.Fatalf("expected invalid sample, got %v", err)
		}

		if rates != nil {
			t.Fatal("invalid interval returned rates")
		}
	}
}

func TestNetworkInterfaceRateJSON(t *testing.T) {
	rate := NetworkInterfaceRate{
		Name:                     "eth0",
		Index:                    2,
		ReceiveBytesPerSecond:    6000,
		TransmitBytesPerSecond:   3000,
		ReceivePacketsPerSecond:  60,
		TransmitPacketsPerSecond: 30,
		SampleIntervalMillis:     1000,
	}

	data, err := json.Marshal(rate)
	if err != nil {
		t.Fatal(err)
	}

	const expected = `{"name":"eth0",` +
		`"index":2,` +
		`"receive_bytes_per_second":6000,` +
		`"transmit_bytes_per_second":3000,` +
		`"receive_packets_per_second":60,` +
		`"transmit_packets_per_second":30,` +
		`"sample_interval_millis":1000}`

	if string(data) != expected {
		t.Fatalf("unexpected rate JSON: %s", data)
	}

	var decoded NetworkInterfaceRate

	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded != rate {
		t.Fatal("network rate JSON round trip failed")
	}
}

// testNetworkRateSamples creates a consistent pair
// of cumulative interface readings.
func testNetworkRateSamples() (
	NetworkInterfaceUsage,
	NetworkInterfaceUsage,
) {
	previous := NetworkInterfaceUsage{
		Name:  "eth0",
		Index: 2,
		Up:    true,
		MTU:   1500,

		LinkSpeedBitsPerSecond: networkUint64Ptr(
			1000000000,
		),

		ReceivedBytes:      10000,
		TransmittedBytes:   5000,
		ReceivedPackets:    100,
		TransmittedPackets: 50,
		ReceiveErrors:      2,
		TransmitErrors:     1,

		ReceiveDropped:  networkUint64Ptr(3),
		TransmitDropped: networkUint64Ptr(2),
	}

	current := previous

	current.ReceivedBytes += 6000
	current.TransmittedBytes += 3000
	current.ReceivedPackets += 60
	current.TransmittedPackets += 30
	current.ReceiveErrors++
	current.TransmitErrors++

	current.ReceiveDropped = networkUint64Ptr(4)
	current.TransmitDropped = networkUint64Ptr(3)

	return previous, current
}

func requireNetworkRate(
	t *testing.T,
	actual NetworkInterfaceRate,
	expected NetworkInterfaceRate,
) {
	t.Helper()

	if actual.Name != expected.Name ||
		actual.Index != expected.Index {
		t.Fatalf(
			"incorrect interface identity: %+v",
			actual,
		)
	}

	if actual.SampleIntervalMillis !=
		expected.SampleIntervalMillis {
		t.Fatalf(
			"expected interval %d, got %d",
			expected.SampleIntervalMillis,
			actual.SampleIntervalMillis,
		)
	}

	requireNetworkRateValue(
		t,
		actual.ReceiveBytesPerSecond,
		expected.ReceiveBytesPerSecond,
	)

	requireNetworkRateValue(
		t,
		actual.TransmitBytesPerSecond,
		expected.TransmitBytesPerSecond,
	)

	requireNetworkRateValue(
		t,
		actual.ReceivePacketsPerSecond,
		expected.ReceivePacketsPerSecond,
	)

	requireNetworkRateValue(
		t,
		actual.TransmitPacketsPerSecond,
		expected.TransmitPacketsPerSecond,
	)
}

func requireNetworkRateValue(
	t *testing.T,
	actual float64,
	expected float64,
) {
	t.Helper()

	if math.IsNaN(actual) ||
		math.IsInf(actual, 0) ||
		actual < 0 {
		t.Fatalf("invalid network rate: %v", actual)
	}

	tolerance := math.Max(
		1e-9,
		math.Abs(expected)*1e-12,
	)

	if math.Abs(actual-expected) > tolerance {
		t.Fatalf(
			"expected rate %v, got %v",
			expected,
			actual,
		)
	}
}
