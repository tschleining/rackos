package httpserver

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"

	"github.com/tschleining/rackos/internal/appliance/performance"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func testPerformanceResponseUint64(
	value uint64,
) *uint64 {
	return &value
}

// testPerformanceResponseRecord constructs a complete
// performance record containing large counters,
// default values, and optional measurements.
func testPerformanceResponseRecord() performance.Performance {
	collectedAt := time.Date(
		2026, 1, 1,
		12, 0, 0,
		123456789,
		time.UTC,
	)

	receivedAt := time.Date(
		2026, 1, 1,
		12, 0, 0,
		125000000,
		time.UTC,
	)

	return performance.Performance{
		DeviceID: testPerformanceHTTPDeviceID,
		SampleID: testPerformanceHTTPSampleID,

		CollectedAt: collectedAt,
		ReceivedAt:  receivedAt,

		PayloadVersion: performance.CurrentPayloadVersion,

		Snapshot: &rackosv1.PerformanceSnapshot{
			CollectedAt: timestamppb.New(
				collectedAt,
			),

			Cpu: &rackosv1.CPUPerformance{
				UsagePercent:         0,
				SampleIntervalMillis: 1000,
			},

			Memory: &rackosv1.MemoryPerformance{
				TotalBytes:     math.MaxUint64,
				AvailableBytes: 0,
				UsedBytes:      math.MaxUint64,
				UsagePercent:   100,
			},

			Storage: &rackosv1.StoragePerformance{
				Disks: []*rackosv1.DiskPerformance{
					{
						Device:     "sda",
						ReadBytes:  math.MaxUint64,
						WriteBytes: 0,
					},
				},
			},

			Network: &rackosv1.NetworkPerformance{
				Interfaces: []*rackosv1.NetworkInterfacePerformance{
					{
						Name:  "eth0",
						Index: 2,
						Up:    false,
						Mtu:   1500,

						ReceivedBytes: math.MaxUint64,

						ReceiveDropped: testPerformanceResponseUint64(
							0,
						),

						TransmitDropped: nil,

						LinkSpeedBitsPerSecond: nil,
					},
				},
			},
		},
	}
}

// performanceResponseJSONObject decodes an object
// while preserving exact JSON value representations.
func performanceResponseJSONObject(
	t *testing.T,
	data []byte,
) map[string]json.RawMessage {
	t.Helper()

	var object map[string]json.RawMessage

	if err := json.Unmarshal(
		data,
		&object,
	); err != nil {
		t.Fatal(err)
	}

	if object == nil {
		t.Fatal("expected JSON object")
	}

	return object
}

func requirePerformanceResponseField(
	t *testing.T,
	object map[string]json.RawMessage,
	field string,
	expected string,
) {
	t.Helper()

	actual, exists := object[field]

	if !exists {
		t.Fatalf(
			"missing JSON field %q",
			field,
		)
	}

	if string(actual) != expected {
		t.Fatalf(
			"field %q: expected %s, got %s",
			field,
			expected,
			actual,
		)
	}
}

func requirePerformanceResponseAbsent(
	t *testing.T,
	object map[string]json.RawMessage,
	field string,
) {
	t.Helper()

	if _, exists := object[field]; exists {
		t.Fatalf(
			"unexpected JSON field %q",
			field,
		)
	}
}

func TestPerformanceResponseEnvelope(
	t *testing.T,
) {
	record := testPerformanceResponseRecord()

	// The HTTP response must normalize timestamp
	// locations without changing their instants.
	location := time.FixedZone(
		"test-offset",
		-5*60*60,
	)

	record.CollectedAt = record.CollectedAt.In(
		location,
	)

	record.ReceivedAt = record.ReceivedAt.In(
		location,
	)

	response, err := toPerformanceResponse(
		record,
	)
	if err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}

	object := performanceResponseJSONObject(
		t,
		data,
	)

	requirePerformanceResponseField(
		t,
		object,
		"device_id",
		`"`+record.DeviceID+`"`,
	)

	requirePerformanceResponseField(
		t,
		object,
		"sample_id",
		`"`+record.SampleID+`"`,
	)

	requirePerformanceResponseField(
		t,
		object,
		"collected_at",
		`"2026-01-01T12:00:00.123456789Z"`,
	)

	requirePerformanceResponseField(
		t,
		object,
		"received_at",
		`"2026-01-01T12:00:00.125Z"`,
	)

	requirePerformanceResponseField(
		t,
		object,
		"payload_version",
		"1",
	)

	if response.CollectedAt.Location() != time.UTC ||
		response.ReceivedAt.Location() != time.UTC {
		t.Fatal("response timestamps are not UTC")
	}

	// The snapshot must be embedded as an object,
	// rather than a serialized JSON string.
	snapshot := performanceResponseJSONObject(
		t,
		object["snapshot"],
	)

	requirePerformanceResponseField(
		t,
		snapshot,
		"collected_at",
		`"2026-01-01T12:00:00.123456789Z"`,
	)

	requirePerformanceResponseAbsent(
		t,
		object,
		"payload",
	)

	requirePerformanceResponseAbsent(
		t,
		object,
		"agent_token",
	)
}

