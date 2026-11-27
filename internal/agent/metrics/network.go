package metrics

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidNetworkStatistics = errors.New(
		"invalid network statistics",
	)

	ErrInvalidNetworkSample = errors.New(
		"invalid network sample",
	)

	ErrNetworkCounterRegression = errors.New(
		"network counters decreased",
	)
)

// NetworkUsage represents system-wide network
// interface measurements collected by the agent.
type NetworkUsage struct {
	Interfaces     []NetworkInterfaceUsage `json:"interfaces"`
	InterfaceRates []NetworkInterfaceRate  `json:"interface_rates"`
}

// NetworkInterfaceUsage represents interface
// metadata and cumulative network activity counters.
//
// Counters may reset following a reboot, interface
// recreation, or driver reinitialization.
type NetworkInterfaceUsage struct {
	// Interface identity and metadata.
	Name     string `json:"name"`
	Index    uint32 `json:"index"`
	Up       bool   `json:"up"`
	Loopback bool   `json:"loopback"`
	MTU      uint32 `json:"mtu"`

	// LinkSpeedBitsPerSecond is nil when the
	// operating system cannot provide a link speed.
	LinkSpeedBitsPerSecond *uint64 `json:"link_speed_bits_per_second"`

	// Cumulative receive counters.
	ReceivedBytes   uint64 `json:"received_bytes"`
	ReceivedPackets uint64 `json:"received_packets"`
	ReceiveErrors   uint64 `json:"receive_errors"`

	// Cumulative transmit counters.
	TransmittedBytes   uint64 `json:"transmitted_bytes"`
	TransmittedPackets uint64 `json:"transmitted_packets"`
	TransmitErrors     uint64 `json:"transmit_errors"`

	// Dropped-packet counters are optional because
	// availability varies by platform and interface.
	ReceiveDropped  *uint64 `json:"receive_dropped"`
	TransmitDropped *uint64 `json:"transmit_dropped"`
}

func validateNetworkInterface(
	iface NetworkInterfaceUsage,
) error {
	if iface.Name == "" {
		return fmt.Errorf(
			"%w: empty interface name",
			ErrInvalidNetworkStatistics,
		)
	}

	if iface.Index == 0 {
		return fmt.Errorf(
			"%w: interface %q has an invalid index",
			ErrInvalidNetworkStatistics,
			iface.Name,
		)
	}

	return nil
}

// validateNetworkInterfaces validates interface
// metadata and ensures that interface identities
// are unique within a collection.
func validateNetworkInterfaces(
	interfaces []NetworkInterfaceUsage,
) error {
	names := make(
		map[string]struct{},
		len(interfaces),
	)

	indexes := make(
		map[uint32]struct{},
		len(interfaces),
	)

	for _, iface := range interfaces {
		if err := validateNetworkInterface(iface); err != nil {
			return err
		}

		if _, exists := names[iface.Name]; exists {
			return fmt.Errorf(
				"%w: duplicate interface name %q",
				ErrInvalidNetworkStatistics,
				iface.Name,
			)
		}

		if _, exists := indexes[iface.Index]; exists {
			return fmt.Errorf(
				"%w: duplicate interface index %d",
				ErrInvalidNetworkStatistics,
				iface.Index,
			)
		}

		names[iface.Name] = struct{}{}
		indexes[iface.Index] = struct{}{}
	}

	return nil
}

// NetworkInterfaceRate represents calculated network
// activity over a measured sampling interval.
type NetworkInterfaceRate struct {
	Name  string `json:"name"`
	Index uint32 `json:"index"`

	ReceiveBytesPerSecond  float64 `json:"receive_bytes_per_second"`
	TransmitBytesPerSecond float64 `json:"transmit_bytes_per_second"`

	ReceivePacketsPerSecond  float64 `json:"receive_packets_per_second"`
	TransmitPacketsPerSecond float64 `json:"transmit_packets_per_second"`

	SampleIntervalMillis int64 `json:"sample_interval_millis"`
}
