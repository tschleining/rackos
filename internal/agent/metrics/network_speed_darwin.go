package metrics

import (
	"context"
	"math"
)

// readNetworkUsage collects macOS network interface
// metadata, cumulative counters, and optional link
// speed measurements.
//
// All measurements are obtained through the existing
// native IFMIB statistics collector.
func readNetworkUsage(
	ctx context.Context,
) ([]NetworkInterfaceUsage, error) {
	return readDarwinNetworkCounters(ctx)
}

// normalizeDarwinLinkSpeed converts a native interface
// baud rate into an optional RackOS link speed.
//
// Darwin already reports this value in bits per
// second, so no unit conversion is required.
func normalizeDarwinLinkSpeed(
	baudRate uint64,
	up bool,
	loopback bool,
) *uint64 {
	if !up || loopback {
		return nil
	}

	if baudRate == 0 || baudRate == math.MaxUint64 {
		return nil
	}

	return &baudRate
}
