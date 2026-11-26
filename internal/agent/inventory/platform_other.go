//go:build !darwin && !linux

package inventory

import "context"

// collectPlatformHardware provides a portable fallback
// for platforms without a native hardware collector.
func collectPlatformHardware(
	ctx context.Context,
) (hardwareDetails, error) {
	if err := ctx.Err(); err != nil {
		return hardwareDetails{}, err
	}

	return hardwareDetails{}, nil
}
