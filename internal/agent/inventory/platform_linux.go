package inventory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const linuxCPUPath = "/sys/devices/system/cpu"

// collectPlatformHardware retrieves native Linux
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

	// Collect CPU model and manufacturer when available.
	cpuInfo, err := linuxRead(ctx, "/proc/cpuinfo")
	if err == nil {
		result.CPUModel = linuxField(
			cpuInfo,
			"model name",
			":",
		)

		result.CPUManufacturer = linuxField(
			cpuInfo,
			"vendor_id",
			":",
		)

		switch result.CPUManufacturer {
		case "GenuineIntel":
			result.CPUManufacturer = "Intel"
		case "AuthenticAMD":
			result.CPUManufacturer = "AMD"
		}
	}

	// Retrieve online logical CPUs from sysfs.
	online, err := linuxRead(
		ctx,
		filepath.Join(linuxCPUPath, "online"),
	)
	if err != nil {
		return hardwareDetails{}, err
	}

	cpuIDs, err := parseLinuxCPUList(online)
	if err != nil {
		return hardwareDetails{}, err
	}

	result.HostLogicalCPUs = len(cpuIDs)

	// Physical core count is optional when the kernel
	// does not expose sufficient topology information.
	result.PhysicalCPUs = linuxPhysicalCores(
		ctx,
		cpuIDs,
	)

	// Retrieve total usable system memory.
	memInfo, err := linuxRead(
		ctx,
		"/proc/meminfo",
	)
	if err != nil {
		return hardwareDetails{}, err
	}

	result.MemoryBytes, err = parseLinuxMemory(
		memInfo,
	)
	if err != nil {
		return hardwareDetails{}, err
	}

	// Hardware model is optional. DMI is common on
	// x86 systems, while device tree is common on ARM.
	result.HardwareModel = linuxOptional(
		ctx,
		"/sys/class/dmi/id/product_name",
	)

	if result.HardwareModel == "" {
		result.HardwareModel = linuxOptional(
			ctx,
			"/proc/device-tree/model",
		)
	}

	// Retrieve Linux distribution version.
	result.OSVersion = linuxOSVersion(ctx)

	// Retrieve the running kernel version.
	result.KernelVersion, err = linuxRead(
		ctx,
		"/proc/sys/kernel/osrelease",
	)
	if err != nil {
		return hardwareDetails{}, err
	}

	// Retrieve system boot time.
	stat, err := linuxRead(ctx, "/proc/stat")
	if err != nil {
		return hardwareDetails{}, err
	}

	result.BootTime, err = parseLinuxBootTime(stat)
	if err != nil {
		return hardwareDetails{}, err
	}

	if err := ctx.Err(); err != nil {
		return hardwareDetails{}, err
	}

	return result, nil
}

func linuxRead(
	ctx context.Context,
	path string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf(
			"read %s: %w",
			path,
			err,
		)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	value := strings.Trim(
		string(data),
		"\x00 \t\r\n",
	)

	if value == "" {
		return "", fmt.Errorf(
			"empty system information: %s",
			path,
		)
	}

	return value, nil
}

func linuxOptional(
	ctx context.Context,
	path string,
) string {
	value, err := linuxRead(ctx, path)
	if err != nil {
		return ""
	}

	return value
}

func linuxField(
	content string,
	key string,
	separator string,
) string {
	for _, line := range strings.Split(content, "\n") {
		name, value, ok := strings.Cut(
			line,
			separator,
		)

		if !ok {
			continue
		}

		if strings.TrimSpace(name) == key {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

// parseLinuxCPUList parses Linux CPU lists such as
// "0-3", "0,2,4,6", or "0-3,8-11".
func parseLinuxCPUList(
	value string,
) ([]int, error) {
	const maxCPUID = 1 << 20

	value = strings.TrimSpace(value)

	if value == "" {
		return nil, fmt.Errorf("empty CPU list")
	}

	var result []int
	seen := make(map[int]bool)

	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)

		startText, endText, ranged := strings.Cut(
			entry,
			"-",
		)

		start, err := strconv.Atoi(startText)
		if err != nil ||
			start < 0 ||
			start >= maxCPUID {
			return nil, fmt.Errorf(
				"invalid CPU ID: %q",
				entry,
			)
		}

		end := start

		if ranged {
			end, err = strconv.Atoi(endText)

			if err != nil ||
				end < start ||
				end >= maxCPUID {
				return nil, fmt.Errorf(
					"invalid CPU range: %q",
					entry,
				)
			}
		}

		for id := start; id <= end; id++ {
			if seen[id] {
				return nil, fmt.Errorf(
					"duplicate CPU ID: %d",
					id,
				)
			}

			seen[id] = true
			result = append(result, id)
		}
	}

	return result, nil
}

// linuxPhysicalCores counts distinct core sibling
// groups among online logical CPUs.
func linuxPhysicalCores(
	ctx context.Context,
	cpuIDs []int,
) int {
	groups := make(map[string]struct{})

	for _, id := range cpuIDs {
		if ctx.Err() != nil {
			return 0
		}

		topologyPath := filepath.Join(
			linuxCPUPath,
			fmt.Sprintf("cpu%d", id),
			"topology",
		)

		siblings := linuxOptional(
			ctx,
			filepath.Join(
				topologyPath,
				"core_cpus_list",
			),
		)

		if siblings == "" {
			siblings = linuxOptional(
				ctx,
				filepath.Join(
					topologyPath,
					"thread_siblings_list",
				),
			)
		}

		if siblings == "" {
			return 0
		}

		groups[siblings] = struct{}{}
	}

	return len(groups)
}

func parseLinuxMemory(
	content string,
) (uint64, error) {
	value := linuxField(
		content,
		"MemTotal",
		":",
	)

	fields := strings.Fields(value)

	if len(fields) != 2 || fields[1] != "kB" {
		return 0, fmt.Errorf(
			"invalid MemTotal value",
		)
	}

	kib, err := strconv.ParseUint(
		fields[0],
		10,
		64,
	)

	if err != nil ||
		kib == 0 ||
		kib > ^uint64(0)/1024 {
		return 0, fmt.Errorf(
			"invalid system memory size",
		)
	}

	return kib * 1024, nil
}

func linuxOSVersion(
	ctx context.Context,
) string {
	content, err := linuxRead(
		ctx,
		"/etc/os-release",
	)

	if err != nil {
		content, err = linuxRead(
			ctx,
			"/usr/lib/os-release",
		)
	}

	if err != nil {
		return ""
	}

	version := linuxField(
		content,
		"VERSION_ID",
		"=",
	)

	return strings.Trim(version, `"'`)
}

func parseLinuxBootTime(
	content string,
) (time.Time, error) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)

		if len(fields) != 2 ||
			fields[0] != "btime" {
			continue
		}

		seconds, err := strconv.ParseInt(
			fields[1],
			10,
			64,
		)

		if err != nil || seconds <= 0 {
			return time.Time{}, fmt.Errorf(
				"invalid Linux boot time",
			)
		}

		return time.Unix(seconds, 0).UTC(), nil
	}

	return time.Time{}, fmt.Errorf(
		"Linux boot time not found",
	)
}
