package metrics

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	linuxNetworkStats64MinSize = 64
	linuxNetworkAttrHeaderSize = 4
)

// readLinuxNetworkCounters retrieves interface metadata
// and cumulative statistics from a single Netlink dump.
func readLinuxNetworkCounters(
	ctx context.Context,
) ([]NetworkInterfaceUsage, error) {
	return readLinuxNetworkCountersWithReader(
		ctx,
		func() ([]byte, error) {
			return syscall.NetlinkRIB(
				syscall.RTM_GETLINK,
				syscall.AF_UNSPEC,
			)
		},
	)
}

// readLinuxNetworkCountersWithReader allows Netlink
// collection to be tested without native hardware.
func readLinuxNetworkCountersWithReader(
	ctx context.Context,
	read func() ([]byte, error),
) ([]NetworkInterfaceUsage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := read()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err != nil {
		return nil, fmt.Errorf(
			"read Linux network statistics: %w",
			err,
		)
	}

	messages, err := syscall.ParseNetlinkMessage(data)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: parse Netlink messages: %w",
			ErrInvalidNetworkStatistics,
			err,
		)
	}

	return parseLinuxNetworkMessages(
		ctx,
		messages,
	)
}

// parseLinuxNetworkMessages converts a complete
// Netlink link dump into network interface records.
func parseLinuxNetworkMessages(
	ctx context.Context,
	messages []syscall.NetlinkMessage,
) ([]NetworkInterfaceUsage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	interfaces := make(
		[]NetworkInterfaceUsage,
		0,
	)

	completed := false

	for _, message := range messages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if completed {
			return nil, fmt.Errorf(
				"%w: unexpected message after dump completion",
				ErrInvalidNetworkStatistics,
			)
		}

		if message.Header.Flags&unix.NLM_F_DUMP_INTR != 0 {
			return nil, fmt.Errorf(
				"%w: interrupted Netlink dump",
				ErrInvalidNetworkStatistics,
			)
		}

		switch message.Header.Type {
		case unix.NLMSG_DONE:
			// Modern kernels can include an integer
			// completion status in this message.
			if len(message.Data) > 0 {
				if len(message.Data) < 4 {
					return nil, fmt.Errorf(
						"%w: incomplete Netlink completion status",
						ErrInvalidNetworkStatistics,
					)
				}

				status := int32(
					binary.NativeEndian.Uint32(
						message.Data[:4],
					),
				)

				if status != 0 {
					return nil, fmt.Errorf(
						"%w: Netlink completion status %d",
						ErrInvalidNetworkStatistics,
						status,
					)
				}
			}

			completed = true

		case unix.RTM_NEWLINK:
			iface, err := parseLinuxNetworkLink(
				message.Data,
			)
			if err != nil {
				return nil, err
			}

			interfaces = append(
				interfaces,
				iface,
			)

		default:
			return nil, fmt.Errorf(
				"%w: unexpected Netlink message type %d",
				ErrInvalidNetworkStatistics,
				message.Header.Type,
			)
		}
	}

	if !completed {
		return nil, fmt.Errorf(
			"%w: incomplete Netlink dump",
			ErrInvalidNetworkStatistics,
		)
	}

	if err := validateNetworkInterfaces(
		interfaces,
	); err != nil {
		return nil, err
	}

	sort.Slice(interfaces, func(i, j int) bool {
		return interfaces[i].Name < interfaces[j].Name
	})

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return interfaces, nil
}

