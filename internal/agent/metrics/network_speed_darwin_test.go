package metrics

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestNormalizeDarwinLinkSpeed(t *testing.T) {
	tests := []struct {
		name     string
		baudRate uint64
		up       bool
		loopback bool
		expected *uint64
	}{
		{
			name:     "10 Mbps",
			baudRate: 10000000,
			up:       true,
			expected: networkUint64Ptr(10000000),
		},
		{
			name:     "100 Mbps",
			baudRate: 100000000,
			up:       true,
			expected: networkUint64Ptr(100000000),
		},
		{
			name:     "gigabit Ethernet",
			baudRate: 1000000000,
			up:       true,
			expected: networkUint64Ptr(1000000000),
		},
		{
			name:     "2.5 gigabit Ethernet",
			baudRate: 2500000000,
			up:       true,
			expected: networkUint64Ptr(2500000000),
		},
		{
			name:     "5 gigabit Ethernet",
			baudRate: 5000000000,
			up:       true,
			expected: networkUint64Ptr(5000000000),
		},
		{
			name:     "10 gigabit Ethernet",
			baudRate: 10000000000,
			up:       true,
			expected: networkUint64Ptr(10000000000),
		},
		{
			name:     "25 gigabit Ethernet",
			baudRate: 25000000000,
			up:       true,
			expected: networkUint64Ptr(25000000000),
		},
		{
			name:     "40 gigabit interface",
			baudRate: 40000000000,
			up:       true,
			expected: networkUint64Ptr(40000000000),
		},
		{
			name:     "100 gigabit Ethernet",
			baudRate: 100000000000,
			up:       true,
			expected: networkUint64Ptr(100000000000),
		},
		{
			name:     "800 gigabit Ethernet",
			baudRate: 800000000000,
			up:       true,
			expected: networkUint64Ptr(800000000000),
		},
		{
			name:     "nonstandard wireless speed",
			baudRate: 866700000,
			up:       true,
			expected: networkUint64Ptr(866700000),
		},
		{
			name:     "unknown speed",
			baudRate: 0,
			up:       true,
		},
		{
			name:     "maximum unsigned value",
			baudRate: math.MaxUint64,
			up:       true,
		},
		{
			name:     "disabled interface",
			baudRate: 1000000000,
			up:       false,
		},
		{
			name:     "loopback interface",
			baudRate: 10000000000,
			up:       true,
			loopback: true,
		},
		{
			name:     "disabled loopback",
			baudRate: 10000000000,
			loopback: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := normalizeDarwinLinkSpeed(
				tt.baudRate,
				tt.up,
				tt.loopback,
			)

			if !reflect.DeepEqual(actual, tt.expected) {
				if actual == nil {
					t.Fatal("expected speed, got nil")
				}

				if tt.expected == nil {
					t.Fatalf(
						"expected nil, got %d",
						*actual,
					)
				}

				t.Fatalf(
					"expected %d, got %d",
					*tt.expected,
					*actual,
				)
			}
		})
	}
}

