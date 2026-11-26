package inventory

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var darwinBootTimePattern = regexp.MustCompile(
	`sec\s*=\s*(\d+),\s*usec\s*=\s*(\d+)`,
)

// collectPlatformHardware retrieves native macOS
// hardware and operating system information.
func collectPlatformHardware(
	ctx context.Context,
) (hardwareDetails, error) {
	ctx, cancel := context.WithTimeout(
		ctx,
		5*time.Second,
	)
	defer cancel()

	var result hardwareDetails
	var err error

	// CPU branding may not be available on every
	// hardware or virtualized configuration.
	result.CPUModel, _ = darwinSysctl(
		ctx,
		"machdep.cpu.brand_string",
	)

	result.CPUManufacturer, _ = darwinSysctl(
		ctx,
		"machdep.cpu.vendor",
	)

	if result.CPUManufacturer == "" &&
		strings.HasPrefix(result.CPUModel, "Apple ") {
		result.CPUManufacturer = "Apple"
	}

	result.PhysicalCPUs, err = darwinSysctlInt(
		ctx,
		"hw.physicalcpu",
	)
	if err != nil {
		return hardwareDetails{}, err
	}

	result.HostLogicalCPUs, err = darwinSysctlInt(
		ctx,
		"hw.logicalcpu",
	)
	if err != nil {
		return hardwareDetails{}, err
	}

	if result.PhysicalCPUs > result.HostLogicalCPUs {
		return hardwareDetails{}, fmt.Errorf(
			"invalid CPU topology",
		)
	}

	memory, err := darwinSysctl(
		ctx,
		"hw.memsize",
	)
	if err != nil {
		return hardwareDetails{}, err
	}

	result.MemoryBytes, err = strconv.ParseUint(
		memory,
		10,
		64,
	)
	if err != nil || result.MemoryBytes == 0 {
		return hardwareDetails{}, fmt.Errorf(
			"invalid physical memory size: %q",
			memory,
		)
	}

	result.HardwareModel, err = darwinSysctl(
		ctx,
		"hw.model",
	)
	if err != nil {
		return hardwareDetails{}, err
	}

	result.KernelVersion, err = darwinSysctl(
		ctx,
		"kern.osrelease",
	)
	if err != nil {
		return hardwareDetails{}, err
	}

	result.OSVersion, err = darwinCommand(
		ctx,
		"/usr/bin/sw_vers",
		"-productVersion",
	)
	if err != nil {
		return hardwareDetails{}, err
	}

	bootTime, err := darwinSysctl(
		ctx,
		"kern.boottime",
	)
	if err != nil {
		return hardwareDetails{}, err
	}

	result.BootTime, err = parseDarwinBootTime(
		bootTime,
	)
	if err != nil {
		return hardwareDetails{}, err
	}

	if err := ctx.Err(); err != nil {
		return hardwareDetails{}, err
	}

	return result, nil
}

func darwinSysctl(
	ctx context.Context,
	key string,
) (string, error) {
	return darwinCommand(
		ctx,
		"/usr/sbin/sysctl",
		"-n",
		key,
	)
}

func darwinSysctlInt(
	ctx context.Context,
	key string,
) (int, error) {
	value, err := darwinSysctl(ctx, key)
	if err != nil {
		return 0, err
	}

	number, err := strconv.Atoi(value)

	if err != nil || number < 1 {
		return 0, fmt.Errorf(
			"invalid integer for %s: %q",
			key,
			value,
		)
	}

	return number, nil
}

func darwinCommand(
	ctx context.Context,
	path string,
	args ...string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	command := exec.CommandContext(
		ctx,
		path,
		args...,
	)

	output, err := command.Output()

	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	if err != nil {
		return "", fmt.Errorf(
			"execute %s %v: %w",
			path,
			args,
			err,
		)
	}

	value := strings.TrimSpace(string(output))

	if value == "" {
		return "", fmt.Errorf(
			"empty output from %s %v",
			path,
			args,
		)
	}

	return value, nil
}

func parseDarwinBootTime(
	value string,
) (time.Time, error) {
	matches := darwinBootTimePattern.FindStringSubmatch(
		value,
	)

	if len(matches) != 3 {
		return time.Time{}, fmt.Errorf(
			"invalid macOS boot time format",
		)
	}

	seconds, err := strconv.ParseInt(
		matches[1],
		10,
		64,
	)
	if err != nil || seconds <= 0 {
		return time.Time{}, fmt.Errorf(
			"invalid boot time seconds",
		)
	}

	microseconds, err := strconv.ParseInt(
		matches[2],
		10,
		64,
	)
	if err != nil ||
		microseconds < 0 ||
		microseconds >= 1000000 {
		return time.Time{}, fmt.Errorf(
			"invalid boot time microseconds",
		)
	}

	return time.Unix(
		seconds,
		microseconds*1000,
	).UTC(), nil
}
