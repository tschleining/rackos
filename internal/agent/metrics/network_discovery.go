package metrics

import (
	"context"
	"fmt"
	"math"
	"net"
	"sort"
)

// discoverNetworkInterfacesWithReader discovers and
// normalizes native network interface metadata.
//
// The reader is injectable to support deterministic
// tests without accessing native network interfaces.
func discoverNetworkInterfacesWithReader(
	ctx context.Context,
	read func() ([]net.Interface, error),
) ([]NetworkInterfaceUsage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	nativeInterfaces, err := read()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err != nil {
		return nil, fmt.Errorf(
			"discover network interfaces: %w",
			err,
		)
	}

	interfaces := make(
		[]NetworkInterfaceUsage,
		0,
		len(nativeInterfaces),
	)

	for _, native := range nativeInterfaces {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		iface, err := normalizeNetworkInterface(native)
		if err != nil {
			return nil, err
		}

		interfaces = append(interfaces, iface)
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

// normalizeNetworkInterface converts standard
// library interface metadata into the portable
// RackOS network model.
func normalizeNetworkInterface(
	native net.Interface,
) (NetworkInterfaceUsage, error) {
	if native.Index <= 0 ||
		uint64(native.Index) > math.MaxUint32 {
		return NetworkInterfaceUsage{}, fmt.Errorf(
			"%w: interface %q has invalid index %d",
			ErrInvalidNetworkStatistics,
			native.Name,
			native.Index,
		)
	}

	if native.MTU < 0 ||
		uint64(native.MTU) > math.MaxUint32 {
		return NetworkInterfaceUsage{}, fmt.Errorf(
			"%w: interface %q has invalid MTU %d",
			ErrInvalidNetworkStatistics,
			native.Name,
			native.MTU,
		)
	}

	iface := NetworkInterfaceUsage{
		Name:     native.Name,
		Index:    uint32(native.Index),
		Up:       native.Flags&net.FlagUp != 0,
		Loopback: native.Flags&net.FlagLoopback != 0,
		MTU:      uint32(native.MTU),
	}

	if err := validateNetworkInterface(iface); err != nil {
		return NetworkInterfaceUsage{}, err
	}

	return iface, nil
}
