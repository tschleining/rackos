package metrics

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"

	"golang.org/x/sys/unix"
)

const (
	darwinNetworkLinkGeneric   = 0
	darwinNetworkIFMIBIFData   = 2
	darwinNetworkIFDataGeneral = 1

	darwinNetworkNameSize   = 16
	darwinNetworkDataOffset = 52
	darwinNetworkData64Size = 128

	darwinNetworkRecordSize = darwinNetworkDataOffset +
		darwinNetworkData64Size
)

var errDarwinNetworkInterfaceChanged = errors.New(
	"macOS network interface changed",
)

// readDarwinNetworkCounters collects native 64-bit
// interface statistics through the IFMIB sysctl API.
func readDarwinNetworkCounters(
	ctx context.Context,
) ([]NetworkInterfaceUsage, error) {
	return readDarwinNetworkCountersWithReaders(
		ctx,
		discoverDarwinInterfaces,
		readDarwinInterfaceStatistics,
	)
}

// readDarwinInterfaceStatistics requests a native
// ifmibdata record for a specific interface index.
func readDarwinInterfaceStatistics(
	index uint32,
) ([]byte, error) {
	if index == 0 || index > math.MaxInt32 {
		return nil, fmt.Errorf(
			"%w: invalid macOS interface index %d",
			ErrInvalidNetworkStatistics,
			index,
		)
	}

	return unix.SysctlRaw(
		"net",
		unix.AF_LINK,
		darwinNetworkLinkGeneric,
		darwinNetworkIFMIBIFData,
		int(index),
		darwinNetworkIFDataGeneral,
	)
}

// readDarwinNetworkCountersWithReaders allows
// discovery and native statistics collection to
// be tested independently of hardware.
func readDarwinNetworkCountersWithReaders(
	ctx context.Context,
	discover func(context.Context) ([]NetworkInterfaceUsage, error),
	read func(uint32) ([]byte, error),
) ([]NetworkInterfaceUsage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	discovered, err := discover(ctx)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err != nil {
		return nil, err
	}

	if err := validateNetworkInterfaces(discovered); err != nil {
		return nil, err
	}

	interfaces := make(
		[]NetworkInterfaceUsage,
		0,
		len(discovered),
	)

	for _, iface := range discovered {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		data, err := read(iface.Index)

		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		if err != nil {
			if errors.Is(err, unix.ENOENT) ||
				errors.Is(err, unix.ENXIO) ||
				errors.Is(err, unix.ENODEV) {
				// The interface may have disappeared
				// since discovery.
				continue
			}

			return nil, fmt.Errorf(
				"read macOS network statistics for %q: %w",
				iface.Name,
				err,
			)
		}

		current, err := parseDarwinNetworkCounters(
			iface,
			data,
		)

		if errors.Is(err, errDarwinNetworkInterfaceChanged) {
			continue
		}

		if err != nil {
			return nil, fmt.Errorf(
				"parse macOS network statistics for %q: %w",
				iface.Name,
				err,
			)
		}

		interfaces = append(interfaces, current)
	}

	if err := validateNetworkInterfaces(interfaces); err != nil {
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

// parseDarwinNetworkCounters decodes a native
// ifmibdata record.
//
// The embedded if_data64 structure uses four-byte
// alignment in the Darwin ABI. Decode fields
// explicitly rather than casting an unaligned
// byte buffer to a Go structure.
func parseDarwinNetworkCounters(
	expected NetworkInterfaceUsage,
	data []byte,
) (NetworkInterfaceUsage, error) {
	if err := validateNetworkInterface(expected); err != nil {
		return NetworkInterfaceUsage{}, err
	}

	if len(data) != darwinNetworkRecordSize {
		return NetworkInterfaceUsage{}, fmt.Errorf(
			"%w: expected %d-byte IFMIB record, got %d bytes",
			ErrInvalidNetworkStatistics,
			darwinNetworkRecordSize,
			len(data),
		)
	}

	nameData := data[:darwinNetworkNameSize]
	end := bytes.IndexByte(nameData, 0)

	if end == 0 {
		// The kernel can return an empty record
		// for an interface that is no longer attached.
		return NetworkInterfaceUsage{},
			errDarwinNetworkInterfaceChanged
	}

	if end < 0 {
		return NetworkInterfaceUsage{}, fmt.Errorf(
			"%w: unterminated interface name",
			ErrInvalidNetworkStatistics,
		)
	}

	name := string(nameData[:end])

	if name != expected.Name {
		// The requested index no longer identifies
		// the interface discovered earlier.
		return NetworkInterfaceUsage{},
			errDarwinNetworkInterfaceChanged
	}

	flags := binary.NativeEndian.Uint32(data[20:24])

	stats := data[darwinNetworkDataOffset:]

	baudRate := binary.NativeEndian.Uint64(stats[16:24])

	receiveDropped := binary.NativeEndian.Uint64(
		stats[96:104],
	)

	iface := NetworkInterfaceUsage{
		Name:     name,
		Index:    expected.Index,
		Up:       flags&unix.IFF_UP != 0,
		Loopback: flags&unix.IFF_LOOPBACK != 0,

		MTU: binary.NativeEndian.Uint32(
			stats[8:12],
		),

		ReceivedPackets: binary.NativeEndian.Uint64(
			stats[24:32],
		),

		ReceiveErrors: binary.NativeEndian.Uint64(
			stats[32:40],
		),

		TransmittedPackets: binary.NativeEndian.Uint64(
			stats[40:48],
		),

		TransmitErrors: binary.NativeEndian.Uint64(
			stats[48:56],
		),

		ReceivedBytes: binary.NativeEndian.Uint64(
			stats[64:72],
		),

		TransmittedBytes: binary.NativeEndian.Uint64(
			stats[72:80],
		),

		ReceiveDropped: &receiveDropped,

		// A general 64-bit transmit drop counter
		// is not available through this interface.
		TransmitDropped: nil,

		// Link speed normalization will be added
		// in the next implementation step.
		LinkSpeedBitsPerSecond: normalizeDarwinLinkSpeed(
			baudRate,
			flags&unix.IFF_UP != 0,
			flags&unix.IFF_LOOPBACK != 0,
		),
	}

	return iface, nil
}
