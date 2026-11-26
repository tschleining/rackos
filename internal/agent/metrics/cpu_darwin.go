package metrics

import (
	"context"
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
)

const (
	darwinSystemLibrary = "/usr/lib/libSystem.B.dylib"

	darwinHostCPULoadInfo = 3
	darwinKernSuccess     = 0

	darwinCPUStateUser   = 0
	darwinCPUStateSystem = 1
	darwinCPUStateIdle   = 2
	darwinCPUStateNice   = 3
	darwinCPUStateMax    = 4
)

// darwinCPUReader holds the native Mach API binding
// and a reusable host port.
type darwinCPUReader struct {
	host    uint32
	library uintptr

	hostStatistics func(
		host uint32,
		flavor int32,
		info *uint32,
		count *uint32,
	) int32
}

// Initialize the native API only once.
//
// The library and host port remain available for
// the lifetime of the agent process.
var getDarwinCPUReader = sync.OnceValues(
	loadDarwinCPUReader,
)

// loadDarwinCPUReader initializes the native Mach
// functions without requiring CGO.
func loadDarwinCPUReader() (*darwinCPUReader, error) {
	library, err := purego.Dlopen(
		darwinSystemLibrary,
		purego.RTLD_NOW|purego.RTLD_LOCAL,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"load macOS system library: %w",
			err,
		)
	}

	initialized := false

	defer func() {
		if !initialized {
			_ = purego.Dlclose(library)
		}
	}()

	machHostSelfSymbol, err := purego.Dlsym(
		library,
		"mach_host_self",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve mach_host_self: %w",
			err,
		)
	}

	hostStatisticsSymbol, err := purego.Dlsym(
		library,
		"host_statistics",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve host_statistics: %w",
			err,
		)
	}

	var machHostSelf func() uint32

	reader := &darwinCPUReader{
		library: library,
	}

	purego.RegisterFunc(
		&machHostSelf,
		machHostSelfSymbol,
	)

	purego.RegisterFunc(
		&reader.hostStatistics,
		hostStatisticsSymbol,
	)

	reader.host = machHostSelf()

	if reader.host == 0 {
		return nil, fmt.Errorf(
			"mach_host_self returned an invalid host port",
		)
	}

	initialized = true

	return reader, nil
}

// readCPUCounters reads aggregate CPU activity
// from the macOS Mach host statistics interface.
func readCPUCounters(
	ctx context.Context,
) (cpuCounters, error) {
	if err := ctx.Err(); err != nil {
		return cpuCounters{}, err
	}

	reader, err := getDarwinCPUReader()
	if err != nil {
		return cpuCounters{}, err
	}

	if err := ctx.Err(); err != nil {
		return cpuCounters{}, err
	}

	var ticks [darwinCPUStateMax]uint32

	count := uint32(len(ticks))

	result := reader.hostStatistics(
		reader.host,
		darwinHostCPULoadInfo,
		&ticks[0],
		&count,
	)

	if err := ctx.Err(); err != nil {
		return cpuCounters{}, err
	}

	if result != darwinKernSuccess {
		return cpuCounters{}, fmt.Errorf(
			"host_statistics failed with Mach status %d",
			result,
		)
	}

	if count != uint32(len(ticks)) {
		return cpuCounters{}, fmt.Errorf(
			"%w: unexpected Mach CPU counter count: %d",
			ErrInvalidCPUCounters,
			count,
		)
	}

	return normalizeDarwinCPUCounters(ticks)
}

// normalizeDarwinCPUCounters converts Mach CPU
// activity into RackOS busy and idle counters.
//
// Individual counters are widened before addition
// to prevent 32-bit integer overflow.
func normalizeDarwinCPUCounters(
	ticks [darwinCPUStateMax]uint32,
) (cpuCounters, error) {
	busy := uint64(ticks[darwinCPUStateUser]) +
		uint64(ticks[darwinCPUStateSystem]) +
		uint64(ticks[darwinCPUStateNice])

	idle := uint64(ticks[darwinCPUStateIdle])

	if busy == 0 && idle == 0 {
		return cpuCounters{}, fmt.Errorf(
			"%w: all Mach CPU counters are zero",
			ErrInvalidCPUCounters,
		)
	}

	return cpuCounters{
		Busy: busy,
		Idle: idle,
	}, nil
}
