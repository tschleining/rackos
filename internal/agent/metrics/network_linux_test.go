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

func TestNormalizeLinuxInterface(t *testing.T) {
	tests := []struct {
		name     string
		native   net.Interface
		expected NetworkInterfaceUsage
	}{
		{
			name: "physical Ethernet",
			native: net.Interface{
				Name:  "eth0",
				Index: 2,
				MTU:   1500,
				Flags: net.FlagUp |
					net.FlagBroadcast |
					net.FlagMulticast |
					net.FlagRunning,
			},
			expected: NetworkInterfaceUsage{
				Name:  "eth0",
				Index: 2,
				MTU:   1500,
				Up:    true,
			},
		},
		{
			name: "loopback",
			native: net.Interface{
				Name:  "lo",
				Index: 1,
				MTU:   65536,
				Flags: net.FlagUp |
					net.FlagLoopback,
			},
			expected: NetworkInterfaceUsage{
				Name:     "lo",
				Index:    1,
				MTU:      65536,
				Up:       true,
				Loopback: true,
			},
		},
		{
			name: "administratively disabled interface",
			native: net.Interface{
				Name:  "eth1",
				Index: 3,
				MTU:   1500,
				Flags: net.FlagBroadcast |
					net.FlagMulticast,
			},
			expected: NetworkInterfaceUsage{
				Name:  "eth1",
				Index: 3,
				MTU:   1500,
			},
		},
		{
			name: "enabled interface without running flag",
			native: net.Interface{
				Name:  "eth2",
				Index: 4,
				MTU:   1500,
				Flags: net.FlagUp,
			},
			expected: NetworkInterfaceUsage{
				Name:  "eth2",
				Index: 4,
				MTU:   1500,
				Up:    true,
			},
		},
		{
			name: "running flag without administrative up",
			native: net.Interface{
				Name:  "eth3",
				Index: 5,
				MTU:   1500,
				Flags: net.FlagRunning,
			},
			expected: NetworkInterfaceUsage{
				Name:  "eth3",
				Index: 5,
				MTU:   1500,
			},
		},
		{
			name: "virtual Ethernet",
			native: net.Interface{
				Name:  "veth1234",
				Index: 10,
				MTU:   1500,
				Flags: net.FlagUp,
			},
			expected: NetworkInterfaceUsage{
				Name:  "veth1234",
				Index: 10,
				MTU:   1500,
				Up:    true,
			},
		},
		{
			name: "VPN tunnel",
			native: net.Interface{
				Name:  "wg0",
				Index: 11,
				MTU:   1420,
				Flags: net.FlagUp |
					net.FlagPointToPoint,
			},
			expected: NetworkInterfaceUsage{
				Name:  "wg0",
				Index: 11,
				MTU:   1420,
				Up:    true,
			},
		},
		{
			name: "zero MTU",
			native: net.Interface{
				Name:  "eth0",
				Index: 2,
			},
			expected: NetworkInterfaceUsage{
				Name:  "eth0",
				Index: 2,
			},
		},
		{
			name: "hardware address excluded",
			native: net.Interface{
				Name:  "eth0",
				Index: 2,
				MTU:   1500,
				HardwareAddr: net.HardwareAddr{
					0x02, 0x00, 0x00,
					0x00, 0x00, 0x01,
				},
			},
			expected: NetworkInterfaceUsage{
				Name:  "eth0",
				Index: 2,
				MTU:   1500,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, err := normalizeLinuxInterface(
				tt.native,
			)
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(actual, tt.expected) {
				t.Fatalf(
					"expected %+v, got %+v",
					tt.expected,
					actual,
				)
			}
		})
	}
}

func TestNormalizeLinuxInterfaceInvalid(t *testing.T) {
	tests := []struct {
		name   string
		native net.Interface
	}{
		{
			name: "empty interface name",
			native: net.Interface{
				Index: 2,
				MTU:   1500,
			},
		},
		{
			name: "zero interface index",
			native: net.Interface{
				Name: "eth0",
				MTU:  1500,
			},
		},
		{
			name: "negative interface index",
			native: net.Interface{
				Name:  "eth0",
				Index: -1,
				MTU:   1500,
			},
		},
		{
			name: "negative MTU",
			native: net.Interface{
				Name:  "eth0",
				Index: 2,
				MTU:   -1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iface, err := normalizeLinuxInterface(
				tt.native,
			)

			if !errors.Is(
				err,
				ErrInvalidNetworkStatistics,
			) {
				t.Fatalf(
					"expected invalid network statistics, got %v",
					err,
				)
			}

			if iface != (NetworkInterfaceUsage{}) {
				t.Fatal("invalid metadata returned interface")
			}
		})
	}
}

