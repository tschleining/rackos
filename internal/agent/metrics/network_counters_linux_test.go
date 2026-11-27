package metrics

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"reflect"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestApplyLinuxNetworkStats64(t *testing.T) {
	data := testLinuxNetworkStats64(
		420000,
		210000,
		524288000,
		262144000,
		2,
		1,
		3,
		4,
	)

	iface := NetworkInterfaceUsage{
		Name:  "eth0",
		Index: 2,
		Up:    true,
		MTU:   1500,
	}

	err := applyLinuxNetworkStats64(
		&iface,
		data,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := NetworkInterfaceUsage{
		Name:  "eth0",
		Index: 2,
		Up:    true,
		MTU:   1500,

		ReceivedPackets:    420000,
		TransmittedPackets: 210000,
		ReceivedBytes:      524288000,
		TransmittedBytes:   262144000,
		ReceiveErrors:      2,
		TransmitErrors:     1,

		ReceiveDropped:  networkUint64Ptr(3),
		TransmitDropped: networkUint64Ptr(4),
	}

	if !reflect.DeepEqual(iface, expected) {
		t.Fatalf(
			"expected %+v, got %+v",
			expected,
			iface,
		)
	}
}

func TestApplyLinuxNetworkStats64Zero(t *testing.T) {
	var iface NetworkInterfaceUsage

	err := applyLinuxNetworkStats64(
		&iface,
		make([]byte, 64),
	)
	if err != nil {
		t.Fatal(err)
	}

	if iface.ReceiveDropped == nil ||
		*iface.ReceiveDropped != 0 {
		t.Fatal("expected supported zero receive drops")
	}

	if iface.TransmitDropped == nil ||
		*iface.TransmitDropped != 0 {
		t.Fatal("expected supported zero transmit drops")
	}

	if iface.ReceivedBytes != 0 ||
		iface.TransmittedBytes != 0 {
		t.Fatal("zero counters were not preserved")
	}
}

func TestApplyLinuxNetworkStats64LargeCounters(t *testing.T) {
	data := testLinuxNetworkStats64(
		math.MaxUint64,
		math.MaxUint64-1,
		math.MaxUint64-2,
		math.MaxUint64-3,
		math.MaxUint64-4,
		math.MaxUint64-5,
		math.MaxUint64-6,
		math.MaxUint64-7,
	)

	var iface NetworkInterfaceUsage

	if err := applyLinuxNetworkStats64(
		&iface,
		data,
	); err != nil {
		t.Fatal(err)
	}

	actual := []uint64{
		iface.ReceivedPackets,
		iface.TransmittedPackets,
		iface.ReceivedBytes,
		iface.TransmittedBytes,
		iface.ReceiveErrors,
		iface.TransmitErrors,
		*iface.ReceiveDropped,
		*iface.TransmitDropped,
	}

	for i, value := range actual {
		expected := uint64(math.MaxUint64) -
			uint64(i)

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

func TestApplyLinuxNetworkStats64Extended(t *testing.T) {
	data := testLinuxNetworkStats64(
		10, 20, 1000, 2000, 1, 2, 3, 4,
	)

	// Simulate additional statistics supplied by
	// a newer Linux kernel.
	data = append(data, make([]byte, 136)...)

	var iface NetworkInterfaceUsage

	if err := applyLinuxNetworkStats64(
		&iface,
		data,
	); err != nil {
		t.Fatal(err)
	}

	if iface.ReceivedBytes != 1000 ||
		iface.TransmittedBytes != 2000 {
		t.Fatal("extended statistics changed counter values")
	}
}

func TestApplyLinuxNetworkStats64Invalid(t *testing.T) {
	for _, length := range []int{
		0, 1, 8, 32, 63,
	} {
		iface := NetworkInterfaceUsage{
			Name:          "eth0",
			Index:         2,
			ReceivedBytes: 100,
		}

		original := iface

		err := applyLinuxNetworkStats64(
			&iface,
			make([]byte, length),
		)

		if !errors.Is(
			err,
			ErrInvalidNetworkStatistics,
		) {
			t.Fatalf(
				"length %d: expected invalid statistics, got %v",
				length,
				err,
			)
		}

		if iface != original {
			t.Fatal("invalid statistics modified interface")
		}
	}
}

func TestParseLinuxNetworkLink(t *testing.T) {
	data := testLinuxNetworkLink(
		"eth0",
		2,
		unix.IFF_UP,
		1500,
	)

	iface, err := parseLinuxNetworkLink(data)
	if err != nil {
		t.Fatal(err)
	}

	if iface.Name != "eth0" ||
		iface.Index != 2 ||
		iface.MTU != 1500 ||
		!iface.Up ||
		iface.Loopback {
		t.Fatalf("unexpected metadata: %+v", iface)
	}

	if iface.ReceivedBytes != 524288000 ||
		iface.TransmittedBytes != 262144000 ||
		iface.ReceivedPackets != 420000 ||
		iface.TransmittedPackets != 210000 {
		t.Fatalf("unexpected counters: %+v", iface)
	}

	if iface.LinkSpeedBitsPerSecond != nil {
		t.Fatal("link speed should not be collected yet")
	}
}

func TestParseLinuxNetworkLinkLoopback(t *testing.T) {
	data := testLinuxNetworkLink(
		"lo",
		1,
		unix.IFF_UP|unix.IFF_LOOPBACK,
		65536,
	)

	iface, err := parseLinuxNetworkLink(data)
	if err != nil {
		t.Fatal(err)
	}

	if !iface.Up || !iface.Loopback {
		t.Fatal("loopback flags were not preserved")
	}
}

func TestParseLinuxNetworkLinkUnknownAttribute(t *testing.T) {
	data := testLinuxNetworkLink(
		"eth0",
		2,
		unix.IFF_UP,
		1500,
	)

	data = append(
		data,
		testLinuxNetworkAttribute(
			999,
			[]byte{1, 2, 3},
		)...,
	)

	iface, err := parseLinuxNetworkLink(data)
	if err != nil {
		t.Fatal(err)
	}

	if iface.Name != "eth0" {
		t.Fatal("unknown attribute changed interface")
	}
}

func TestParseLinuxNetworkLinkInvalid(t *testing.T) {
	header := func(index uint32) []byte {
		data := make(
			[]byte,
			syscall.SizeofIfInfomsg,
		)

		binary.NativeEndian.PutUint32(
			data[4:8],
			index,
		)

		return data
	}

	valid := func() []byte {
		return testLinuxNetworkLink(
			"eth0",
			2,
			unix.IFF_UP,
			1500,
		)
	}

	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty message",
		},
		{
			name: "truncated interface header",
			data: make([]byte, 15),
		},
		{
			name: "zero interface index",
			data: header(0),
		},
		{
			name: "negative interface index",
			data: header(math.MaxUint32),
		},
		{
			name: "missing interface name",
			data: header(2),
		},
		{
			name: "missing statistics",
			data: append(
				header(2),
				testLinuxNetworkAttribute(
					unix.IFLA_IFNAME,
					[]byte("eth0\x00"),
				)...,
			),
		},
		{
			name: "unterminated interface name",
			data: append(
				header(2),
				testLinuxNetworkAttribute(
					unix.IFLA_IFNAME,
					[]byte("eth0"),
				)...,
			),
		},
		{
			name: "empty interface name",
			data: append(
				header(2),
				testLinuxNetworkAttribute(
					unix.IFLA_IFNAME,
					[]byte{0},
				)...,
			),
		},
		{
			name: "invalid MTU length",
			data: append(
				header(2),
				testLinuxNetworkAttribute(
					unix.IFLA_MTU,
					[]byte{1, 2},
				)...,
			),
		},
		{
			name: "truncated statistics",
			data: append(
				header(2),
				testLinuxNetworkAttribute(
					unix.IFLA_STATS64,
					make([]byte, 63),
				)...,
			),
		},
		{
			name: "duplicate name",
			data: append(
				valid(),
				testLinuxNetworkAttribute(
					unix.IFLA_IFNAME,
					[]byte("eth0\x00"),
				)...,
			),
		},
		{
			name: "duplicate MTU",
			data: append(
				valid(),
				testLinuxNetworkAttribute(
					unix.IFLA_MTU,
					make([]byte, 4),
				)...,
			),
		},
		{
			name: "duplicate statistics",
			data: append(
				valid(),
				testLinuxNetworkAttribute(
					unix.IFLA_STATS64,
					make([]byte, 64),
				)...,
			),
		},
		{
			name: "incomplete attribute header",
			data: append(
				header(2),
				1, 2, 3,
			),
		},
		{
			name: "invalid attribute length",
			data: append(
				header(2),
				0, 0, 0, 0,
			),
		},
		{
			name: "missing attribute padding",
			data: append(
				header(2),
				testLinuxNetworkAttribute(
					unix.IFLA_IFNAME,
					[]byte("eth0\x00"),
				)[:9]...,
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iface, err := parseLinuxNetworkLink(
				tt.data,
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

			if iface != (NetworkInterfaceUsage{}) {
				t.Fatal("invalid message returned interface")
			}
		})
	}
}

func TestParseLinuxNetworkMessages(t *testing.T) {
	messages := []syscall.NetlinkMessage{
		{
			Header: syscall.NlMsghdr{
				Type: unix.RTM_NEWLINK,
			},
			Data: testLinuxNetworkLink(
				"lo",
				1,
				unix.IFF_UP|unix.IFF_LOOPBACK,
				65536,
			),
		},
		{
			Header: syscall.NlMsghdr{
				Type: unix.RTM_NEWLINK,
			},
			Data: testLinuxNetworkLink(
				"eth0",
				2,
				unix.IFF_UP,
				1500,
			),
		},
		{
			Header: syscall.NlMsghdr{
				Type: unix.NLMSG_DONE,
			},
			Data: make([]byte, 4),
		},
	}

	interfaces, err := parseLinuxNetworkMessages(
		context.Background(),
		messages,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(interfaces) != 2 {
		t.Fatal("incorrect interface count")
	}

	if interfaces[0].Name != "eth0" ||
		interfaces[1].Name != "lo" {
		t.Fatal("incorrect interface ordering")
	}
}

func TestParseLinuxNetworkMessagesInvalid(t *testing.T) {
	link := syscall.NetlinkMessage{
		Header: syscall.NlMsghdr{
			Type: unix.RTM_NEWLINK,
		},
		Data: testLinuxNetworkLink(
			"eth0",
			2,
			unix.IFF_UP,
			1500,
		),
	}

	done := syscall.NetlinkMessage{
		Header: syscall.NlMsghdr{
			Type: unix.NLMSG_DONE,
		},
	}

	interrupted := done
	interrupted.Header.Flags = unix.NLM_F_DUMP_INTR

	failed := done
	failed.Data = make([]byte, 4)

	binary.NativeEndian.PutUint32(
		failed.Data,
		math.MaxUint32,
	)

	truncated := done
	truncated.Data = []byte{0, 0}

	duplicateIndex := syscall.NetlinkMessage{
		Header: syscall.NlMsghdr{
			Type: unix.RTM_NEWLINK,
		},
		Data: testLinuxNetworkLink(
			"eth1",
			2,
			unix.IFF_UP,
			1500,
		),
	}

	tests := []struct {
		name     string
		messages []syscall.NetlinkMessage
	}{
		{
			name: "missing completion",
			messages: []syscall.NetlinkMessage{
				link,
			},
		},
		{
			name: "interrupted dump",
			messages: []syscall.NetlinkMessage{
				link, interrupted,
			},
		},
		{
			name: "failed completion",
			messages: []syscall.NetlinkMessage{
				link, failed,
			},
		},
		{
			name: "truncated completion",
			messages: []syscall.NetlinkMessage{
				link, truncated,
			},
		},
		{
			name: "duplicate interfaces",
			messages: []syscall.NetlinkMessage{
				link, link, done,
			},
		},
		{
			name: "duplicate indexes",
			messages: []syscall.NetlinkMessage{
				link, duplicateIndex, done,
			},
		},
		{
			name: "message after completion",
			messages: []syscall.NetlinkMessage{
				done, link,
			},
		},
		{
			name: "unexpected message type",
			messages: []syscall.NetlinkMessage{
				{
					Header: syscall.NlMsghdr{
						Type: unix.NLMSG_ERROR,
					},
				},
				done,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interfaces, err :=
				parseLinuxNetworkMessages(
					context.Background(),
					tt.messages,
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
				t.Fatal("invalid dump returned interfaces")
			}
		})
	}
}

func TestParseLinuxNetworkMessagesEmpty(t *testing.T) {
	interfaces, err := parseLinuxNetworkMessages(
		context.Background(),
		[]syscall.NetlinkMessage{
			{
				Header: syscall.NlMsghdr{
					Type: unix.NLMSG_DONE,
				},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if interfaces == nil || len(interfaces) != 0 {
		t.Fatal("expected initialized empty interface slice")
	}
}

func TestReadLinuxNetworkCountersWithReader(t *testing.T) {
	link := testLinuxNetworkLink(
		"eth0",
		2,
		unix.IFF_UP,
		1500,
	)

	data := testLinuxNetworkMessage(
		unix.RTM_NEWLINK,
		link,
	)

	data = append(
		data,
		testLinuxNetworkMessage(
			unix.NLMSG_DONE,
			make([]byte, 4),
		)...,
	)

	interfaces, err :=
		readLinuxNetworkCountersWithReader(
			context.Background(),
			func() ([]byte, error) {
				return data, nil
			},
		)
	if err != nil {
		t.Fatal(err)
	}

	if len(interfaces) != 1 ||
		interfaces[0].Name != "eth0" {
		t.Fatalf(
			"unexpected interfaces: %+v",
			interfaces,
		)
	}
}

func TestReadLinuxNetworkCountersMalformed(t *testing.T) {
	data := make([]byte, syscall.NLMSG_HDRLEN)

	// Invalid message length.
	binary.NativeEndian.PutUint32(
		data[:4],
		1,
	)

	interfaces, err :=
		readLinuxNetworkCountersWithReader(
			context.Background(),
			func() ([]byte, error) {
				return data, nil
			},
		)

	if !errors.Is(
		err,
		ErrInvalidNetworkStatistics,
	) {
		t.Fatalf(
			"expected malformed Netlink error, got %v",
			err,
		)
	}

	if interfaces != nil {
		t.Fatal("malformed response returned interfaces")
	}
}

func TestReadLinuxNetworkCountersFailure(t *testing.T) {
	expectedError := errors.New(
		"Netlink request failed",
	)

	interfaces, err :=
		readLinuxNetworkCountersWithReader(
			context.Background(),
			func() ([]byte, error) {
				return nil, expectedError
			},
		)

	if !errors.Is(err, expectedError) {
		t.Fatalf(
			"expected read failure, got %v",
			err,
		)
	}

	if interfaces != nil {
		t.Fatal("failed collection returned interfaces")
	}
}

func TestReadLinuxNetworkCountersCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	interfaces, err :=
		readLinuxNetworkCountersWithReader(
			ctx,
			func() ([]byte, error) {
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
		t.Fatal("canceled collection returned interfaces")
	}
}

func TestReadLinuxNetworkCountersCanceledDuringRead(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	interfaces, err :=
		readLinuxNetworkCountersWithReader(
			ctx,
			func() ([]byte, error) {
				cancel()
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
		t.Fatal("canceled collection returned interfaces")
	}
}

func TestReadLinuxNetworkCountersNative(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	interfaces, err := readLinuxNetworkCounters(ctx)
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
		"Collected %d Linux network interfaces",
		len(interfaces),
	)

	for i, iface := range interfaces {
		if i > 0 &&
			interfaces[i-1].Name >= iface.Name {
			t.Fatal("incorrect interface ordering")
		}

		if iface.ReceiveDropped == nil ||
			iface.TransmitDropped == nil {
			t.Fatal("Linux drop counters are missing")
		}

		t.Logf(
			"interface=%q index=%d rx_bytes=%d tx_bytes=%d rx_packets=%d tx_packets=%d rx_errors=%d tx_errors=%d rx_dropped=%d tx_dropped=%d",
			iface.Name,
			iface.Index,
			iface.ReceivedBytes,
			iface.TransmittedBytes,
			iface.ReceivedPackets,
			iface.TransmittedPackets,
			iface.ReceiveErrors,
			iface.TransmitErrors,
			*iface.ReceiveDropped,
			*iface.TransmitDropped,
		)
	}
}

// testLinuxNetworkStats64 builds a native-order
// cumulative network statistics fixture.
func testLinuxNetworkStats64(
	values ...uint64,
) []byte {
	data := make(
		[]byte,
		len(values)*8,
	)

	for i, value := range values {
		offset := i * 8

		binary.NativeEndian.PutUint64(
			data[offset:offset+8],
			value,
		)
	}

	return data
}

// testLinuxNetworkAttribute builds a four-byte
// aligned Netlink attribute.
func testLinuxNetworkAttribute(
	kind uint16,
	value []byte,
) []byte {
	length := 4 + len(value)
	alignedLength := (length + 3) &^ 3

	data := make([]byte, alignedLength)

	binary.NativeEndian.PutUint16(
		data[:2],
		uint16(length),
	)

	binary.NativeEndian.PutUint16(
		data[2:4],
		kind,
	)

	copy(data[4:], value)

	return data
}

// testLinuxNetworkLink constructs a synthetic
// ifinfomsg header and its attributes.
func testLinuxNetworkLink(
	name string,
	index uint32,
	flags uint32,
	mtu uint32,
) []byte {
	data := make(
		[]byte,
		syscall.SizeofIfInfomsg,
	)

	binary.NativeEndian.PutUint32(
		data[4:8],
		index,
	)

	binary.NativeEndian.PutUint32(
		data[8:12],
		flags,
	)

	data = append(
		data,
		testLinuxNetworkAttribute(
			unix.IFLA_IFNAME,
			[]byte(name+"\x00"),
		)...,
	)

	mtuData := make([]byte, 4)

	binary.NativeEndian.PutUint32(
		mtuData,
		mtu,
	)

	data = append(
		data,
		testLinuxNetworkAttribute(
			unix.IFLA_MTU,
			mtuData,
		)...,
	)

	data = append(
		data,
		testLinuxNetworkAttribute(
			unix.IFLA_STATS64,
			testLinuxNetworkStats64(
				420000,
				210000,
				524288000,
				262144000,
				2,
				1,
				3,
				4,
			),
		)...,
	)

	return data
}

// testLinuxNetworkMessage wraps a payload in a
// native Netlink message header.
func testLinuxNetworkMessage(
	kind uint16,
	payload []byte,
) []byte {
	length := syscall.NLMSG_HDRLEN +
		len(payload)

	alignedLength := (length + 3) &^ 3

	data := make([]byte, alignedLength)

	binary.NativeEndian.PutUint32(
		data[:4],
		uint32(length),
	)

	binary.NativeEndian.PutUint16(
		data[4:6],
		kind,
	)

	copy(
		data[syscall.NLMSG_HDRLEN:],
		payload,
	)

	return data
}
