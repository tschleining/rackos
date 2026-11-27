package performance

import (
	"errors"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
)

const CurrentPayloadVersion = 1

var (
	ErrNotFound = errors.New(
		"device performance sample not found",
	)

	ErrPerformanceConflict = errors.New(
		"performance sample conflicts with stored sample",
	)

	ErrUnsupportedPayloadVersion = errors.New(
		"unsupported performance payload version",
	)

	ErrCorruptPerformance = errors.New(
		"invalid stored performance record",
	)
)

// Performance represents a historical performance
// snapshot stored for a registered device.
//
// CollectedAt preserves the original snapshot timestamp.
// ReceivedAt uses the appliance's millisecond precision.
//
// Snapshot contains the decoded Protobuf payload.
type Performance struct {
	DeviceID string
	SampleID string

	CollectedAt time.Time
	ReceivedAt  time.Time

	PayloadVersion int

	Snapshot *rackosv1.PerformanceSnapshot
}
