package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/agent/metrics"

	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

const (
	performanceReportTimeout = 10 * time.Second

	performanceSampleIDBytes  = 16
	performanceSampleIDLength = 32

	maxPerformanceRequestBytes = 512 * 1024
)

var (
	ErrInvalidPerformanceReport = errors.New(
		"invalid performance report",
	)

	ErrPerformanceReportTooLarge = errors.New(
		"performance report exceeds maximum size",
	)

	ErrInvalidPerformanceAcknowledgement = errors.New(
		"invalid performance acknowledgement",
	)
)

// NewPerformanceRequest constructs a performance report
// with an independently owned snapshot and a unique
// sample identity.
//
// The same request must be retained for retries.
// Creating a new request generates a new sample ID.
func NewPerformanceRequest(
	deviceID string,
	snapshot metrics.Snapshot,
) (*rackosv1.ReportPerformanceRequest, error) {
	if err := validatePerformanceDeviceID(deviceID); err != nil {
		return nil, err
	}

	message, err := performanceToProto(snapshot)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: convert performance snapshot: %v",
			ErrInvalidPerformanceReport,
			err,
		)
	}

	sampleID, err := newPerformanceSampleID()
	if err != nil {
		return nil, err
	}

	request := &rackosv1.ReportPerformanceRequest{
		DeviceId: deviceID,
		SampleId: sampleID,
		Snapshot: message,
	}

	if err := validatePerformanceReport(request); err != nil {
		return nil, err
	}

	return request, nil
}

// newPerformanceSampleID generates a cryptographically
// random 128-bit sample identity encoded as lowercase
// hexadecimal.
func newPerformanceSampleID() (string, error) {
	var value [performanceSampleIDBytes]byte

	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf(
			"generate performance sample ID: %w",
			err,
		)
	}

	return hex.EncodeToString(value[:]), nil
}

// ReportPerformance sends an existing performance
// report to the appliance.
//
// This method does not generate sample IDs or perform
// retries. The caller must retain the request unchanged
// until it is acknowledged or discarded.
//
// A successful response returns the original appliance
// receipt timestamp, including for identical retries.
func (c *Client) ReportPerformance(
	ctx context.Context,
	token string,
	request *rackosv1.ReportPerformanceRequest,
) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}

	if strings.TrimSpace(token) == "" {
		return time.Time{}, fmt.Errorf(
			"%w: agent credential is required",
			ErrInvalidPerformanceReport,
		)
	}

	if err := validatePerformanceReport(request); err != nil {
		return time.Time{}, err
	}

	ctx, cancel := context.WithTimeout(
		ctx,
		performanceReportTimeout,
	)
	defer cancel()

	// Preserve unrelated outgoing metadata while
	// ensuring exactly one authorization value.
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()

	md.Set(
		"authorization",
		"Bearer "+token,
	)

	ctx = metadata.NewOutgoingContext(ctx, md)

	response, err := c.service.ReportPerformance(
		ctx,
		request,
	)
	if err != nil {
		return time.Time{}, err
	}

	return validatePerformanceAcknowledgement(
		request.SampleId,
		response,
	)
}

// validatePerformanceDeviceID checks the local device
// identity before constructing or sending a report.
//
// The appliance remains responsible for authentication.
func validatePerformanceDeviceID(deviceID string) error {
	if deviceID == "" ||
		strings.TrimSpace(deviceID) != deviceID {
		return fmt.Errorf(
			"%w: invalid device identity",
			ErrInvalidPerformanceReport,
		)
	}

	return nil
}

// validatePerformanceReport checks the reporting
// envelope before transmission.
//
// Full measurement validation remains the responsibility
// of the appliance.
func validatePerformanceReport(
	request *rackosv1.ReportPerformanceRequest,
) error {
	if request == nil {
		return fmt.Errorf(
			"%w: request is required",
			ErrInvalidPerformanceReport,
		)
	}

	if err := validatePerformanceDeviceID(
		request.DeviceId,
	); err != nil {
		return err
	}

	if !validPerformanceSampleID(request.SampleId) {
		return fmt.Errorf(
			"%w: invalid sample identity",
			ErrInvalidPerformanceReport,
		)
	}

	if request.Snapshot == nil {
		return fmt.Errorf(
			"%w: snapshot is required",
			ErrInvalidPerformanceReport,
		)
	}

	if proto.Size(request) > maxPerformanceRequestBytes {
		return ErrPerformanceReportTooLarge
	}

	return nil
}

// validPerformanceSampleID checks the expected
// lowercase hexadecimal sample ID format.
func validPerformanceSampleID(value string) bool {
	if len(value) != performanceSampleIDLength {
		return false
	}

	for _, character := range value {
		if character >= '0' && character <= '9' {
			continue
		}

		if character >= 'a' && character <= 'f' {
			continue
		}

		return false
	}

	return true
}

// validatePerformanceAcknowledgement checks that the
// appliance acknowledged the requested sample.
//
// A malformed acknowledgement does not establish
// whether the appliance persisted the sample.
func validatePerformanceAcknowledgement(
	sampleID string,
	response *rackosv1.ReportPerformanceResponse,
) (time.Time, error) {
	if response == nil {
		return time.Time{}, fmt.Errorf(
			"%w: response is missing",
			ErrInvalidPerformanceAcknowledgement,
		)
	}

	if !response.GetAccepted() {
		return time.Time{}, fmt.Errorf(
			"%w: report was not accepted",
			ErrInvalidPerformanceAcknowledgement,
		)
	}

	if response.GetSampleId() != sampleID {
		return time.Time{}, fmt.Errorf(
			"%w: sample identity does not match",
			ErrInvalidPerformanceAcknowledgement,
		)
	}

	receivedAt := response.GetReceivedAt()

	if receivedAt == nil {
		return time.Time{}, fmt.Errorf(
			"%w: receipt timestamp is missing",
			ErrInvalidPerformanceAcknowledgement,
		)
	}

	if err := receivedAt.CheckValid(); err != nil {
		return time.Time{}, fmt.Errorf(
			"%w: receipt timestamp is invalid",
			ErrInvalidPerformanceAcknowledgement,
		)
	}

	receipt := receivedAt.AsTime()

	if receipt.UnixMilli() <= 0 {
		return time.Time{}, fmt.Errorf(
			"%w: receipt timestamp must be positive",
			ErrInvalidPerformanceAcknowledgement,
		)
	}

	return receipt, nil
}
