package pki

import (
	"crypto/tls"
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"strconv"
)

const ManagementDirectory = "management-pki"

// ManagementCAPath returns the public management
// certificate authority path.
//
// It never returns the private signing key path.
func ManagementCAPath(dataDir string) string {
	return filepath.Join(
		dataDir,
		ManagementDirectory,
		CAFilename,
	)
}

// LoadOrCreateManagement initializes an independent
// certificate authority for the management HTTPS
// service.
//
// Management certificates are restricted to the
// explicitly configured loopback IP address.
func LoadOrCreateManagement(
	dataDir string,
	installationID string,
	address string,
) (*tls.Config, error) {
	if installationID == "" {
		return nil, errors.New(
			"installation identity is required",
		)
	}

	ip, err := managementBindIP(address)
	if err != nil {
		return nil, err
	}

	names := serverNames{
		ips: []net.IP{
			net.IP(ip.AsSlice()),
		},
	}

	return loadOrCreate(
		dataDir,
		ManagementDirectory,
		"RackOS Management CA "+installationID,
		"RackOS Management HTTPS",
		names,
	)
}

// managementBindIP accepts only canonical loopback
// addresses with a valid numeric port.
func managementBindIP(
	address string,
) (netip.Addr, error) {
	invalid := errors.New(
		"management TLS requires a canonical loopback IP address and numeric port",
	)

	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return netip.Addr{}, invalid
	}

	ip, err := netip.ParseAddr(host)
	if err != nil ||
		!ip.IsLoopback() ||
		ip.Is4In6() ||
		ip.Zone() != "" ||
		ip.String() != host {
		return netip.Addr{}, invalid
	}

	port, err := strconv.ParseUint(
		portText,
		10,
		16,
	)
	if err != nil ||
		strconv.FormatUint(port, 10) != portText {
		return netip.Addr{}, invalid
	}

	if net.JoinHostPort(host, portText) != address {
		return netip.Addr{}, invalid
	}

	return ip, nil
}