func TestParseDarwinNetworkCountersLinkSpeed(t *testing.T) {
	tests := []struct {
		name     string
		flags    uint32
		baudRate uint64
		expected *uint64
	}{
		{
			name:     "gigabit Ethernet",
			flags:    unix.IFF_UP,
			baudRate: 1000000000,
			expected: networkUint64Ptr(1000000000),
		},
		{
			name:     "10 gigabit Ethernet",
			flags:    unix.IFF_UP,
			baudRate: 10000000000,
			expected: networkUint64Ptr(10000000000),
		},
		{
			name:     "40 gigabit interface",
			flags:    unix.IFF_UP,
			baudRate: 40000000000,
			expected: networkUint64Ptr(40000000000),
		},
		{
			name:     "unknown speed",
			flags:    unix.IFF_UP,
			baudRate: 0,
		},
		{
			name:     "invalid maximum value",
			flags:    unix.IFF_UP,
			baudRate: math.MaxUint64,
		},
		{
			name:     "disabled interface",
			baudRate: 1000000000,
		},
		{
			name:     "loopback",
			flags:    unix.IFF_UP | unix.IFF_LOOPBACK,
			baudRate: 10000000000,
		},
		{
			name:     "running without administrative up",
			flags:    unix.IFF_RUNNING,
			baudRate: 1000000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := testDarwinNetworkRecord(
				"test0",
				tt.flags,
				1500,
			)

			// Use the absolute native ABI offset.
			binary.NativeEndian.PutUint64(
				data[68:76],
				tt.baudRate,
			)

			iface, err := parseDarwinNetworkCounters(
				NetworkInterfaceUsage{
					Name:  "test0",
					Index: 6,
				},
				data,
			)
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(
				iface.LinkSpeedBitsPerSecond,
				tt.expected,
			) {
				t.Fatal("incorrect normalized link speed")
			}

			// Link speed handling must not modify
			// the adjacent cumulative counters.
			if iface.ReceivedPackets != 420000 ||
				iface.TransmittedPackets != 210000 ||
				iface.ReceivedBytes != 524288000 ||
				iface.TransmittedBytes != 262144000 ||
				iface.ReceiveErrors != 2 ||
				iface.TransmitErrors != 1 {
				t.Fatal("network counters were modified")
			}

			if iface.ReceiveDropped == nil ||
				*iface.ReceiveDropped != 3 {
				t.Fatal("receive drop counter was modified")
			}

			if iface.TransmitDropped != nil {
				t.Fatal("unexpected transmit drop counter")
			}
		})
	}
}

func TestReadDarwinNetworkUsageCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	interfaces, err := readNetworkUsage(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected cancellation, got %v",
			err,
		)
	}

	if interfaces != nil {
		t.Fatal("canceled collection returned interfaces")
	}
}

func TestReadDarwinNetworkUsageNative(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	interfaces, err := readNetworkUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(interfaces) == 0 {
		t.Fatal("no macOS network interfaces collected")
	}

	if err := validateNetworkInterfaces(interfaces); err != nil {
		t.Fatal(err)
	}

	foundLoopback := false
	supportedSpeeds := 0

	for i, iface := range interfaces {
		if i > 0 && interfaces[i-1].Name >= iface.Name {
			t.Fatal("incorrect interface ordering")
		}

		if iface.Loopback {
			foundLoopback = true
		}

		if iface.ReceiveDropped == nil {
			t.Fatal("missing receive drop counter")
		}

		if iface.TransmitDropped != nil {
			t.Fatal("unexpected transmit drop counter")
		}

		speed := iface.LinkSpeedBitsPerSecond

		if iface.Loopback || !iface.Up {
			if speed != nil {
				t.Fatal("ineligible interface reported speed")
			}
		}

		if speed == nil {
			t.Logf(
				"interface=%q index=%d up=%t loopback=%t speed=unavailable rx_bytes=%d tx_bytes=%d",
				iface.Name,
				iface.Index,
				iface.Up,
				iface.Loopback,
				iface.ReceivedBytes,
				iface.TransmittedBytes,
			)

			continue
		}

		if *speed == 0 || *speed == math.MaxUint64 {
			t.Fatal("invalid normalized link speed")
		}

		supportedSpeeds++

		t.Logf(
			"interface=%q index=%d up=%t loopback=%t speed=%d bits/s rx_bytes=%d tx_bytes=%d",
			iface.Name,
			iface.Index,
			iface.Up,
			iface.Loopback,
			*speed,
			iface.ReceivedBytes,
			iface.TransmittedBytes,
		)
	}

	if !foundLoopback {
		t.Fatal("no loopback interface collected")
	}

	t.Logf(
		"Collected %d interfaces; %d reported link speeds",
		len(interfaces),
		supportedSpeeds,
	)
}