func TestPerformanceResponseNumericPrecision(
	t *testing.T,
) {
	record := testPerformanceResponseRecord()

	response, err := toPerformanceResponse(
		record,
	)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := performanceResponseJSONObject(
		t,
		response.Snapshot,
	)

	cpu := performanceResponseJSONObject(
		t,
		snapshot["cpu"],
	)

	requirePerformanceResponseField(
		t,
		cpu,
		"usage_percent",
		"0",
	)

	requirePerformanceResponseField(
		t,
		cpu,
		"sample_interval_millis",
		`"1000"`,
	)

	memory := performanceResponseJSONObject(
		t,
		snapshot["memory"],
	)

	requirePerformanceResponseField(
		t,
		memory,
		"total_bytes",
		`"18446744073709551615"`,
	)

	requirePerformanceResponseField(
		t,
		memory,
		"available_bytes",
		`"0"`,
	)

	requirePerformanceResponseField(
		t,
		memory,
		"used_bytes",
		`"18446744073709551615"`,
	)

	requirePerformanceResponseField(
		t,
		memory,
		"usage_percent",
		"100",
	)

	storage := performanceResponseJSONObject(
		t,
		snapshot["storage"],
	)

	var disks []map[string]json.RawMessage

	if err := json.Unmarshal(
		storage["disks"],
		&disks,
	); err != nil {
		t.Fatal(err)
	}

	if len(disks) != 1 {
		t.Fatal("incorrect disk record count")
	}

	requirePerformanceResponseField(
		t,
		disks[0],
		"read_bytes",
		`"18446744073709551615"`,
	)

	requirePerformanceResponseField(
		t,
		disks[0],
		"write_bytes",
		`"0"`,
	)

	requirePerformanceResponseField(
		t,
		disks[0],
		"read_operations",
		`"0"`,
	)
}

func TestPerformanceResponseOptionalMeasurements(
	t *testing.T,
) {
	record := testPerformanceResponseRecord()

	response, err := toPerformanceResponse(
		record,
	)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := performanceResponseJSONObject(
		t,
		response.Snapshot,
	)

	network := performanceResponseJSONObject(
		t,
		snapshot["network"],
	)

	var interfaces []map[string]json.RawMessage

	if err := json.Unmarshal(
		network["interfaces"],
		&interfaces,
	); err != nil {
		t.Fatal(err)
	}

	if len(interfaces) != 1 {
		t.Fatal("incorrect interface count")
	}

	iface := interfaces[0]

	requirePerformanceResponseField(
		t,
		iface,
		"received_bytes",
		`"18446744073709551615"`,
	)

	// Explicit zero is different from an
	// unavailable optional counter.
	requirePerformanceResponseField(
		t,
		iface,
		"receive_dropped",
		`"0"`,
	)

	requirePerformanceResponseAbsent(
		t,
		iface,
		"transmit_dropped",
	)

	requirePerformanceResponseAbsent(
		t,
		iface,
		"link_speed_bits_per_second",
	)

	// Non-optional default values remain visible.
	requirePerformanceResponseField(
		t,
		iface,
		"up",
		"false",
	)

	requirePerformanceResponseField(
		t,
		iface,
		"loopback",
		"false",
	)

	requirePerformanceResponseField(
		t,
		iface,
		"index",
		"2",
	)

	requirePerformanceResponseField(
		t,
		iface,
		"mtu",
		"1500",
	)

	requirePerformanceResponseField(
		t,
		iface,
		"receive_errors",
		`"0"`,
	)
}

