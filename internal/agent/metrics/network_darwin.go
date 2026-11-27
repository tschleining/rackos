package metrics

import (
	"context"
	"net"
)

// discoverDarwinInterfaces discovers native macOS
// network interfaces, including physical and
// virtual interfaces.
func discoverDarwinInterfaces(
	ctx context.Context,
) ([]NetworkInterfaceUsage, error) {
	return discoverDarwinInterfacesWithReader(
		ctx,
		net.Interfaces,
	)
}

// discoverDarwinInterfacesWithReader allows macOS
// interface discovery to be tested independently
// of native hardware.
func discoverDarwinInterfacesWithReader(
	ctx context.Context,
	read func() ([]net.Interface, error),
) ([]NetworkInterfaceUsage, error) {
	return discoverNetworkInterfacesWithReader(
		ctx,
		read,
	)
}