func TestNormalizeLinuxInterfaceOverflow(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("int cannot represent values above uint32")
	}

	overflow := uint64(math.MaxUint32) + 1

	tests := []net.Interface{
		{
			Name:  "eth0",
			Index: int(overflow),
			MTU:   1500,
		},
		{
			Name:  "eth0",
			Index: 2,
			MTU:   int(overflow),
		},
	}

	for _, native := range tests {
		iface, err := normalizeLinuxInterface(native)

		if !errors.Is(
			err,
			ErrInvalidNetworkStatistics,
		) {
			t.Fatalf(
				"expected overflow validation error, got %v",
				err,
			)
		}

		if iface != (NetworkInterfaceUsage{}) {
			t.Fatal("overflow returned interface metadata")
		}
	}
}

func TestDiscoverLinuxInterfaces(t *testing.T) {
	native := []net.Interface{
		{
			Name:  "wg0",
			Index: 4,
			MTU:   1420,
			Flags: net.FlagUp,
		},
		{
			Name:  "lo",
			Index: 1,
			MTU:   65536,
			Flags: net.FlagUp |
				net.FlagLoopback,
		},
		{
			Name:  "eth0",
			Index: 2,
			MTU:   1500,
			Flags: net.FlagUp,
		},
		{
			Name:  "eth1",
			Index: 3,
			MTU:   1500,
		},
	}

	interfaces, err := discoverLinuxInterfacesWithReader(
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
			Name:  "eth0",
			Index: 2,
			MTU:   1500,
			Up:    true,
		},
		{
			Name:  "eth1",
			Index: 3,
			MTU:   1500,
		},
		{
			Name:     "lo",
			Index:    1,
			MTU:      65536,
			Up:       true,
			Loopback: true,
		},
		{
			Name:  "wg0",
			Index: 4,
			MTU:   1420,
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

func TestDiscoverLinuxInterfacesInvalid(t *testing.T) {
	tests := []struct {
		name   string
		native []net.Interface
	}{
		{
			name: "duplicate interface names",
			native: []net.Interface{
				{Name: "eth0", Index: 2},
				{Name: "eth0", Index: 3},
			},
		},
		{
			name: "duplicate interface indexes",
			native: []net.Interface{
				{Name: "eth0", Index: 2},
				{Name: "eth1", Index: 2},
			},
		},
		{
			name: "invalid interface metadata",
			native: []net.Interface{
				{Name: "eth0", Index: 2},
				{Name: "eth1", Index: -1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interfaces, err :=
				discoverLinuxInterfacesWithReader(
					context.Background(),
					func() ([]net.Interface, error) {
						return tt.native, nil
					},
				)

			if !errors.Is(
				err,
				ErrInvalidNetworkStatistics,
			) {
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

func TestDiscoverLinuxInterfacesEmpty(t *testing.T) {
	interfaces, err := discoverLinuxInterfacesWithReader(
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

func TestDiscoverLinuxInterfacesReadFailure(t *testing.T) {
	expectedError := errors.New(
		"interface discovery failed",
	)

	interfaces, err := discoverLinuxInterfacesWithReader(
		context.Background(),
		func() ([]net.Interface, error) {
			return nil, expectedError
		},
	)

	if !errors.Is(err, expectedError) {
		t.Fatalf(
			"expected discovery failure, got %v",
			err,
		)
	}

	if interfaces != nil {
		t.Fatal("failed discovery returned interfaces")
	}
}

func TestDiscoverLinuxInterfacesCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	interfaces, err := discoverLinuxInterfacesWithReader(
		ctx,
		func() ([]net.Interface, error) {
			t.Fatal("reader invoked after cancellation")
			return nil, nil
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected cancellation, got %v",
			err,
		)
	}

	if interfaces != nil {
		t.Fatal("canceled discovery returned interfaces")
	}
}

func TestDiscoverLinuxInterfacesCanceledDuringRead(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	interfaces, err := discoverLinuxInterfacesWithReader(
		ctx,
		func() ([]net.Interface, error) {
			cancel()

			return []net.Interface{
				{Name: "eth0", Index: 2},
			}, nil
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected cancellation, got %v",
			err,
		)
	}

	if interfaces != nil {
		t.Fatal("canceled discovery returned interfaces")
	}
}

func TestDiscoverLinuxInterfacesNative(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	interfaces, err := discoverLinuxInterfaces(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if interfaces == nil {
		t.Fatal("interface slice is nil")
	}

	if err := validateNetworkInterfaces(
		interfaces,
	); err != nil {
		t.Fatal(err)
	}

	if len(interfaces) == 0 {
		t.Skip("no network interfaces visible")
	}

	t.Logf(
		"Discovered %d Linux network interfaces",
		len(interfaces),
	)

	for i, iface := range interfaces {
		if i > 0 &&
			interfaces[i-1].Name >= iface.Name {
			t.Fatal("invalid interface ordering")
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
}