func TestPerformanceResponsePresentOptionalMeasurements(
	t *testing.T,
) {
	record := testPerformanceResponseRecord()

	iface := record.Snapshot.Network.Interfaces[0]

	iface.Up = true

	iface.LinkSpeedBitsPerSecond =
		testPerformanceResponseUint64(1000000000)

	iface.TransmitDropped =
		testPerformanceResponseUint64(0)

	iface.ReceiveDropped = nil

	response, err := toPerformanceResponse(
		record,
	)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := performanceResponseJSONObject(
		t,
		response.Snapshot,
	)

	network := performanceResponseJSONObject(
		t,
		snapshot["network"],
	)

	var interfaces []map[string]json.RawMessage

	if err := json.Unmarshal(
		network["interfaces"],
		&interfaces,
	); err != nil {
		t.Fatal(err)
	}

	if len(interfaces) != 1 {
		t.Fatal("incorrect interface count")
	}

	actual := interfaces[0]

	requirePerformanceResponseField(
		t,
		actual,
		"link_speed_bits_per_second",
		`"1000000000"`,
	)

	requirePerformanceResponseField(
		t,
		actual,
		"transmit_dropped",
		`"0"`,
	)

	requirePerformanceResponseAbsent(
		t,
		actual,
		"receive_dropped",
	)
}

func TestPerformanceResponseEmptyCollections(
	t *testing.T,
) {
	record := testPerformanceResponseRecord()

	record.Snapshot.Storage =
		&rackosv1.StoragePerformance{}

	record.Snapshot.Network =
		&rackosv1.NetworkPerformance{}

	response, err := toPerformanceResponse(
		record,
	)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := performanceResponseJSONObject(
		t,
		response.Snapshot,
	)

	storage := performanceResponseJSONObject(
		t,
		snapshot["storage"],
	)

	for _, field := range []string{
		"filesystems",
		"disks",
		"disk_rates",
	} {
		requirePerformanceResponseField(
			t,
			storage,
			field,
			"[]",
		)
	}

	network := performanceResponseJSONObject(
		t,
		snapshot["network"],
	)

	for _, field := range []string{
		"interfaces",
		"interface_rates",
	} {
		requirePerformanceResponseField(
			t,
			network,
			field,
			"[]",
		)
	}
}

func TestPerformanceResponseProtobufRoundTrip(
	t *testing.T,
) {
	record := testPerformanceResponseRecord()

	before := proto.Clone(record.Snapshot)

	response, err := toPerformanceResponse(
		record,
	)
	if err != nil {
		t.Fatal(err)
	}

	var decoded rackosv1.PerformanceSnapshot

	if err := protojson.Unmarshal(
		response.Snapshot,
		&decoded,
	); err != nil {
		t.Fatal(err)
	}

	if !proto.Equal(
		record.Snapshot,
		&decoded,
	) {
		t.Fatal("JSON round trip changed snapshot")
	}

	if !proto.Equal(
		record.Snapshot,
		before,
	) {
		t.Fatal("serialization modified original snapshot")
	}
}

func TestPerformanceResponseSerializationFailure(
	t *testing.T,
) {
	tests := []struct {
		name   string
		change func(*performance.Performance)
	}{
		{
			name: "missing snapshot",
			change: func(r *performance.Performance) {
				r.Snapshot = nil
			},
		},
		{
			name: "invalid protobuf timestamp",
			change: func(r *performance.Performance) {
				r.Snapshot.CollectedAt =
					&timestamppb.Timestamp{
						Seconds: 253402300800,
					}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := testPerformanceResponseRecord()

			tt.change(&record)

			response, err := toPerformanceResponse(
				record,
			)

			if err == nil {
				t.Fatal("invalid snapshot was serialized")
			}

			if response.Snapshot != nil {
				t.Fatal("failed conversion returned snapshot")
			}
		})
	}
}

