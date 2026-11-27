package metrics

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	linuxNetworkSysfsPath = "/sys/class/net"
	linuxBitsPerMegabit   = uint64(1000000)
)

// readNetworkUsage collects Linux network interface
// metadata, cumulative counters, and optional link
// speed measurements.
func readNetworkUsage(
	ctx context.Context,
) ([]NetworkInterfaceUsage, error) {
	return collectLinuxNetworkUsage(
		ctx,
		readLinuxNetworkCounters,
		os.ReadFile,
	)
}

// collectLinuxNetworkUsage combines required
// network counters with optional link speeds.
func collectLinuxNetworkUsage(
	ctx context.Context,
	readCounters func(context.Context) ([]NetworkInterfaceUsage, error),
	readFile func(string) ([]byte, error),
) ([]NetworkInterfaceUsage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	interfaces, err := readCounters(ctx)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err != nil {
		return nil, err
	}

	if err := validateNetworkInterfaces(interfaces); err != nil {
		return nil, err
	}

	// Preserve the original counter records and
	// ensure empty collections serialize as arrays.
	result := make(
		[]NetworkInterfaceUsage,
		len(interfaces),
	)

	copy(result, interfaces)

	for i := range result {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		speed, err := readLinuxLinkSpeed(
			ctx,
			result[i],
			readFile,
		)
		if err != nil {
			return nil, err
		}

		result[i].LinkSpeedBitsPerSecond = speed
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// readLinuxLinkSpeed retrieves optional link speed
// information for a previously discovered interface.
//
// Unavailable speed measurements are represented
// by nil rather than zero.
func readLinuxLinkSpeed(
	ctx context.Context,
	iface NetworkInterfaceUsage,
	readFile func(string) ([]byte, error),
) (*uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if !iface.Up || iface.Loopback {
		return nil, nil
	}

	// Interface names must be single path components
	// before they can be used in sysfs paths.
	if iface.Name == "" ||
		iface.Name == "." ||
		iface.Name == ".." ||
		filepath.Base(iface.Name) != iface.Name ||
		strings.ContainsRune(iface.Name, 0) ||
		iface.Index == 0 {
		return nil, nil
	}

	directory := filepath.Join(
		linuxNetworkSysfsPath,
		iface.Name,
	)

	indexPath := filepath.Join(directory, "ifindex")
	speedPath := filepath.Join(directory, "speed")

	// Verify that sysfs identifies the same interface
	// as the original Netlink counter record.
	before, err := readFile(indexPath)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err != nil ||
		!linuxNetworkIndexMatches(before, iface.Index) {
		return nil, nil
	}

	data, err := readFile(speedPath)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err != nil {
		// Unsupported, inaccessible, or disappeared
		// speed attributes are not fatal.
		return nil, nil
	}

	speed := parseLinuxLinkSpeed(data)
	if speed == nil {
		return nil, nil
	}

	// Recheck identity in case the interface changed
	// while the speed attribute was being read.
	after, err := readFile(indexPath)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err != nil ||
		!linuxNetworkIndexMatches(after, iface.Index) {
		return nil, nil
	}

	return speed, nil
}

// linuxNetworkIndexMatches verifies the decimal
// interface index reported by sysfs.
func linuxNetworkIndexMatches(
	data []byte,
	expected uint32,
) bool {
	index, err := strconv.ParseUint(
		strings.TrimSpace(string(data)),
		10,
		32,
	)

	return err == nil &&
		index != 0 &&
		uint32(index) == expected
}

// parseLinuxLinkSpeed converts a Linux link speed
// from megabits per second into bits per second.
//
// Unknown, zero, and malformed speeds are treated
// as unavailable measurements.
func parseLinuxLinkSpeed(data []byte) *uint64 {
	value := strings.TrimSpace(string(data))

	megabits, err := strconv.ParseUint(
		value,
		10,
		32,
	)

	if err != nil ||
		megabits == 0 ||
		megabits > math.MaxInt32 {
		return nil
	}

	bitsPerSecond := megabits * linuxBitsPerMegabit

	return &bitsPerSecond
}
