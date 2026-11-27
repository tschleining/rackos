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

func TestParseDarwinNetworkCounters(t *testing.T) {
	data := testDarwinNetworkRecord(
		"en0",
		unix.IFF_UP,
		1500,
	)

	iface, err := parseDarwinNetworkCounters(
		NetworkInterfaceUsage{
			Name:  "en0",
			Index: 6,
		},
		data,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := NetworkInterfaceUsage{
		Name:  "en0",
		Index: 6,
		Up:    true,
		MTU:   1500,

		LinkSpeedBitsPerSecond: networkUint64Ptr(
			1000000000,
		),

		ReceivedBytes:      524288000,
		ReceivedPackets:    420000,
		ReceiveErrors:      2,
		TransmittedBytes:   262144000,
		TransmittedPackets: 210000,
		TransmitErrors:     1,

		ReceiveDropped: networkUint64Ptr(3),
	}

	if !reflect.DeepEqual(iface, expected) {
		t.Fatalf(
			"expected %+v, got %+v",
			expected,
			iface,
		)
	}
}

func TestParseDarwinNetworkCountersFlags(t *testing.T) {
	tests := []struct {
		name     string
		flags    uint32
		up       bool
		loopback bool
	}{
		{
			name:  "enabled interface",
			flags: unix.IFF_UP,
			up:    true,
		},
		{
			name: "disabled interface",
		},
		{
			name:     "loopback",
			flags:    unix.IFF_UP | unix.IFF_LOOPBACK,
			up:       true,
			loopback: true,
		},
		{
			name:  "running without up",
			flags: unix.IFF_RUNNING,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := testDarwinNetworkRecord(
				"test0",
				tt.flags,
				1500,
			)

			iface, err := parseDarwinNetworkCounters(
				NetworkInterfaceUsage{
					Name:     "test0",
					Index:    2,
					Up:       !tt.up,
					Loopback: !tt.loopback,
					MTU:      1280,
				},
				data,
			)
			if err != nil {
				t.Fatal(err)
			}

			if iface.Up != tt.up ||
				iface.Loopback != tt.loopback {
				t.Fatal("incorrect interface flags")
			}

			if iface.MTU != 1500 {
				t.Fatal("MTU was not refreshed")
			}
		})
	}
}

func TestParseDarwinNetworkCountersZero(t *testing.T) {
	data := make([]byte, 180)
	copy(data[:16], "en0")

	iface, err := parseDarwinNetworkCounters(
		NetworkInterfaceUsage{
			Name:  "en0",
			Index: 6,
		},
		data,
	)
	if err != nil {
		t.Fatal(err)
	}

	if iface.ReceivedBytes != 0 ||
		iface.TransmittedBytes != 0 ||
		iface.ReceivedPackets != 0 ||
		iface.TransmittedPackets != 0 {
		t.Fatal("zero counters were not preserved")
	}

	if iface.ReceiveDropped == nil ||
		*iface.ReceiveDropped != 0 {
		t.Fatal("expected supported zero receive drops")
	}

	if iface.TransmitDropped != nil {
		t.Fatal("unavailable transmit drops returned a value")
	}

	if iface.LinkSpeedBitsPerSecond != nil {
		t.Fatal("zero native baud rate returned link speed")
	}
}

func TestParseDarwinNetworkCountersLargeValues(t *testing.T) {
	data := testDarwinNetworkRecord(
		"en0",
		unix.IFF_UP,
		1500,
	)

	offsets := []int{
		76,
		84,
		92,
		100,
		116,
		124,
		148,
	}

	for i, offset := range offsets {
		binary.NativeEndian.PutUint64(
			data[offset:offset+8],
			uint64(math.MaxUint64)-uint64(i),
		)
	}

	iface, err := parseDarwinNetworkCounters(
		NetworkInterfaceUsage{
			Name:  "en0",
			Index: 6,
		},
		data,
	)
	if err != nil {
		t.Fatal(err)
	}

	actual := []uint64{
		iface.ReceivedPackets,
		iface.ReceiveErrors,
		iface.TransmittedPackets,
		iface.TransmitErrors,
		iface.ReceivedBytes,
		iface.TransmittedBytes,
		*iface.ReceiveDropped,
	}

	for i, value := range actual {
		expected := uint64(math.MaxUint64) - uint64(i)

		if value != expected {
			t.Fatalf(
				"counter %d: expected %d, got %d",
				i,
				expected,
				value,
			)
		}
	}
}

func TestParseDarwinNetworkCountersInvalidSize(t *testing.T) {
	expected := NetworkInterfaceUsage{
		Name:  "en0",
		Index: 6,
	}

	for _, size := range []int{
		0, 15, 51, 52, 179, 181, 256,
	} {
		iface, err := parseDarwinNetworkCounters(
			expected,
			make([]byte, size),
		)

		if !errors.Is(err, ErrInvalidNetworkStatistics) {
			t.Fatalf(
				"size %d: expected invalid statistics, got %v",
				size,
				err,
			)
		}

		if iface != (NetworkInterfaceUsage{}) {
			t.Fatal("invalid record returned interface")
		}
	}
}