func TestPerformanceResponseHistoryPagination(
	t *testing.T,
) {
	first := testPerformanceResponseRecord()
	second := testPerformanceResponseRecord()

	first.SampleID = strings.Repeat("b", 32)
	second.SampleID = strings.Repeat("a", 32)

	second.CollectedAt = second.CollectedAt.Add(
		-time.Minute,
	)

	second.Snapshot.CollectedAt = timestamppb.New(
		second.CollectedAt,
	)

	options := performance.HistoryQuery{
		Limit: 2,
	}

	cursor := &performance.HistoryCursor{
		CollectedAtMillis: second.CollectedAt.UnixMilli(),
		SampleID:          second.SampleID,
	}

	page := performance.HistoryPage{
		Samples: []performance.Performance{
			first,
			second,
		},

		NextCursor: cursor,
	}

	response, err := toPerformanceHistoryResponse(
		testPerformanceHTTPDeviceID,
		options,
		page,
	)
	if err != nil {
		t.Fatal(err)
	}

	if response.DeviceID != testPerformanceHTTPDeviceID {
		t.Fatal("incorrect history device identity")
	}

	if response.Count != 2 ||
		len(response.Samples) != 2 {
		t.Fatal("incorrect history response count")
	}

	if !response.HasMore ||
		response.NextCursor == nil {
		t.Fatal("missing history continuation")
	}

	if response.Samples[0].SampleID != first.SampleID ||
		response.Samples[1].SampleID != second.SampleID {
		t.Fatal("response changed repository ordering")
	}

	decoded, err := decodePerformanceCursor(
		testPerformanceHTTPDeviceID,
		options,
		*response.NextCursor,
	)
	if err != nil {
		t.Fatal(err)
	}

	if decoded == nil || *decoded != *cursor {
		t.Fatal("incorrect serialized continuation")
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}

	object := performanceResponseJSONObject(
		t,
		data,
	)

	requirePerformanceResponseField(
		t,
		object,
		"count",
		"2",
	)

	requirePerformanceResponseField(
		t,
		object,
		"has_more",
		"true",
	)

	// Samples must be a JSON array of objects.
	var samples []map[string]json.RawMessage

	if err := json.Unmarshal(
		object["samples"],
		&samples,
	); err != nil {
		t.Fatal(err)
	}

	if len(samples) != 2 {
		t.Fatal("incorrect serialized sample count")
	}
}

func TestPerformanceResponseEmptyHistory(
	t *testing.T,
) {
	response, err := toPerformanceHistoryResponse(
		testPerformanceHTTPDeviceID,
		performance.HistoryQuery{
			Limit: performance.DefaultHistoryLimit,
		},
		performance.HistoryPage{},
	)
	if err != nil {
		t.Fatal(err)
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}

	object := performanceResponseJSONObject(
		t,
		data,
	)

	requirePerformanceResponseField(
		t,
		object,
		"samples",
		"[]",
	)

	requirePerformanceResponseField(
		t,
		object,
		"count",
		"0",
	)

	requirePerformanceResponseField(
		t,
		object,
		"has_more",
		"false",
	)

	requirePerformanceResponseField(
		t,
		object,
		"next_cursor",
		"null",
	)
}

func TestPerformanceResponseFinalHistoryPage(
	t *testing.T,
) {
	record := testPerformanceResponseRecord()

	response, err := toPerformanceHistoryResponse(
		record.DeviceID,
		performance.HistoryQuery{
			Limit: 10,
		},
		performance.HistoryPage{
			Samples: []performance.Performance{
				record,
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if response.Count != 1 ||
		len(response.Samples) != 1 ||
		response.HasMore ||
		response.NextCursor != nil {
		t.Fatal("incorrect final history page")
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}

	object := performanceResponseJSONObject(
		t,
		data,
	)

	requirePerformanceResponseField(
		t,
		object,
		"next_cursor",
		"null",
	)
}

func TestPerformanceResponseHistoryFailure(
	t *testing.T,
) {
	tests := []struct {
		name   string
		change func(*performance.HistoryPage)
	}{
		{
			name: "missing snapshot",
			change: func(p *performance.HistoryPage) {
				p.Samples[1].Snapshot = nil
			},
		},
		{
			name: "different device",
			change: func(p *performance.HistoryPage) {
				p.Samples[1].DeviceID =
					testPerformanceHTTPOtherDeviceID
			},
		},
		{
			name: "invalid cursor",
			change: func(p *performance.HistoryPage) {
				p.NextCursor =
					&performance.HistoryCursor{
						CollectedAtMillis: 1000,
						SampleID:          "invalid",
					}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := performance.HistoryPage{
				Samples: []performance.Performance{
					testPerformanceResponseRecord(),
					testPerformanceResponseRecord(),
				},
			}

			tt.change(&page)

			response, err := toPerformanceHistoryResponse(
				testPerformanceHTTPDeviceID,
				performance.HistoryQuery{
					Limit: 2,
				},
				page,
			)

			if err == nil {
				t.Fatal("invalid history was serialized")
			}

			if response.Samples != nil ||
				response.NextCursor != nil ||
				response.Count != 0 {
				t.Fatal("failed history returned partial results")
			}
		})
	}
}
