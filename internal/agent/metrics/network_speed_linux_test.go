package metrics

import (
	"context"
	"errors"
	"math"
	"os"
	"reflect"
	"syscall"
	"testing"
	"time"
)

func TestParseLinuxLinkSpeed(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *uint64
	}{
		{
			name:     "10 Mbps",
			input:    "10\n",
			expected: networkUint64Ptr(10000000),
		},
		{
			name:     "100 Mbps",
			input:    "100\n",
			expected: networkUint64Ptr(100000000),
		},
		{
			name:     "gigabit Ethernet",
			input:    "1000\n",
			expected: networkUint64Ptr(1000000000),
		},
		{
			name:     "2.5 gigabit Ethernet",
			input:    "2500\n",
			expected: networkUint64Ptr(2500000000),
		},
		{
			name:     "10 gigabit Ethernet",
			input:    "10000\n",
			expected: networkUint64Ptr(10000000000),
		},
		{
			name:     "25 gigabit Ethernet",
			input:    "25000\n",
			expected: networkUint64Ptr(25000000000),
		},
		{
			name:     "100 gigabit Ethernet",
			input:    "100000\n",
			expected: networkUint64Ptr(100000000000),
		},
		{
			name:     "800 gigabit Ethernet",
			input:    "800000\n",
			expected: networkUint64Ptr(800000000000),
		},
		{
			name:     "whitespace",
			input:    " \t1000\r\n",
			expected: networkUint64Ptr(1000000000),
		},
		{
			name:  "maximum supported integer",
			input: "2147483647",
			expected: networkUint64Ptr(
				uint64(math.MaxInt32) * 1000000,
			),
		},
		{
			name:  "unknown signed speed",
			input: "-1\n",
		},
		{
			name:  "unknown unsigned speed",
			input: "4294967295\n",
		},
		{
			name:  "zero speed",
			input: "0\n",
		},
		{
			name:  "negative speed",
			input: "-1000",
		},
		{
			name:  "invalid text",
			input: "unknown",
		},
		{
			name:  "fractional speed",
			input: "2.5",
		},
		{
			name:  "unexpected units",
			input: "1000 Mbps",
		},
		{
			name:  "multiple values",
			input: "1000\n2000\n",
		},
		{
			name: "empty input",
		},
		{
			name:  "whitespace only",
			input: " \n\t",
		},
		{
			name:  "unsupported integer range",
			input: "2147483648",
		},
		{
			name:  "uint32 overflow",
			input: "4294967296",
		},
		{
			name:  "uint64 overflow",
			input: "18446744073709551616",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := parseLinuxLinkSpeed(
				[]byte(tt.input),
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

func TestLinuxNetworkIndexMatches(t *testing.T) {
	tests := []struct {
		input    string
		expected uint32
		matches  bool
	}{
		{"2\n", 2, true},
		{" 10 \n", 10, true},
		{"3\n", 2, false},
		{"0\n", 0, false},
		{"-1\n", 2, false},
		{"invalid", 2, false},
		{"4294967296", 2, false},
		{"", 2, false},
	}

	for _, tt := range tests {
		actual := linuxNetworkIndexMatches(
			[]byte(tt.input),
			tt.expected,
		)

		if actual != tt.matches {
			t.Fatalf(
				"input %q: expected %t, got %t",
				tt.input,
				tt.matches,
				actual,
			)
		}
	}
}

func TestReadLinuxLinkSpeed(t *testing.T) {
	iface := NetworkInterfaceUsage{
		Name:  "eth0",
		Index: 2,
		Up:    true,
	}

	var paths []string

	speed, err := readLinuxLinkSpeed(
		context.Background(),
		iface,
		func(path string) ([]byte, error) {
			paths = append(paths, path)

			switch path {
			case "/sys/class/net/eth0/ifindex":
				return []byte("2\n"), nil

			case "/sys/class/net/eth0/speed":
				return []byte("2500\n"), nil

			default:
				t.Fatalf(
					"unexpected file read: %s",
					path,
				)

				return nil, nil
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if speed == nil || *speed != 2500000000 {
		t.Fatal("incorrect link speed")
	}

	expectedPaths := []string{
		"/sys/class/net/eth0/ifindex",
		"/sys/class/net/eth0/speed",
		"/sys/class/net/eth0/ifindex",
	}

	if !reflect.DeepEqual(paths, expectedPaths) {
		t.Fatalf("unexpected read order: %v", paths)
	}
}

func TestReadLinuxLinkSpeedSkipped(t *testing.T) {
	tests := []NetworkInterfaceUsage{
		{
			Name:     "lo",
			Index:    1,
			Up:       true,
			Loopback: true,
		},
		{
			Name:  "eth0",
			Index: 2,
			Up:    false,
		},
		{
			Name:  "",
			Index: 2,
			Up:    true,
		},
		{
			Name:  "..",
			Index: 2,
			Up:    true,
		},
		{
			Name:  ".",
			Index: 2,
			Up:    true,
		},
		{
			Name:  "../../eth0",
			Index: 2,
			Up:    true,
		},
		{
			Name:  "eth0\x00",
			Index: 2,
			Up:    true,
		},
		{
			Name: "eth0",
			Up:   true,
		},
	}

	for _, iface := range tests {
		speed, err := readLinuxLinkSpeed(
			context.Background(),
			iface,
			func(string) ([]byte, error) {
				t.Fatal("unexpected sysfs read")
				return nil, nil
			},
		)

		if err != nil {
			t.Fatal(err)
		}

		if speed != nil {
			t.Fatal("skipped interface returned speed")
		}
	}
}

func TestReadLinuxLinkSpeedUnavailable(t *testing.T) {
	iface := NetworkInterfaceUsage{
		Name:  "eth0",
		Index: 2,
		Up:    true,
	}

	for _, readError := range []error{
		os.ErrNotExist,
		os.ErrPermission,
		syscall.EINVAL,
		syscall.EOPNOTSUPP,
		syscall.ENODEV,
		syscall.EIO,
	} {
		t.Run(readError.Error(), func(t *testing.T) {
			speed, err := readLinuxLinkSpeed(
				context.Background(),
				iface,
				func(path string) ([]byte, error) {
					if path == "/sys/class/net/eth0/ifindex" {
						return []byte("2\n"), nil
					}

					return nil, readError
				},
			)

			if err != nil {
				t.Fatal(err)
			}

			if speed != nil {
				t.Fatal("unavailable speed returned a value")
			}
		})
	}
}

func TestReadLinuxLinkSpeedIdentityChanged(t *testing.T) {
	iface := NetworkInterfaceUsage{
		Name:  "eth0",
		Index: 2,
		Up:    true,
	}

	tests := []struct {
		name       string
		responses  []string
		readErrors []error
	}{
		{
			name:      "initial index mismatch",
			responses: []string{"3\n"},
		},
		{
			name:      "invalid initial index",
			responses: []string{"invalid"},
		},
		{
			name:      "final index mismatch",
			responses: []string{"2\n", "1000\n", "3\n"},
		},
		{
			name:      "interface disappeared",
			responses: []string{"2\n", "1000\n", ""},
			readErrors: []error{
				nil,
				nil,
				os.ErrNotExist,
			},
		},
		{
			name:       "initial index unavailable",
			responses:  []string{""},
			readErrors: []error{os.ErrNotExist},
		},
		{
			name:      "unknown link speed",
			responses: []string{"2\n", "-1\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0

			speed, err := readLinuxLinkSpeed(
				context.Background(),
				iface,
				func(string) ([]byte, error) {
					if calls >= len(tt.responses) {
						t.Fatal("unexpected file read")
					}

					value := tt.responses[calls]

					var readErr error

					if calls < len(tt.readErrors) {
						readErr = tt.readErrors[calls]
					}

					calls++

					return []byte(value), readErr
				},
			)

			if err != nil {
				t.Fatal(err)
			}

			if speed != nil {
				t.Fatal("invalid identity returned speed")
			}

			if calls != len(tt.responses) {
				t.Fatal("unexpected number of file reads")
			}
		})
	}
}

func TestReadLinuxLinkSpeedCanceled(t *testing.T) {
	iface := NetworkInterfaceUsage{
		Name:  "eth0",
		Index: 2,
		Up:    true,
	}

	for _, cancelStage := range []int{0, 1, 2, 3} {
		ctx, cancel := context.WithCancel(
			context.Background(),
		)

		calls := 0

		if cancelStage == 0 {
			cancel()
		}

		speed, err := readLinuxLinkSpeed(
			ctx,
			iface,
			func(path string) ([]byte, error) {
				calls++

				if calls == cancelStage {
					cancel()
				}

				if path == "/sys/class/net/eth0/speed" {
					return []byte("1000\n"), nil
				}

				return []byte("2\n"), nil
			},
		)

		cancel()

		if !errors.Is(err, context.Canceled) {
			t.Fatalf(
				"expected cancellation, got %v",
				err,
			)
		}

		if speed != nil {
			t.Fatal("canceled collection returned speed")
		}

		if calls != cancelStage {
			t.Fatal("collection continued after cancellation")
		}
	}
}

func TestCollectLinuxNetworkUsage(t *testing.T) {
	original := []NetworkInterfaceUsage{
		{
			Name:             "eth0",
			Index:            2,
			Up:               true,
			MTU:              1500,
			ReceivedBytes:    5000,
			TransmittedBytes: 2500,
			ReceiveDropped:   networkUint64Ptr(0),
			TransmitDropped:  networkUint64Ptr(0),
		},
		{
			Name:     "lo",
			Index:    1,
			Up:       true,
			Loopback: true,
			MTU:      65536,
		},
		{
			Name:  "wg0",
			Index: 3,
			Up:    true,
		},
	}

	interfaces, err := collectLinuxNetworkUsage(
		context.Background(),
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			return original, nil
		},
		func(path string) ([]byte, error) {
			switch path {
			case "/sys/class/net/eth0/ifindex":
				return []byte("2\n"), nil

			case "/sys/class/net/eth0/speed":
				return []byte("1000\n"), nil

			case "/sys/class/net/wg0/ifindex":
				return []byte("3\n"), nil

			case "/sys/class/net/wg0/speed":
				return nil, syscall.EOPNOTSUPP

			default:
				t.Fatalf("unexpected file read: %s", path)
				return nil, nil
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := append(
		[]NetworkInterfaceUsage{},
		original...,
	)

	expected[0].LinkSpeedBitsPerSecond =
		networkUint64Ptr(1000000000)

	if !reflect.DeepEqual(interfaces, expected) {
		t.Fatalf(
			"unexpected interfaces: %+v",
			interfaces,
		)
	}

	if original[0].LinkSpeedBitsPerSecond != nil {
		t.Fatal("original counter records were modified")
	}
}

func TestCollectLinuxNetworkUsageEmpty(t *testing.T) {
	interfaces, err := collectLinuxNetworkUsage(
		context.Background(),
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			return nil, nil
		},
		func(string) ([]byte, error) {
			t.Fatal("unexpected file read")
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

func TestCollectLinuxNetworkUsageFailure(t *testing.T) {
	expectedError := errors.New("counter collection failed")

	interfaces, err := collectLinuxNetworkUsage(
		context.Background(),
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			return nil, expectedError
		},
		func(string) ([]byte, error) {
			t.Fatal("unexpected file read")
			return nil, nil
		},
	)

	if !errors.Is(err, expectedError) {
		t.Fatalf("expected counter failure, got %v", err)
	}

	if interfaces != nil {
		t.Fatal("failed collection returned interfaces")
	}
}

func TestCollectLinuxNetworkUsageInvalid(t *testing.T) {
	interfaces, err := collectLinuxNetworkUsage(
		context.Background(),
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			return []NetworkInterfaceUsage{
				{Name: "eth0", Index: 2},
				{Name: "eth0", Index: 3},
			}, nil
		},
		func(string) ([]byte, error) {
			t.Fatal("unexpected file read")
			return nil, nil
		},
	)

	if !errors.Is(err, ErrInvalidNetworkStatistics) {
		t.Fatalf("expected invalid statistics, got %v", err)
	}

	if interfaces != nil {
		t.Fatal("invalid collection returned interfaces")
	}
}

func TestCollectLinuxNetworkUsageCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	interfaces, err := collectLinuxNetworkUsage(
		ctx,
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			cancel()
			return nil, nil
		},
		func(string) ([]byte, error) {
			t.Fatal("unexpected file read")
			return nil, nil
		},
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	if interfaces != nil {
		t.Fatal("canceled collection returned interfaces")
	}
}

func TestReadLinuxNetworkUsageNative(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	interfaces, err := readNetworkUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if interfaces == nil {
		t.Fatal("interface slice is nil")
	}

	if err := validateNetworkInterfaces(interfaces); err != nil {
		t.Fatal(err)
	}

	t.Logf(
		"Collected %d Linux network interfaces",
		len(interfaces),
	)

	for _, iface := range interfaces {
		if iface.ReceiveDropped == nil ||
			iface.TransmitDropped == nil {
			t.Fatal("missing Linux drop counters")
		}

		speed := iface.LinkSpeedBitsPerSecond

		if iface.Loopback || !iface.Up {
			if speed != nil {
				t.Fatal("ineligible interface reported speed")
			}
		}

		if speed == nil {
			t.Logf(
				"interface=%q speed=unavailable rx_bytes=%d tx_bytes=%d",
				iface.Name,
				iface.ReceivedBytes,
				iface.TransmittedBytes,
			)

			continue
		}

		if *speed == 0 ||
			*speed%linuxBitsPerMegabit != 0 {
			t.Fatal("invalid normalized link speed")
		}

		t.Logf(
			"interface=%q speed=%d bits/s rx_bytes=%d tx_bytes=%d",
			iface.Name,
			*speed,
			iface.ReceivedBytes,
			iface.TransmittedBytes,
		)
	}
}
