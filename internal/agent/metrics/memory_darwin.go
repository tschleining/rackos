package metrics

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

const (
	darwinHostVMInfo64 = 4

	// The current vm_statistics64 structure occupies
	// 160 bytes, or 40 Mach integer-sized words.
	darwinVMInfo64Count = 40

	// The original revision occupies 96 bytes.
	darwinVMInfo64MinimumCount = 24
)

// darwinVMStatistics represents the native
// vm_statistics64 structure.
//
// Only the first four fields are currently needed.
// The remaining storage accommodates the other
// native fields and preserves 64-bit alignment.
type darwinVMStatistics struct {
	FreeCount     uint32
	ActiveCount   uint32
	InactiveCount uint32
	WireCount     uint32

	Reserved [18]uint64
}

// darwinMemoryReader holds reusable native bindings
// and static system memory information.
type darwinMemoryReader struct {
	host     uint32
	total    uint64
	pageSize uint64

	hostStatistics64 func(
		host uint32,
		flavor int32,
		info *darwinVMStatistics,
		count *uint32,
	) int32
}

var getDarwinMemoryReader = sync.OnceValues(
	loadDarwinMemoryReader,
)

// loadDarwinMemoryReader initializes the native
// memory statistics interface.
func loadDarwinMemoryReader() (*darwinMemoryReader, error) {
	cpuReader, err := getDarwinCPUReader()
	if err != nil {
		return nil, err
	}

	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return nil, fmt.Errorf(
			"read physical memory capacity: %w",
			err,
		)
	}

	if total == 0 {
		return nil, fmt.Errorf(
			"%w: physical memory capacity is zero",
			ErrInvalidMemory,
		)
	}

	statisticsSymbol, err := purego.Dlsym(
		cpuReader.library,
		"host_statistics64",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve host_statistics64: %w",
			err,
		)
	}

	pageSizeSymbol, err := purego.Dlsym(
		cpuReader.library,
		"host_page_size",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve host_page_size: %w",
			err,
		)
	}

	var hostPageSize func(
		host uint32,
		size *uint64,
	) int32

	purego.RegisterFunc(
		&hostPageSize,
		pageSizeSymbol,
	)

	var pageSize uint64

	result := hostPageSize(
		cpuReader.host,
		&pageSize,
	)

	if result != darwinKernSuccess {
		return nil, fmt.Errorf(
			"host_page_size failed with Mach status %d",
			result,
		)
	}

	if pageSize == 0 ||
		pageSize > total ||
		pageSize&(pageSize-1) != 0 {
		return nil, fmt.Errorf(
			"%w: invalid kernel page size",
			ErrInvalidMemory,
		)
	}

	reader := &darwinMemoryReader{
		host:     cpuReader.host,
		total:    total,
		pageSize: pageSize,
	}

	purego.RegisterFunc(
		&reader.hostStatistics64,
		statisticsSymbol,
	)

	return reader, nil
}

// readMemoryUsage collects system-wide memory
// statistics using the native Mach interface.
func readMemoryUsage(
	ctx context.Context,
) (MemoryUsage, error) {
	if err := ctx.Err(); err != nil {
		return MemoryUsage{}, err
	}

	reader, err := getDarwinMemoryReader()
	if err != nil {
		return MemoryUsage{}, err
	}

	if err := ctx.Err(); err != nil {
		return MemoryUsage{}, err
	}

	var statistics darwinVMStatistics

	count := uint32(darwinVMInfo64Count)

	result := reader.hostStatistics64(
		reader.host,
		darwinHostVMInfo64,
		&statistics,
		&count,
	)

	if err := ctx.Err(); err != nil {
		return MemoryUsage{}, err
	}

	if result != darwinKernSuccess {
		return MemoryUsage{}, fmt.Errorf(
			"host_statistics64 failed with Mach status %d",
			result,
		)
	}

	if count < darwinVMInfo64MinimumCount ||
		count > darwinVMInfo64Count {
		return MemoryUsage{}, fmt.Errorf(
			"%w: unexpected Mach memory counter count: %d",
			ErrInvalidMemory,
			count,
		)
	}

	return normalizeDarwinMemoryUsage(
		reader.total,
		reader.pageSize,
		statistics.FreeCount,
		statistics.InactiveCount,
	)
}

// normalizeDarwinMemoryUsage estimates available
// physical memory from free and inactive pages.
//
// Speculative pages are already included in
// FreeCount and must not be added separately.
func normalizeDarwinMemoryUsage(
	totalBytes uint64,
	pageSize uint64,
	freePages uint32,
	inactivePages uint32,
) (MemoryUsage, error) {
	if totalBytes == 0 ||
		pageSize == 0 ||
		pageSize > totalBytes ||
		pageSize&(pageSize-1) != 0 {
		return MemoryUsage{}, fmt.Errorf(
			"%w: invalid memory capacity or page size",
			ErrInvalidMemory,
		)
	}

	availablePages := uint64(freePages) +
		uint64(inactivePages)

	if availablePages > math.MaxUint64/pageSize {
		return MemoryUsage{}, fmt.Errorf(
			"%w: available memory overflow",
			ErrInvalidMemory,
		)
	}

	availableBytes := availablePages * pageSize

	return calculateMemoryUsage(
		totalBytes,
		availableBytes,
	)
}
