//go:build !linux && !darwin

package metrics

import (
	"context"
	"fmt"
	"runtime"
)

// readCPUCounters returns an explicit error on
// platforms without a native CPU collector.
func readCPUCounters(
	ctx context.Context,
) (cpuCounters, error) {
	if err := ctx.Err(); err != nil {
		return cpuCounters{}, err
	}

	return cpuCounters{}, fmt.Errorf(
		"%w: CPU collection on %s",
		ErrUnsupportedPlatform,
		runtime.GOOS,
	)
}

// readMemoryUsage returns an explicit error on
// platforms without a native memory collector.
func readMemoryUsage(
	ctx context.Context,
) (MemoryUsage, error) {
	if err := ctx.Err(); err != nil {
		return MemoryUsage{}, err
	}

	return MemoryUsage{}, fmt.Errorf(
		"%w: memory collection on %s",
		ErrUnsupportedPlatform,
		runtime.GOOS,
	)
}

func readFilesystemUsage(
	ctx context.Context,
) ([]FilesystemUsage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return nil, fmt.Errorf(
		"%w: filesystem capacity collection",
		ErrUnsupportedPlatform,
	)
}

func readDiskUsage(
	ctx context.Context,
) ([]DiskUsage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return nil, fmt.Errorf(
		"%w: disk activity collection",
		ErrUnsupportedPlatform,
	)
}
