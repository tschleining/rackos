package metrics

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	linuxDiskStatsPath = "/proc/diskstats"
	linuxSysRoot       = "/sys"

	linuxDiskSectorBytes = uint64(512)
)

var errInvalidLinuxDiskStats = errors.New(
	"invalid Linux disk statistics",
)

// linuxDiskDevice identifies a block device within
// the current Linux system.
type linuxDiskDevice struct {
	Major uint32
	Minor uint32
}

// readDiskUsage collects cumulative read and write
// statistics for eligible Linux block devices.
func readDiskUsage(
	ctx context.Context,
) ([]DiskUsage, error) {
	devices, err := discoverLinuxDisks(
		ctx,
		linuxSysRoot,
	)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	file, err := os.Open(linuxDiskStatsPath)
	if err != nil {
		return nil, fmt.Errorf(
			"open disk statistics: %w",
			err,
		)
	}
	defer file.Close()

	return parseLinuxDiskStats(
		ctx,
		file,
		devices,
	)
}

// discoverLinuxDisks discovers whole block devices
// and excludes virtual storage layers.
//
// The sysRoot argument allows discovery to be tested
// against a synthetic sysfs directory.
func discoverLinuxDisks(
	ctx context.Context,
	sysRoot string,
) (map[string]linuxDiskDevice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	blockPath := filepath.Join(
		sysRoot,
		"block",
	)

	entries, err := os.ReadDir(blockPath)
	if err != nil {
		return nil, fmt.Errorf(
			"discover block devices: %w",
			err,
		)
	}

	devices := make(
		map[string]linuxDiskDevice,
		len(entries),
	)

	virtualPath := filepath.Join(
		sysRoot,
		"devices",
		"virtual",
	)

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		name := entry.Name()

		devicePath, err := filepath.EvalSymlinks(
			filepath.Join(blockPath, name),
		)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Device disappeared during discovery.
				continue
			}

			return nil, fmt.Errorf(
				"resolve block device %q: %w",
				name,
				err,
			)
		}

		// Exclude software-defined virtual block
		// devices such as loop, RAM, device mapper,
		// and software RAID devices.
		if devicePath == virtualPath ||
			strings.HasPrefix(
				devicePath,
				virtualPath+string(os.PathSeparator),
			) {
			continue
		}

		// Read the device number so diskstats rows
		// can be checked against sysfs discovery.
		data, err := os.ReadFile(
			filepath.Join(devicePath, "dev"),
		)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			return nil, fmt.Errorf(
				"read block device identity %q: %w",
				name,
				err,
			)
		}

		major, minor, found := strings.Cut(
			strings.TrimSpace(string(data)),
			":",
		)

		if !found {
			return nil, fmt.Errorf(
				"%w: invalid device identity for %q",
				errInvalidLinuxDiskStats,
				name,
			)
		}

		device, err := parseLinuxDiskDevice(
			major,
			minor,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"block device %q: %w",
				name,
				err,
			)
		}

		devices[name] = device
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return devices, nil
}

// parseLinuxDiskStats parses /proc/diskstats and
// retains only devices selected through sysfs.
func parseLinuxDiskStats(
	ctx context.Context,
	reader io.Reader,
	devices map[string]linuxDiskDevice,
) ([]DiskUsage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(reader)

	disks := make(
		[]DiskUsage,
		0,
		len(devices),
	)

	seen := make(map[string]struct{})

	lineNumber := 0

	for scanner.Scan() {
		lineNumber++

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		fields := strings.Fields(
			scanner.Text(),
		)

		if len(fields) == 0 {
			continue
		}

		if len(fields) < 3 {
			return nil, fmt.Errorf(
				"%w: incomplete diskstats line %d",
				errInvalidLinuxDiskStats,
				lineNumber,
			)
		}

		name := fields[2]

		expectedDevice, monitored := devices[name]
		if !monitored {
			continue
		}

		device, err := parseLinuxDiskDevice(
			fields[0],
			fields[1],
		)
		if err != nil {
			return nil, fmt.Errorf(
				"diskstats line %d: %w",
				lineNumber,
				err,
			)
		}

		if device != expectedDevice {
			// The device identity changed between
			// sysfs discovery and diskstats collection.
			continue
		}

		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf(
				"%w: duplicate disk %q",
				errInvalidLinuxDiskStats,
				name,
			)
		}

		disk, err := parseLinuxDiskCounters(
			fields,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"diskstats line %d: %w",
				lineNumber,
				err,
			)
		}

		seen[name] = struct{}{}

		disks = append(disks, disk)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf(
			"read disk statistics: %w",
			err,
		)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Stable device ordering simplifies comparisons
	// between consecutive performance snapshots.
	sort.Slice(disks, func(i, j int) bool {
		return disks[i].Device < disks[j].Device
	})

	return disks, nil
}

// parseLinuxDiskDevice parses major and minor
// block device numbers.
func parseLinuxDiskDevice(
	majorText string,
	minorText string,
) (linuxDiskDevice, error) {
	major, err := strconv.ParseUint(
		majorText,
		10,
		32,
	)
	if err != nil {
		return linuxDiskDevice{}, fmt.Errorf(
			"%w: invalid major device number",
			errInvalidLinuxDiskStats,
		)
	}

	minor, err := strconv.ParseUint(
		minorText,
		10,
		32,
	)
	if err != nil {
		return linuxDiskDevice{}, fmt.Errorf(
			"%w: invalid minor device number",
			errInvalidLinuxDiskStats,
		)
	}

	return linuxDiskDevice{
		Major: uint32(major),
		Minor: uint32(minor),
	}, nil
}

// parseLinuxDiskCounters parses the modern diskstats
// format, which contains three identity fields followed
// by at least eleven statistics.
//
// Additional discard and flush counters are ignored.
func parseLinuxDiskCounters(
	fields []string,
) (DiskUsage, error) {
	if len(fields) < 14 || fields[2] == "" {
		return DiskUsage{}, fmt.Errorf(
			"%w: incomplete disk counters",
			errInvalidLinuxDiskStats,
		)
	}

	var counters [11]uint64

	for i := range counters {
		value, err := strconv.ParseUint(
			fields[i+3],
			10,
			64,
		)
		if err != nil {
			return DiskUsage{}, fmt.Errorf(
				"%w: invalid disk counter at index %d",
				errInvalidLinuxDiskStats,
				i,
			)
		}

		counters[i] = value
	}

	readOperations := counters[0]
	readSectors := counters[2]

	writeOperations := counters[4]
	writeSectors := counters[6]

	readBytes, err := linuxDiskSectorsToBytes(
		readSectors,
	)
	if err != nil {
		return DiskUsage{}, err
	}

	writeBytes, err := linuxDiskSectorsToBytes(
		writeSectors,
	)
	if err != nil {
		return DiskUsage{}, err
	}

	return DiskUsage{
		Device:          fields[2],
		ReadBytes:       readBytes,
		WriteBytes:      writeBytes,
		ReadOperations:  readOperations,
		WriteOperations: writeOperations,
	}, nil
}

// linuxDiskSectorsToBytes converts Linux diskstats
// accounting sectors to bytes while protecting
// against unsigned integer overflow.
func linuxDiskSectorsToBytes(
	sectors uint64,
) (uint64, error) {
	if sectors > math.MaxUint64/linuxDiskSectorBytes {
		return 0, fmt.Errorf(
			"%w: disk byte counter overflow",
			errInvalidLinuxDiskStats,
		)
	}

	return sectors * linuxDiskSectorBytes, nil
}