func TestParseDarwinNetworkCountersIdentityChanged(t *testing.T) {
	expected := NetworkInterfaceUsage{
		Name:  "en0",
		Index: 6,
	}

	tests := [][]byte{
		make([]byte, 180),
		testDarwinNetworkRecord("en1", 0, 1500),
	}

	for _, data := range tests {
		iface, err := parseDarwinNetworkCounters(
			expected,
			data,
		)

		if !errors.Is(
			err,
			errDarwinNetworkInterfaceChanged,
		) {
			t.Fatalf(
				"expected changed interface error, got %v",
				err,
			)
		}

		if iface != (NetworkInterfaceUsage{}) {
			t.Fatal("changed interface returned counters")
		}
	}
}

func TestParseDarwinNetworkCountersInvalidName(t *testing.T) {
	data := make([]byte, 180)

	copy(
		data[:16],
		"abcdefghijklmnop",
	)

	iface, err := parseDarwinNetworkCounters(
		NetworkInterfaceUsage{
			Name:  "en0",
			Index: 6,
		},
		data,
	)

	if !errors.Is(err, ErrInvalidNetworkStatistics) {
		t.Fatalf("expected invalid name, got %v", err)
	}

	if iface != (NetworkInterfaceUsage{}) {
		t.Fatal("invalid name returned interface")
	}
}

func TestParseDarwinNetworkCountersInvalidIdentity(t *testing.T) {
	for _, expected := range []NetworkInterfaceUsage{
		{},
		{Name: "en0"},
		{Index: 6},
	} {
		iface, err := parseDarwinNetworkCounters(
			expected,
			testDarwinNetworkRecord("en0", 0, 1500),
		)

		if !errors.Is(err, ErrInvalidNetworkStatistics) {
			t.Fatalf("expected invalid identity, got %v", err)
		}

		if iface != (NetworkInterfaceUsage{}) {
			t.Fatal("invalid identity returned interface")
		}
	}
}

func TestReadDarwinInterfaceStatisticsInvalidIndex(t *testing.T) {
	for _, index := range []uint32{
		0,
		math.MaxUint32,
	} {
		data, err := readDarwinInterfaceStatistics(index)

		if !errors.Is(err, ErrInvalidNetworkStatistics) {
			t.Fatalf("expected invalid index, got %v", err)
		}

		if data != nil {
			t.Fatal("invalid index returned statistics")
		}
	}
}

