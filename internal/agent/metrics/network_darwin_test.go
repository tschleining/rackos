package metrics

import (
	"context"
	"errors"
	"math"
	"net"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestDiscoverDarwinInterfaces(t *testing.T) {
	native := []net.Interface{
		{
			Name:  "utun0",
			Index: 12,
			MTU:   1280,
			Flags: net.FlagUp | net.FlagPointToPoint,
		},
		{
			Name:  "lo0",
			Index: 1,
			MTU:   16384,
			Flags: net.FlagUp | net.FlagLoopback,
		},
		{
			Name:  "en0",
			Index: 6,
			MTU:   1500,
			Flags: net.FlagUp | net.FlagRunning,
			HardwareAddr: net.HardwareAddr{
				0x02, 0x00, 0x00,
				0x00, 0x00, 0x01,
			},
		},
		{
			Name:  "bridge0",
			Index: 8,
			MTU:   1500,
			Flags: net.FlagUp,
		},
		{
			Name:  "en1",
			Index: 7,
			MTU:   1500,
		},
		{
			Name:  "awdl0",
			Index: 10,
			MTU:   1500,
			Flags: net.FlagUp | net.FlagMulticast,
		},
	}

	interfaces, err := discoverDarwinInterfacesWithReader(
		context.Background(),
		func() ([]net.Interface, error) {
			return native, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := []NetworkInterfaceUsage{
		{
			Name:  "awdl0",
			Index: 10,
			MTU:   1500,
			Up:    true,
		},
		{
			Name:  "bridge0",
			Index: 8,
			MTU:   1500,
			Up:    true,
		},
		{
			Name:  "en0",
			Index: 6,
			MTU:   1500,
			Up:    true,
		},
		{
			Name:  "en1",
			Index: 7,
			MTU:   1500,
		},
		{
			Name:     "lo0",
			Index:    1,
			MTU:      16384,
			Up:       true,
			Loopback: true,
		},
		{
			Name:  "utun0",
			Index: 12,
			MTU:   1280,
			Up:    true,
		},
	}

	if !reflect.DeepEqual(interfaces, expected) {
		t.Fatalf(
			"expected %+v, got %+v",
			expected,
			interfaces,
		)
	}
}

func TestNormalizeNetworkInterfaceFlags(t *testing.T) {
	tests := []struct {
		name     string
		flags    net.Flags
		up       bool
		loopback bool
	}{
		{
			name:  "administratively up",
			flags: net.FlagUp,
			up:    true,
		},
		{
			name: "administratively down",
		},
		{
			name:  "running without administrative up",
			flags: net.FlagRunning,
		},
		{
			name:     "loopback up",
			flags:    net.FlagUp | net.FlagLoopback,
			up:       true,
			loopback: true,
		},
		{
			name:     "loopback down",
			flags:    net.FlagLoopback,
			loopback: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iface, err := normalizeNetworkInterface(
				net.Interface{
					Name:  "test0",
					Index: 2,
					MTU:   1500,
					Flags: tt.flags,
				},
			)
			if err != nil {
				t.Fatal(err)
			}

			if iface.Up != tt.up {
				t.Fatal("incorrect administrative status")
			}

			if iface.Loopback != tt.loopback {
				t.Fatal("incorrect loopback status")
			}
		})
	}
}

func TestNormalizeNetworkInterfaceInvalid(t *testing.T) {
	tests := []net.Interface{
		{
			Name:  "",
			Index: 2,
		},
		{
			Name:  "en0",
			Index: 0,
		},
		{
			Name:  "en0",
			Index: -1,
		},
		{
			Name:  "en0",
			Index: 2,
			MTU:   -1,
		},
	}

	for _, native := range tests {
		iface, err := normalizeNetworkInterface(native)

		if !errors.Is(err, ErrInvalidNetworkStatistics) {
			t.Fatalf(
				"expected invalid statistics, got %v",
				err,
			)
		}

		if iface != (NetworkInterfaceUsage{}) {
			t.Fatal("invalid metadata returned interface")
		}
	}
}

func TestNormalizeNetworkInterfaceOverflow(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("int cannot represent values above uint32")
	}

	overflow := uint64(math.MaxUint32) + 1

	tests := []net.Interface{
		{
			Name:  "en0",
			Index: int(overflow),
			MTU:   1500,
		},
		{
			Name:  "en0",
			Index: 2,
			MTU:   int(overflow),
		},
	}

	for _, native := range tests {
		iface, err := normalizeNetworkInterface(native)

		if !errors.Is(err, ErrInvalidNetworkStatistics) {
			t.Fatalf("expected overflow error, got %v", err)
		}

		if iface != (NetworkInterfaceUsage{}) {
			t.Fatal("overflow returned interface metadata")
		}
	}
}

func TestDiscoverDarwinInterfacesInvalid(t *testing.T) {
	tests := []struct {
		name   string
		native []net.Interface
	}{
		{
			name: "duplicate names",
			native: []net.Interface{
				{Name: "en0", Index: 2},
				{Name: "en0", Index: 3},
			},
		},
		{
			name: "duplicate indexes",
			native: []net.Interface{
				{Name: "en0", Index: 2},
				{Name: "en1", Index: 2},
			},
		},
		{
			name: "invalid interface",
			native: []net.Interface{
				{Name: "en0", Index: 2},
				{Name: "en1", Index: -1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interfaces, err :=
				discoverDarwinInterfacesWithReader(
					context.Background(),
					func() ([]net.Interface, error) {
						return tt.native, nil
					},
				)

			if !errors.Is(err, ErrInvalidNetworkStatistics) {
				t.Fatalf(
					"expected invalid statistics, got %v",
					err,
				)
			}

			if interfaces != nil {
				t.Fatal("invalid discovery returned interfaces")
			}
		})
	}
}

func TestDiscoverDarwinInterfacesEmpty(t *testing.T) {
	interfaces, err := discoverDarwinInterfacesWithReader(
		context.Background(),
		func() ([]net.Interface, error) {
			return nil, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if interfaces == nil || len(interfaces) != 0 {
		t.Fatal("expected initialized empty interface slice")
	}
}

func TestDiscoverDarwinInterfacesReadFailure(t *testing.T) {
	expectedError := errors.New("interface discovery failed")

	interfaces, err := discoverDarwinInterfacesWithReader(
		context.Background(),
		func() ([]net.Interface, error) {
			return nil, expectedError
		},
	)

	if !errors.Is(err, expectedError) {
		t.Fatalf("expected discovery failure, got %v", err)
	}

	if interfaces != nil {
		t.Fatal("failed discovery returned interfaces")
	}
}

func TestDiscoverDarwinInterfacesCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	interfaces, err := discoverDarwinInterfacesWithReader(
		ctx,
		func() ([]net.Interface, error) {
			t.Fatal("reader invoked after cancellation")
			return nil, nil
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	if interfaces != nil {
		t.Fatal("canceled discovery returned interfaces")
	}
}

func TestDiscoverDarwinInterfacesCanceledDuringRead(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	interfaces, err := discoverDarwinInterfacesWithReader(
		ctx,
		func() ([]net.Interface, error) {
			cancel()

			return []net.Interface{
				{Name: "en0", Index: 6},
			}, nil
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	if interfaces != nil {
		t.Fatal("canceled discovery returned interfaces")
	}
}

func TestDiscoverDarwinInterfacesNative(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	interfaces, err := discoverDarwinInterfaces(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if interfaces == nil {
		t.Fatal("interface slice is nil")
	}

	if err := validateNetworkInterfaces(interfaces); err != nil {
		t.Fatal(err)
	}

	if len(interfaces) == 0 {
		t.Fatal("no macOS network interfaces discovered")
	}

	t.Logf(
		"Discovered %d macOS network interfaces",
		len(interfaces),
	)

	foundLoopback := false

	for i, iface := range interfaces {
		if i > 0 &&
			interfaces[i-1].Name >= iface.Name {
			t.Fatal("invalid interface ordering")
		}

		if iface.Loopback {
			foundLoopback = true
		}

		t.Logf(
			"interface=%q index=%d up=%t loopback=%t mtu=%d",
			iface.Name,
			iface.Index,
			iface.Up,
			iface.Loopback,
			iface.MTU,
		)
	}

	if !foundLoopback {
		t.Fatal("no loopback interface discovered")
	}
}
