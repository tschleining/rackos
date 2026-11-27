package metrics

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// calculateNetworkInterfaceRate calculates network
// activity between two readings of the same interface.
func calculateNetworkInterfaceRate(
	previous NetworkInterfaceUsage,
	current NetworkInterfaceUsage,
	interval time.Duration,
) (NetworkInterfaceRate, error) {
	if interval.Milliseconds() <= 0 {
		return NetworkInterfaceRate{}, fmt.Errorf(
			"%w: invalid sampling interval %s",
			ErrInvalidNetworkSample,
			interval,
		)
	}

	if err := validateNetworkInterface(previous); err != nil {
		return NetworkInterfaceRate{}, fmt.Errorf(
			"%w: invalid previous interface: %w",
			ErrInvalidNetworkSample,
			err,
		)
	}

	if err := validateNetworkInterface(current); err != nil {
		return NetworkInterfaceRate{}, fmt.Errorf(
			"%w: invalid current interface: %w",
			ErrInvalidNetworkSample,
			err,
		)
	}

	if previous.Name != current.Name ||
		previous.Index != current.Index {
		return NetworkInterfaceRate{}, fmt.Errorf(
			"%w: interface identity changed",
			ErrInvalidNetworkSample,
		)
	}

	if networkCountersRegressed(previous, current) {
		return NetworkInterfaceRate{}, fmt.Errorf(
			"%w: interface %q",
			ErrNetworkCounterRegression,
			current.Name,
		)
	}

	seconds := interval.Seconds()

	return NetworkInterfaceRate{
		Name:  current.Name,
		Index: current.Index,

		ReceiveBytesPerSecond: float64(
			current.ReceivedBytes-
				previous.ReceivedBytes,
		) / seconds,

		TransmitBytesPerSecond: float64(
			current.TransmittedBytes-
				previous.TransmittedBytes,
		) / seconds,

		ReceivePacketsPerSecond: float64(
			current.ReceivedPackets-
				previous.ReceivedPackets,
		) / seconds,

		TransmitPacketsPerSecond: float64(
			current.TransmittedPackets-
				previous.TransmittedPackets,
		) / seconds,

		SampleIntervalMillis: interval.Milliseconds(),
	}, nil
}

// networkCountersRegressed determines whether any
// comparable cumulative interface counter decreased.
func networkCountersRegressed(
	previous NetworkInterfaceUsage,
	current NetworkInterfaceUsage,
) bool {
	if current.ReceivedBytes < previous.ReceivedBytes ||
		current.TransmittedBytes < previous.TransmittedBytes ||
		current.ReceivedPackets < previous.ReceivedPackets ||
		current.TransmittedPackets < previous.TransmittedPackets ||
		current.ReceiveErrors < previous.ReceiveErrors ||
		current.TransmitErrors < previous.TransmitErrors {
		return true
	}

	if optionalNetworkCounterRegressed(
		previous.ReceiveDropped,
		current.ReceiveDropped,
	) {
		return true
	}

	if optionalNetworkCounterRegressed(
		previous.TransmitDropped,
		current.TransmitDropped,
	) {
		return true
	}

	return false
}

// optionalNetworkCounterRegressed compares optional
// counters only when both readings are available.
func optionalNetworkCounterRegressed(
	previous *uint64,
	current *uint64,
) bool {
	if previous == nil || current == nil {
		return false
	}

	return *current < *previous
}

// calculateNetworkRates calculates network activity
// for interfaces present in both samples.
//
// New, removed, renamed, replaced, or reset
// interfaces do not produce rates for the interval.
func calculateNetworkRates(
	previous []NetworkInterfaceUsage,
	current []NetworkInterfaceUsage,
	interval time.Duration,
) ([]NetworkInterfaceRate, error) {
	if interval.Milliseconds() <= 0 {
		return nil, fmt.Errorf(
			"%w: invalid sampling interval %s",
			ErrInvalidNetworkSample,
			interval,
		)
	}

	if err := validateNetworkInterfaces(previous); err != nil {
		return nil, fmt.Errorf(
			"%w: invalid previous interfaces: %w",
			ErrInvalidNetworkSample,
			err,
		)
	}

	if err := validateNetworkInterfaces(current); err != nil {
		return nil, fmt.Errorf(
			"%w: invalid current interfaces: %w",
			ErrInvalidNetworkSample,
			err,
		)
	}

	previousByName := make(
		map[string]NetworkInterfaceUsage,
		len(previous),
	)

	for _, iface := range previous {
		previousByName[iface.Name] = iface
	}

	rates := make(
		[]NetworkInterfaceRate,
		0,
		len(current),
	)

	for _, iface := range current {
		baseline, exists := previousByName[iface.Name]

		if !exists || baseline.Index != iface.Index {
			continue
		}

		rate, err := calculateNetworkInterfaceRate(
			baseline,
			iface,
			interval,
		)

		if errors.Is(err, ErrNetworkCounterRegression) {
			// A reset on one interface does not
			// invalidate other interface rates.
			continue
		}

		if err != nil {
			return nil, err
		}

		rates = append(rates, rate)
	}

	sort.Slice(rates, func(i, j int) bool {
		return rates[i].Name < rates[j].Name
	})

	return rates, nil
}