func TestReadDarwinNetworkCountersWithReaders(t *testing.T) {
	discovered := []NetworkInterfaceUsage{
		{Name: "lo0", Index: 1},
		{Name: "en0", Index: 6},
		{Name: "utun0", Index: 12},
	}

	var indexes []uint32

	interfaces, err := readDarwinNetworkCountersWithReaders(
		context.Background(),
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			return discovered, nil
		},
		func(index uint32) ([]byte, error) {
			indexes = append(indexes, index)

			switch index {
			case 1:
				return testDarwinNetworkRecord(
					"lo0",
					unix.IFF_UP|unix.IFF_LOOPBACK,
					16384,
				), nil

			case 6:
				return testDarwinNetworkRecord(
					"en0",
					unix.IFF_UP,
					1500,
				), nil

			case 12:
				return testDarwinNetworkRecord(
					"utun0",
					unix.IFF_UP,
					1280,
				), nil

			default:
				t.Fatalf("unexpected index %d", index)
				return nil, nil
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(indexes, []uint32{1, 6, 12}) {
		t.Fatalf("unexpected interface reads: %v", indexes)
	}

	if len(interfaces) != 3 {
		t.Fatal("incorrect interface count")
	}

	if interfaces[0].Name != "en0" ||
		interfaces[1].Name != "lo0" ||
		interfaces[2].Name != "utun0" {
		t.Fatal("incorrect interface ordering")
	}

	if !interfaces[1].Loopback {
		t.Fatal("loopback interface flag was lost")
	}

	if discovered[0].ReceivedBytes != 0 {
		t.Fatal("discovery input was modified")
	}
}

func TestReadDarwinNetworkCountersDisappeared(t *testing.T) {
	discovered := []NetworkInterfaceUsage{
		{Name: "en0", Index: 6},
		{Name: "en1", Index: 7},
		{Name: "utun0", Index: 12},
	}

	interfaces, err := readDarwinNetworkCountersWithReaders(
		context.Background(),
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			return discovered, nil
		},
		func(index uint32) ([]byte, error) {
			switch index {
			case 6:
				return testDarwinNetworkRecord(
					"en0",
					unix.IFF_UP,
					1500,
				), nil

			case 7:
				return nil, unix.ENOENT

			case 12:
				return testDarwinNetworkRecord(
					"utun1",
					unix.IFF_UP,
					1280,
				), nil

			default:
				t.Fatal("unexpected interface index")
				return nil, nil
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(interfaces) != 1 ||
		interfaces[0].Name != "en0" {
		t.Fatalf("unexpected interfaces: %+v", interfaces)
	}
}

func TestReadDarwinNetworkCountersFailure(t *testing.T) {
	expectedError := errors.New("native statistics failed")

	interfaces, err := readDarwinNetworkCountersWithReaders(
		context.Background(),
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			return []NetworkInterfaceUsage{
				{Name: "en0", Index: 6},
				{Name: "en1", Index: 7},
			}, nil
		},
		func(index uint32) ([]byte, error) {
			if index == 6 {
				return testDarwinNetworkRecord(
					"en0", 0, 1500,
				), nil
			}

			return nil, expectedError
		},
	)

	if !errors.Is(err, expectedError) {
		t.Fatalf("expected read failure, got %v", err)
	}

	if interfaces != nil {
		t.Fatal("failed collection returned partial results")
	}
}

func TestReadDarwinNetworkCountersDiscoveryFailure(t *testing.T) {
	expectedError := errors.New("interface discovery failed")

	interfaces, err := readDarwinNetworkCountersWithReaders(
		context.Background(),
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			return nil, expectedError
		},
		func(uint32) ([]byte, error) {
			t.Fatal("unexpected statistics read")
			return nil, nil
		},
	)

	if !errors.Is(err, expectedError) {
		t.Fatalf("expected discovery failure, got %v", err)
	}

	if interfaces != nil {
		t.Fatal("failed discovery returned interfaces")
	}
}

func TestReadDarwinNetworkCountersInvalid(t *testing.T) {
	interfaces, err := readDarwinNetworkCountersWithReaders(
		context.Background(),
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			return []NetworkInterfaceUsage{
				{Name: "en0", Index: 6},
			}, nil
		},
		func(uint32) ([]byte, error) {
			return make([]byte, 179), nil
		},
	)

	if !errors.Is(err, ErrInvalidNetworkStatistics) {
		t.Fatalf("expected invalid statistics, got %v", err)
	}

	if interfaces != nil {
		t.Fatal("invalid collection returned interfaces")
	}
}

func TestReadDarwinNetworkCountersEmpty(t *testing.T) {
	interfaces, err := readDarwinNetworkCountersWithReaders(
		context.Background(),
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			return nil, nil
		},
		func(uint32) ([]byte, error) {
			t.Fatal("unexpected statistics read")
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

func TestReadDarwinNetworkCountersCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	interfaces, err := readDarwinNetworkCounters(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	if interfaces != nil {
		t.Fatal("canceled collection returned interfaces")
	}
}

func TestReadDarwinNetworkCountersCanceledDuringRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	interfaces, err := readDarwinNetworkCountersWithReaders(
		ctx,
		func(context.Context) ([]NetworkInterfaceUsage, error) {
			return []NetworkInterfaceUsage{
				{Name: "en0", Index: 6},
			}, nil
		},
		func(uint32) ([]byte, error) {
			cancel()
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

func TestReadDarwinNetworkCountersNative(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	interfaces, err := readDarwinNetworkCounters(ctx)
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

	t.Logf(
		"Collected %d macOS network interfaces",
		len(interfaces),
	)

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

		t.Logf(
			"interface=%q index=%d rx_bytes=%d tx_bytes=%d rx_packets=%d tx_packets=%d rx_errors=%d tx_errors=%d rx_dropped=%d",
			iface.Name,
			iface.Index,
			iface.ReceivedBytes,
			iface.TransmittedBytes,
			iface.ReceivedPackets,
			iface.TransmittedPackets,
			iface.ReceiveErrors,
			iface.TransmitErrors,
			*iface.ReceiveDropped,
		)
	}

	if !foundLoopback {
		t.Fatal("no loopback interface collected")
	}
}

// testDarwinNetworkRecord constructs a synthetic
// ifmibdata record using explicit native ABI offsets.
func testDarwinNetworkRecord(
	name string,
	flags uint32,
	mtu uint32,
) []byte {
	data := make([]byte, 180)

	copy(data[:16], name)

	binary.NativeEndian.PutUint32(
		data[20:24],
		flags,
	)

	binary.NativeEndian.PutUint32(
		data[60:64],
		mtu,
	)

	// Native link speed in bits per second.
	binary.NativeEndian.PutUint64(
		data[68:76],
		1000000000,
	)

	counters := map[int]uint64{
		76:  420000,
		84:  2,
		92:  210000,
		100: 1,
		116: 524288000,
		124: 262144000,
		148: 3,
	}

	for offset, value := range counters {
		binary.NativeEndian.PutUint64(
			data[offset:offset+8],
			value,
		)
	}

	return data
}
