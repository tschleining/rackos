package metrics

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestValidateNetworkInterface(t *testing.T) {
	tests := []struct {
		name    string
		iface   NetworkInterfaceUsage
		wantErr bool
	}{
		{
			name: "physical Ethernet",
			iface: NetworkInterfaceUsage{
				Name:  "eth0",
				Index: 2,
				Up:    true,
				MTU:   1500,
				LinkSpeedBitsPerSecond: networkUint64Ptr(
					1000000000,
				),
			},
		},
		{
			name: "macOS Wi-Fi",
			iface: NetworkInterfaceUsage{
				Name:  "en0",
				Index: 6,
				Up:    true,
				MTU:   1500,
			},
		},
		{
			name: "loopback",
			iface: NetworkInterfaceUsage{
				Name:     "lo",
				Index:    1,
				Up:       true,
				Loopback: true,
				MTU:      65536,
			},
		},
		{
			name: "disabled interface",
			iface: NetworkInterfaceUsage{
				Name:  "eth1",
				Index: 3,
				Up:    false,
				MTU:   1500,
			},
		},
		{
			name: "virtual interface",
			iface: NetworkInterfaceUsage{
				Name:  "veth1234",
				Index: 10,
				Up:    true,
				MTU:   1500,
			},
		},
		{
			name: "VPN interface",
			iface: NetworkInterfaceUsage{
				Name:  "utun0",
				Index: 12,
				Up:    true,
				MTU:   1280,
			},
		},
		{
			name: "unknown MTU",
			iface: NetworkInterfaceUsage{
				Name:  "eth0",
				Index: 2,
				MTU:   0,
			},
		},
		{
			name: "unavailable optional measurements",
			iface: NetworkInterfaceUsage{
				Name:                   "eth0",
				Index:                  2,
				LinkSpeedBitsPerSecond: nil,
				ReceiveDropped:         nil,
				TransmitDropped:        nil,
			},
		},
		{
			name: "zero optional measurements",
			iface: NetworkInterfaceUsage{
				Name:                   "eth0",
				Index:                  2,
				LinkSpeedBitsPerSecond: networkUint64Ptr(0),
				ReceiveDropped:         networkUint64Ptr(0),
				TransmitDropped:        networkUint64Ptr(0),
			},
		},
		{
			name: "maximum cumulative counters",
			iface: NetworkInterfaceUsage{
				Name:               "eth0",
				Index:              2,
				ReceivedBytes:      math.MaxUint64,
				ReceivedPackets:    math.MaxUint64,
				ReceiveErrors:      math.MaxUint64,
				TransmittedBytes:   math.MaxUint64,
				TransmittedPackets: math.MaxUint64,
				TransmitErrors:     math.MaxUint64,
				ReceiveDropped: networkUint64Ptr(
					math.MaxUint64,
				),
				TransmitDropped: networkUint64Ptr(
					math.MaxUint64,
				),
			},
		},
		{
			name: "empty interface name",
			iface: NetworkInterfaceUsage{
				Index: 2,
			},
			wantErr: true,
		},
		{
			name: "invalid interface index",
			iface: NetworkInterfaceUsage{
				Name: "eth0",
			},
			wantErr: true,
		},
		{
			name:    "empty interface",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNetworkInterface(
				tt.iface,
			)

			if tt.wantErr {
				if !errors.Is(
					err,
					ErrInvalidNetworkStatistics,
				) {
					t.Fatalf(
						"expected invalid network statistics, got %v",
						err,
					)
				}

				return
			}

			if err != nil {
				t.Fatalf(
					"unexpected validation error: %v",
					err,
				)
			}
		})
	}
}

