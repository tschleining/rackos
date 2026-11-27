package metrics

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrInvalidDiskSample = errors.New(
		"invalid disk sample",
	)

	ErrDiskCounterRegression = errors.New(
		"disk counters decreased",
	)
)

// calculateDiskRate calculates throughput and
// operation rates between two cumulative readings
// for the same storage device.
func calculateDiskRate(
	previous DiskUsage,
	current DiskUsage,
	interval time.Duration,
) (DiskRate, error) {
	if interval.Milliseconds() <= 0 {
		return DiskRate{}, fmt.Errorf(
			"%w: invalid sampling interval %s",
			ErrInvalidDiskSample,
			interval,
		)
	}

	if previous.Device == "" ||
		current.Device == "" {
		return DiskRate{}, fmt.Errorf(
			"%w: empty device name",
			ErrInvalidDiskSample,
		)
	}

	if previous.Device != current.Device {
		return DiskRate{}, fmt.Errorf(
			"%w: device mismatch %q and %q",
			ErrInvalidDiskSample,
			previous.Device,
			current.Device,
		)
	}

	if current.ReadBytes < previous.ReadBytes ||
		current.WriteBytes < previous.WriteBytes ||
		current.ReadOperations < previous.ReadOperations ||
		current.WriteOperations < previous.WriteOperations {
		return DiskRate{}, fmt.Errorf(
			"%w: device %q",
			ErrDiskCounterRegression,
			current.Device,
		)
	}

	readBytes := current.ReadBytes -
		previous.ReadBytes

	writeBytes := current.WriteBytes -
		previous.WriteBytes

	readOperations := current.ReadOperations -
		previous.ReadOperations

	writeOperations := current.WriteOperations -
		previous.WriteOperations

	seconds := interval.Seconds()

	return DiskRate{
		Device: current.Device,

		ReadBytesPerSecond: float64(
			readBytes,
		) / seconds,

		WriteBytesPerSecond: float64(
			writeBytes,
		) / seconds,

		ReadOperationsPerSecond: float64(
			readOperations,
		) / seconds,

		WriteOperationsPerSecond: float64(
			writeOperations,
		) / seconds,

		SampleIntervalMillis: interval.Milliseconds(),
	}, nil
}

// calculateDiskRates matches consecutive disk
// readings by device name and calculates rates.
//
// Newly connected devices, disconnected devices,
// and devices with counter regressions do not
// produce rate measurements for this interval.
func calculateDiskRates(
	previous []DiskUsage,
	current []DiskUsage,
	interval time.Duration,
) ([]DiskRate, error) {
	if interval.Milliseconds() <= 0 {
		return nil, fmt.Errorf(
			"%w: invalid sampling interval %s",
			ErrInvalidDiskSample,
			interval,
		)
	}

	previousByDevice, err := indexDiskUsage(
		previous,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"previous disk sample: %w",
			err,
		)
	}

	currentByDevice, err := indexDiskUsage(
		current,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"current disk sample: %w",
			err,
		)
	}

	rates := make(
		[]DiskRate,
		0,
		min(
			len(previousByDevice),
			len(currentByDevice),
		),
	)

	for device, currentDisk := range currentByDevice {
		previousDisk, exists := previousByDevice[device]
		if !exists {
			// No baseline exists for this device.
			continue
		}

		rate, err := calculateDiskRate(
			previousDisk,
			currentDisk,
			interval,
		)

		if errors.Is(err, ErrDiskCounterRegression) {
			// The device counters reset. A new
			// baseline is required before another
			// valid rate can be calculated.
			continue
		}

		if err != nil {
			return nil, fmt.Errorf(
				"calculate disk rate for %q: %w",
				device,
				err,
			)
		}

		rates = append(rates, rate)
	}

	// Stable ordering simplifies diagnostics and
	// comparisons between performance snapshots.
	sort.Slice(rates, func(i, j int) bool {
		return rates[i].Device < rates[j].Device
	})

	return rates, nil
}

// indexDiskUsage validates disk identifiers and
// creates an index for matching consecutive samples.
func indexDiskUsage(
	disks []DiskUsage,
) (map[string]DiskUsage, error) {
	index := make(
		map[string]DiskUsage,
		len(disks),
	)

	for _, disk := range disks {
		if disk.Device == "" {
			return nil, fmt.Errorf(
				"%w: empty device name",
				ErrInvalidDiskSample,
			)
		}

		if _, exists := index[disk.Device]; exists {
			return nil, fmt.Errorf(
				"%w: duplicate device %q",
				ErrInvalidDiskSample,
				disk.Device,
			)
		}

		index[disk.Device] = disk
	}

	return index, nil
}
