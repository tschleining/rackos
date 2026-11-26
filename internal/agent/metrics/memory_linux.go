package metrics

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

const linuxMemInfoPath = "/proc/meminfo"

// readMemoryUsage reads system-wide memory information
// from the Linux proc filesystem.
func readMemoryUsage(
	ctx context.Context,
) (MemoryUsage, error) {
	if err := ctx.Err(); err != nil {
		return MemoryUsage{}, err
	}

	file, err := os.Open(linuxMemInfoPath)
	if err != nil {
		return MemoryUsage{}, fmt.Errorf(
			"open memory statistics: %w",
			err,
		)
	}
	defer file.Close()

	memory, err := parseLinuxMemoryUsage(file)
	if err != nil {
		return MemoryUsage{}, err
	}

	if err := ctx.Err(); err != nil {
		return MemoryUsage{}, err
	}

	return memory, nil
}

// parseLinuxMemoryUsage extracts total and available
// memory from /proc/meminfo.
//
// Both fields are required. Values reported in kB
// are converted to bytes using a multiplier of 1024.
func parseLinuxMemoryUsage(
	reader io.Reader,
) (MemoryUsage, error) {
	scanner := bufio.NewScanner(reader)

	var totalBytes uint64
	var availableBytes uint64

	var foundTotal bool
	var foundAvailable bool

	for scanner.Scan() {
		key, value, found := strings.Cut(
			scanner.Text(),
			":",
		)

		if !found {
			continue
		}

		key = strings.TrimSpace(key)

		switch key {
		case "MemTotal":
			if foundTotal {
				return MemoryUsage{}, fmt.Errorf(
					"%w: duplicate MemTotal",
					ErrInvalidMemory,
				)
			}

			parsed, err := parseLinuxMemoryBytes(value)
			if err != nil {
				return MemoryUsage{}, fmt.Errorf(
					"MemTotal: %w",
					err,
				)
			}

			totalBytes = parsed
			foundTotal = true

		case "MemAvailable":
			if foundAvailable {
				return MemoryUsage{}, fmt.Errorf(
					"%w: duplicate MemAvailable",
					ErrInvalidMemory,
				)
			}

			parsed, err := parseLinuxMemoryBytes(value)
			if err != nil {
				return MemoryUsage{}, fmt.Errorf(
					"MemAvailable: %w",
					err,
				)
			}

			availableBytes = parsed
			foundAvailable = true
		}
	}

	if err := scanner.Err(); err != nil {
		return MemoryUsage{}, fmt.Errorf(
			"read memory statistics: %w",
			err,
		)
	}

	if !foundTotal {
		return MemoryUsage{}, fmt.Errorf(
			"%w: missing MemTotal",
			ErrInvalidMemory,
		)
	}

	if !foundAvailable {
		return MemoryUsage{}, fmt.Errorf(
			"%w: missing MemAvailable",
			ErrInvalidMemory,
		)
	}

	return calculateMemoryUsage(
		totalBytes,
		availableBytes,
	)
}

// parseLinuxMemoryBytes converts a /proc/meminfo
// value from kB to bytes.
//
// Example: "16384 kB" becomes 16777216 bytes.
func parseLinuxMemoryBytes(
	value string,
) (uint64, error) {
	fields := strings.Fields(value)

	if len(fields) != 2 || fields[1] != "kB" {
		return 0, fmt.Errorf(
			"%w: invalid memory value or unit",
			ErrInvalidMemory,
		)
	}

	kilobytes, err := strconv.ParseUint(
		fields[0],
		10,
		64,
	)
	if err != nil {
		return 0, fmt.Errorf(
			"%w: invalid memory counter",
			ErrInvalidMemory,
		)
	}

	const bytesPerKilobyte = uint64(1024)

	if kilobytes > math.MaxUint64/bytesPerKilobyte {
		return 0, fmt.Errorf(
			"%w: memory counter overflow",
			ErrInvalidMemory,
		)
	}

	return kilobytes * bytesPerKilobyte, nil
}
