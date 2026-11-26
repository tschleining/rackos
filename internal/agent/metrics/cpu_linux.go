package metrics

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

const linuxCPUStatPath = "/proc/stat"

// readCPUCounters reads aggregate system CPU counters.
//
// The Linux-specific filename ensures this implementation
// is selected automatically when building for Linux.
func readCPUCounters(
	ctx context.Context,
) (cpuCounters, error) {
	if err := ctx.Err(); err != nil {
		return cpuCounters{}, err
	}

	file, err := os.Open(linuxCPUStatPath)
	if err != nil {
		return cpuCounters{}, fmt.Errorf(
			"open CPU statistics: %w",
			err,
		)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// The aggregate CPU counters appear on the first
	// line of /proc/stat. Individual CPU counters
	// are not needed for aggregate utilization.
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return cpuCounters{}, fmt.Errorf(
				"read CPU statistics: %w",
				err,
			)
		}

		return cpuCounters{}, fmt.Errorf(
			"%w: CPU statistics are empty",
			ErrInvalidCPUCounters,
		)
	}

	if err := ctx.Err(); err != nil {
		return cpuCounters{}, err
	}

	return parseLinuxCPUCounters(scanner.Text())
}

// parseLinuxCPUCounters parses the aggregate CPU line
// from /proc/stat.
//
// Expected field order:
//
// cpu user nice system idle iowait irq softirq
//
//	steal guest guest_nice
//
// The first four counters are required. Missing
// trailing counters are treated as zero.
func parseLinuxCPUCounters(
	line string,
) (cpuCounters, error) {
	fields := strings.Fields(line)

	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuCounters{}, fmt.Errorf(
			"%w: invalid aggregate CPU statistics",
			ErrInvalidCPUCounters,
		)
	}

	var counters [10]uint64

	for index, field := range fields[1:] {
		// Ignore additional fields introduced by
		// future kernel versions.
		if index >= len(counters) {
			break
		}

		value, err := strconv.ParseUint(
			field,
			10,
			64,
		)
		if err != nil {
			return cpuCounters{}, fmt.Errorf(
				"%w: invalid CPU counter at index %d",
				ErrInvalidCPUCounters,
				index,
			)
		}

		counters[index] = value
	}

	user := counters[0]
	nice := counters[1]
	system := counters[2]
	idle := counters[3]
	ioWait := counters[4]
	irq := counters[5]
	softIRQ := counters[6]
	steal := counters[7]

	// Guest and guest_nice are already accounted
	// for in user and nice respectively.
	//
	// Adding them again would double-count
	// virtual CPU activity.

	busyTotal, err := sumLinuxCPUCounters(
		user,
		nice,
		system,
		irq,
		softIRQ,
		steal,
	)
	if err != nil {
		return cpuCounters{}, err
	}

	idleTotal, err := sumLinuxCPUCounters(
		idle,
		ioWait,
	)
	if err != nil {
		return cpuCounters{}, err
	}

	if busyTotal == 0 && idleTotal == 0 {
		return cpuCounters{}, fmt.Errorf(
			"%w: all CPU counters are zero",
			ErrInvalidCPUCounters,
		)
	}

	return cpuCounters{
		Busy: busyTotal,
		Idle: idleTotal,
	}, nil
}

// sumLinuxCPUCounters adds cumulative counters while
// protecting against unsigned integer overflow.
func sumLinuxCPUCounters(
	values ...uint64,
) (uint64, error) {
	var total uint64

	for _, value := range values {
		if value > math.MaxUint64-total {
			return 0, fmt.Errorf(
				"%w: CPU counter overflow",
				ErrInvalidCPUCounters,
			)
		}

		total += value
	}

	return total, nil
}