func TestValidateNetworkInterfaces(t *testing.T) {
	tests := []struct {
		name       string
		interfaces []NetworkInterfaceUsage
		wantErr    bool
	}{
		{
			name: "multiple valid interfaces",
			interfaces: []NetworkInterfaceUsage{
				{Name: "lo", Index: 1},
				{Name: "eth0", Index: 2},
				{Name: "eth1", Index: 3},
			},
		},
		{
			name: "single interface",
			interfaces: []NetworkInterfaceUsage{
				{Name: "en0", Index: 6},
			},
		},
		{
			name:       "empty collection",
			interfaces: []NetworkInterfaceUsage{},
		},
		{
			name:       "nil collection",
			interfaces: nil,
		},
		{
			name: "duplicate names",
			interfaces: []NetworkInterfaceUsage{
				{Name: "eth0", Index: 2},
				{Name: "eth0", Index: 3},
			},
			wantErr: true,
		},
		{
			name: "duplicate indexes",
			interfaces: []NetworkInterfaceUsage{
				{Name: "eth0", Index: 2},
				{Name: "eth1", Index: 2},
			},
			wantErr: true,
		},
		{
			name: "duplicate interface",
			interfaces: []NetworkInterfaceUsage{
				{Name: "eth0", Index: 2},
				{Name: "eth0", Index: 2},
			},
			wantErr: true,
		},
		{
			name: "invalid interface name",
			interfaces: []NetworkInterfaceUsage{
				{Name: "eth0", Index: 2},
				{Index: 3},
			},
			wantErr: true,
		},
		{
			name: "invalid interface index",
			interfaces: []NetworkInterfaceUsage{
				{Name: "eth0", Index: 2},
				{Name: "eth1", Index: 0},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNetworkInterfaces(
				tt.interfaces,
			)

			if tt.wantErr {
				if !errors.Is(
					err,
					ErrInvalidNetworkStatistics,
				) {
					t.Fatalf(
						"expected invalid network statistics, got %v",
						err,
					)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNetworkUsageJSON(t *testing.T) {
	network := NetworkUsage{
		Interfaces: []NetworkInterfaceUsage{
			{
				Name:     "eth0",
				Index:    2,
				Up:       true,
				Loopback: false,
				MTU:      1500,

				LinkSpeedBitsPerSecond: networkUint64Ptr(
					1000000000,
				),

				ReceivedBytes:   524288000,
				ReceivedPackets: 420000,
				ReceiveErrors:   2,

				TransmittedBytes:   262144000,
				TransmittedPackets: 210000,
				TransmitErrors:     1,

				ReceiveDropped:  networkUint64Ptr(0),
				TransmitDropped: nil,
			},
		},

		InterfaceRates: []NetworkInterfaceRate{
			{
				Name:                     "eth0",
				Index:                    2,
				ReceiveBytesPerSecond:    6000,
				TransmitBytesPerSecond:   3000,
				ReceivePacketsPerSecond:  60,
				TransmitPacketsPerSecond: 30,
				SampleIntervalMillis:     1000,
			},
		},
	}

	data, err := json.Marshal(network)
	if err != nil {
		t.Fatal(err)
	}

	const expected = `{
		"interfaces": [
			{
				"name": "eth0",
				"index": 2,
				"up": true,
				"loopback": false,
				"mtu": 1500,
				"link_speed_bits_per_second": 1000000000,
				"received_bytes": 524288000,
				"received_packets": 420000,
				"receive_errors": 2,
				"transmitted_bytes": 262144000,
				"transmitted_packets": 210000,
				"transmit_errors": 1,
				"receive_dropped": 0,
				"transmit_dropped": null
			}
		],
		"interface_rates": [
			{
				"name": "eth0",
				"index": 2,
				"receive_bytes_per_second": 6000,
				"transmit_bytes_per_second": 3000,
				"receive_packets_per_second": 60,
				"transmit_packets_per_second": 30,
				"sample_interval_millis": 1000
			}
		]
	}`

	var actualJSON map[string]any
	var expectedJSON map[string]any

	if err := json.Unmarshal(data, &actualJSON); err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(
		[]byte(expected),
		&expectedJSON,
	); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(actualJSON, expectedJSON) {
		t.Fatalf("unexpected network JSON: %s", data)
	}

	var decoded NetworkUsage

	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(network, decoded) {
		t.Fatal("network JSON round trip failed")
	}
}

func TestNetworkUsageEmptyJSON(t *testing.T) {
	network := NetworkUsage{
		Interfaces:     []NetworkInterfaceUsage{},
		InterfaceRates: []NetworkInterfaceRate{},
	}

	data, err := json.Marshal(network)
	if err != nil {
		t.Fatal(err)
	}

	const expected = `{"interfaces":[],"interface_rates":[]}`

	if string(data) != expected {
		t.Fatalf(
			"expected %s, got %s",
			expected,
			data,
		)
	}
}

func TestNetworkInterfaceOptionalJSON(t *testing.T) {
	tests := []struct {
		name     string
		value    *uint64
		expected string
	}{
		{
			name:     "unavailable",
			value:    nil,
			expected: "null",
		},
		{
			name:     "zero",
			value:    networkUint64Ptr(0),
			expected: "0",
		},
		{
			name:     "positive value",
			value:    networkUint64Ptr(1000),
			expected: "1000",
		},
		{
			name:     "maximum value",
			value:    networkUint64Ptr(math.MaxUint64),
			expected: "18446744073709551615",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iface := NetworkInterfaceUsage{
				Name:  "eth0",
				Index: 2,

				LinkSpeedBitsPerSecond: tt.value,
				ReceiveDropped:         tt.value,
				TransmitDropped:        tt.value,
			}

			data, err := json.Marshal(iface)
			if err != nil {
				t.Fatal(err)
			}

			var fields map[string]json.RawMessage

			if err := json.Unmarshal(
				data,
				&fields,
			); err != nil {
				t.Fatal(err)
			}

			for _, key := range []string{
				"link_speed_bits_per_second",
				"receive_dropped",
				"transmit_dropped",
			} {
				value, exists := fields[key]
				if !exists {
					t.Fatalf(
						"missing JSON field %q",
						key,
					)
				}

				if string(value) != tt.expected {
					t.Fatalf(
						"field %q: expected %s, got %s",
						key,
						tt.expected,
						value,
					)
				}
			}

			var decoded NetworkInterfaceUsage

			if err := json.Unmarshal(
				data,
				&decoded,
			); err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(
				iface,
				decoded,
			) {
				t.Fatal("optional field round trip failed")
			}
		})
	}
}

func TestNetworkInterfaceLargeCountersJSON(t *testing.T) {
	iface := NetworkInterfaceUsage{
		Name:  "eth0",
		Index: 2,

		ReceivedBytes:      math.MaxUint64,
		ReceivedPackets:    math.MaxUint64 - 1,
		ReceiveErrors:      math.MaxUint64 - 2,
		TransmittedBytes:   math.MaxUint64 - 3,
		TransmittedPackets: math.MaxUint64 - 4,
		TransmitErrors:     math.MaxUint64 - 5,

		ReceiveDropped: networkUint64Ptr(
			math.MaxUint64 - 6,
		),

		TransmitDropped: networkUint64Ptr(
			math.MaxUint64 - 7,
		),
	}

	data, err := json.Marshal(iface)
	if err != nil {
		t.Fatal(err)
	}

	var decoded NetworkInterfaceUsage

	if err := json.Unmarshal(
		data,
		&decoded,
	); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(iface, decoded) {
		t.Fatalf(
			"network counter precision was lost: %s",
			data,
		)
	}
}

// networkUint64Ptr creates a pointer to an optional
// unsigned integer measurement for test fixtures.
func networkUint64Ptr(value uint64) *uint64 {
	return &value
}
