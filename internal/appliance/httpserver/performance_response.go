package httpserver

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/tschleining/rackos/internal/appliance/performance"

	"google.golang.org/protobuf/encoding/protojson"
)

// performanceResponse represents a single historical
// performance record exposed through the management API.
//
// The snapshot uses the Protobuf JSON representation
// to preserve unsigned 64-bit counter precision and
// optional measurement presence.
type performanceResponse struct {
	DeviceID string `json:"device_id"`
	SampleID string `json:"sample_id"`

	CollectedAt time.Time `json:"collected_at"`
	ReceivedAt  time.Time `json:"received_at"`

	PayloadVersion int `json:"payload_version"`

	Snapshot json.RawMessage `json:"snapshot"`
}

// performanceHistoryResponse represents a bounded
// page of historical performance records.
type performanceHistoryResponse struct {
	DeviceID string                `json:"device_id"`
	Samples  []performanceResponse `json:"samples"`

	Count int `json:"count"`

	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor"`
}

// toPerformanceResponse converts a validated stored
// performance record into its HTTP representation.
//
// Protobuf field names are retained.
//
// Default-valued non-optional scalar fields and empty
// repeated fields are included.
//
// Unavailable optional measurements remain omitted.
func toPerformanceResponse(
	record performance.Performance,
) (performanceResponse, error) {
	if record.Snapshot == nil {
		return performanceResponse{}, fmt.Errorf(
			"cannot serialize missing performance snapshot",
		)
	}

	options := protojson.MarshalOptions{
		UseProtoNames:     true,
		EmitDefaultValues: true,
	}

	snapshot, err := options.Marshal(
		record.Snapshot,
	)
	if err != nil {
		return performanceResponse{}, fmt.Errorf(
			"serialize performance snapshot: %w",
			err,
		)
	}

	return performanceResponse{
		DeviceID: record.DeviceID,
		SampleID: record.SampleID,

		CollectedAt: record.CollectedAt.UTC(),
		ReceivedAt:  record.ReceivedAt.UTC(),

		PayloadVersion: record.PayloadVersion,

		Snapshot: json.RawMessage(snapshot),
	}, nil
}

// toPerformanceHistoryResponse converts a repository
// history page into its HTTP response representation.
//
// Repository ordering is preserved.
//
// A continuation token is included only when another
// page exists.
//
// Empty history is represented as an empty array.
func toPerformanceHistoryResponse(
	deviceID string,
	options performance.HistoryQuery,
	page performance.HistoryPage,
) (performanceHistoryResponse, error) {
	response := performanceHistoryResponse{
		DeviceID: deviceID,

		Samples: make(
			[]performanceResponse,
			0,
			len(page.Samples),
		),

		Count:   len(page.Samples),
		HasMore: page.HasMore(),
	}

	for _, record := range page.Samples {
		if record.DeviceID != deviceID {
			return performanceHistoryResponse{},
				fmt.Errorf(
					"performance history device mismatch",
				)
		}

		sample, err := toPerformanceResponse(
			record,
		)
		if err != nil {
			return performanceHistoryResponse{}, err
		}

		response.Samples = append(
			response.Samples,
			sample,
		)
	}

	if page.NextCursor != nil {
		encoded, err := encodePerformanceCursor(
			deviceID,
			options,
			page.NextCursor,
		)
		if err != nil {
			return performanceHistoryResponse{},
				fmt.Errorf(
					"serialize performance continuation: %w",
					err,
				)
		}

		response.NextCursor = &encoded
	}

	return response, nil
}
