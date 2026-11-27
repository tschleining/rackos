package metrics

import (
	"context"
	"net"
)

// discoverLinuxInterfaces discovers network interfaces
// visible within the current Linux network namespace.
func discoverLinuxInterfaces(
	ctx context.Context,
) ([]NetworkInterfaceUsage, error) {
	return discoverLinuxInterfacesWithReader(
		ctx,
		net.Interfaces,
	)
}

// discoverLinuxInterfacesWithReader allows interface
// discovery to be tested without native hardware.
func discoverLinuxInterfacesWithReader(
	ctx context.Context,
	read func() ([]net.Interface, error),
) ([]NetworkInterfaceUsage, error) {
	return discoverNetworkInterfacesWithReader(
		ctx,
		read,
	)
}

// normalizeLinuxInterface converts native Linux
// interface metadata into the portable model.
func normalizeLinuxInterface(
	native net.Interface,
) (NetworkInterfaceUsage, error) {
	return normalizeNetworkInterface(native)
}