// parseLinuxNetworkLink parses the native ifinfomsg
// header and associated Netlink attributes.
func parseLinuxNetworkLink(
	data []byte,
) (NetworkInterfaceUsage, error) {
	if len(data) < syscall.SizeofIfInfomsg {
		return NetworkInterfaceUsage{}, fmt.Errorf(
			"%w: incomplete interface message",
			ErrInvalidNetworkStatistics,
		)
	}

	// ifinfomsg contains a signed 32-bit interface
	// index at offset 4 and flags at offset 8.
	index := int32(
		binary.NativeEndian.Uint32(data[4:8]),
	)

	flags := binary.NativeEndian.Uint32(
		data[8:12],
	)

	if index <= 0 {
		return NetworkInterfaceUsage{}, fmt.Errorf(
			"%w: invalid interface index %d",
			ErrInvalidNetworkStatistics,
			index,
		)
	}

	iface := NetworkInterfaceUsage{
		Index: uint32(index),

		Up: flags&unix.IFF_UP != 0,

		Loopback: flags&unix.IFF_LOOPBACK != 0,
	}

	attributes := data[syscall.SizeofIfInfomsg:]

	var (
		foundName  bool
		foundMTU   bool
		foundStats bool
	)

	for len(attributes) > 0 {
		if len(attributes) < linuxNetworkAttrHeaderSize {
			return NetworkInterfaceUsage{}, fmt.Errorf(
				"%w: incomplete Netlink attribute",
				ErrInvalidNetworkStatistics,
			)
		}

		length := int(
			binary.NativeEndian.Uint16(
				attributes[:2],
			),
		)

		kind := binary.NativeEndian.Uint16(
			attributes[2:4],
		)

		if length < linuxNetworkAttrHeaderSize ||
			length > len(attributes) {
			return NetworkInterfaceUsage{}, fmt.Errorf(
				"%w: invalid Netlink attribute length",
				ErrInvalidNetworkStatistics,
			)
		}

		// Netlink attributes are aligned to four bytes.
		alignedLength := (length + 3) &^ 3

		if alignedLength > len(attributes) {
			return NetworkInterfaceUsage{}, fmt.Errorf(
				"%w: incomplete Netlink attribute padding",
				ErrInvalidNetworkStatistics,
			)
		}

		value := attributes[linuxNetworkAttrHeaderSize:length]

		switch kind {
		case unix.IFLA_IFNAME:
			if foundName {
				return NetworkInterfaceUsage{}, fmt.Errorf(
					"%w: duplicate interface name attribute",
					ErrInvalidNetworkStatistics,
				)
			}

			end := bytes.IndexByte(value, 0)

			if end <= 0 || end != len(value)-1 {
				return NetworkInterfaceUsage{}, fmt.Errorf(
					"%w: invalid interface name attribute",
					ErrInvalidNetworkStatistics,
				)
			}

			iface.Name = string(value[:end])
			foundName = true

		case unix.IFLA_MTU:
			if foundMTU || len(value) != 4 {
				return NetworkInterfaceUsage{}, fmt.Errorf(
					"%w: invalid or duplicate MTU attribute",
					ErrInvalidNetworkStatistics,
				)
			}

			iface.MTU = binary.NativeEndian.Uint32(
				value,
			)

			foundMTU = true

		case unix.IFLA_STATS64:
			if foundStats {
				return NetworkInterfaceUsage{}, fmt.Errorf(
					"%w: duplicate 64-bit statistics attribute",
					ErrInvalidNetworkStatistics,
				)
			}

			if err := applyLinuxNetworkStats64(
				&iface,
				value,
			); err != nil {
				return NetworkInterfaceUsage{}, err
			}

			foundStats = true
		}

		// Unknown attributes are deliberately ignored.
		attributes = attributes[alignedLength:]
	}

	if !foundName {
		return NetworkInterfaceUsage{}, fmt.Errorf(
			"%w: missing interface name",
			ErrInvalidNetworkStatistics,
		)
	}

	if !foundStats {
		return NetworkInterfaceUsage{}, fmt.Errorf(
			"%w: missing 64-bit statistics for %q",
			ErrInvalidNetworkStatistics,
			iface.Name,
		)
	}

	if err := validateNetworkInterface(iface); err != nil {
		return NetworkInterfaceUsage{}, err
	}

	return iface, nil
}

// applyLinuxNetworkStats64 decodes the required
// prefix of the kernel's rtnl_link_stats64 structure.
//
// The kernel may append additional statistics in
// newer versions, so trailing fields are ignored.
func applyLinuxNetworkStats64(
	iface *NetworkInterfaceUsage,
	data []byte,
) error {
	if len(data) < linuxNetworkStats64MinSize {
		return fmt.Errorf(
			"%w: incomplete 64-bit network counters",
			ErrInvalidNetworkStatistics,
		)
	}

	iface.ReceivedPackets =
		binary.NativeEndian.Uint64(data[0:8])

	iface.TransmittedPackets =
		binary.NativeEndian.Uint64(data[8:16])

	iface.ReceivedBytes =
		binary.NativeEndian.Uint64(data[16:24])

	iface.TransmittedBytes =
		binary.NativeEndian.Uint64(data[24:32])

	iface.ReceiveErrors =
		binary.NativeEndian.Uint64(data[32:40])

	iface.TransmitErrors =
		binary.NativeEndian.Uint64(data[40:48])

	receiveDropped :=
		binary.NativeEndian.Uint64(data[48:56])

	transmitDropped :=
		binary.NativeEndian.Uint64(data[56:64])

	iface.ReceiveDropped = &receiveDropped
	iface.TransmitDropped = &transmitDropped

	return nil
}
