package client

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const performanceReportingDeviceID = "e2a104d8-9c72-4b31-8fa2-63001b24c9e8"

// performanceRPCStub implements the performance RPC
// while embedding the remaining generated interface.
type performanceRPCStub struct {
	rackosv1.AgentServiceClient

	report func(
		context.Context,
		*rackosv1.ReportPerformanceRequest,
	) (*rackosv1.ReportPerformanceResponse, error)
}

func (s *performanceRPCStub) ReportPerformance(
	ctx context.Context,
	request *rackosv1.ReportPerformanceRequest,
	_ ...grpc.CallOption,
) (*rackosv1.ReportPerformanceResponse, error) {
	return s.report(ctx, request)
}

func testPerformanceReportingRequest(
	t *testing.T,
) *rackosv1.ReportPerformanceRequest {
	t.Helper()

	request, err := NewPerformanceRequest(
		performanceReportingDeviceID,
		testPerformanceConversionSnapshot(),
	)
	if err != nil {
		t.Fatal(err)
	}

	return request
}

func testPerformanceReceipt() time.Time {
	return time.Date(
		2026, 1, 1,
		12, 30, 46,
		123000000,
		time.UTC,
	)
}

func testPerformanceAcknowledgement(
	sampleID string,
) *rackosv1.ReportPerformanceResponse {
	return &rackosv1.ReportPerformanceResponse{
		Accepted: true,
		SampleId: sampleID,

		ReceivedAt: timestamppb.New(
			testPerformanceReceipt(),
		),
	}
}

func requirePerformanceReportingError(
	t *testing.T,
	err error,
	expected error,
) {
	t.Helper()

	if !errors.Is(err, expected) {
		t.Fatalf(
			"expected %v, got %v",
			expected,
			err,
		)
	}
}

func TestNewPerformanceRequest(t *testing.T) {
	snapshot := testPerformanceConversionSnapshot()

	request, err := NewPerformanceRequest(
		performanceReportingDeviceID,
		snapshot,
	)
	if err != nil {
		t.Fatal(err)
	}

	if request.DeviceId != performanceReportingDeviceID {
		t.Fatal("incorrect device identity")
	}

	if !validPerformanceSampleID(request.SampleId) {
		t.Fatal("invalid generated sample ID")
	}

	expected, err := performanceToProto(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	if !proto.Equal(
		request.Snapshot,
		expected,
	) {
		t.Fatal("incorrect performance snapshot")
	}
}

func TestNewPerformanceRequestUniqueIDs(t *testing.T) {
	snapshot := testPerformanceConversionSnapshot()

	const count = 128

	seen := make(map[string]struct{}, count)

	for i := 0; i < count; i++ {
		request, err := NewPerformanceRequest(
			performanceReportingDeviceID,
			snapshot,
		)
		if err != nil {
			t.Fatal(err)
		}

		if !validPerformanceSampleID(request.SampleId) {
			t.Fatal("generated invalid sample ID")
		}

		if _, exists := seen[request.SampleId]; exists {
			t.Fatal("duplicate generated sample ID")
		}

		seen[request.SampleId] = struct{}{}
	}
}

func TestNewPerformanceRequestInvalidDevice(t *testing.T) {
	for _, deviceID := range []string{
		"",
		" ",
		" leading",
		"trailing ",
	} {
		request, err := NewPerformanceRequest(
			deviceID,
			testPerformanceConversionSnapshot(),
		)

		requirePerformanceReportingError(
			t,
			err,
			ErrInvalidPerformanceReport,
		)

		if request != nil {
			t.Fatal("invalid device returned a request")
		}
	}
}

func TestNewPerformanceRequestInvalidSnapshot(t *testing.T) {
	snapshot := testPerformanceConversionSnapshot()

	snapshot.Storage = nil

	request, err := NewPerformanceRequest(
		performanceReportingDeviceID,
		snapshot,
	)

	requirePerformanceReportingError(
		t,
		err,
		ErrInvalidPerformanceReport,
	)

	if request != nil {
		t.Fatal("invalid snapshot returned a request")
	}
}

func TestNewPerformanceRequestOversizedSnapshot(t *testing.T) {
	snapshot := testPerformanceConversionSnapshot()

	snapshot.Storage.Filesystems[0].Source =
		strings.Repeat(
			"x",
			maxPerformanceRequestBytes,
		)

	request, err := NewPerformanceRequest(
		performanceReportingDeviceID,
		snapshot,
	)

	requirePerformanceReportingError(
		t,
		err,
		ErrPerformanceReportTooLarge,
	)

	if request != nil {
		t.Fatal("oversized snapshot returned a request")
	}
}

func TestNewPerformanceRequestIndependentSnapshot(t *testing.T) {
	snapshot := testPerformanceConversionSnapshot()

	request, err := NewPerformanceRequest(
		performanceReportingDeviceID,
		snapshot,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := proto.Clone(request)

	snapshot.CPU.UsagePercent = 99

	snapshot.Storage.Disks[0].ReadBytes = 999

	*snapshot.Network.Interfaces[0].
		ReceiveDropped = 999

	if !proto.Equal(request, expected) {
		t.Fatal("source mutation changed pending request")
	}
}

func TestValidPerformanceSampleID(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{strings.Repeat("a", 32), true},
		{strings.Repeat("0", 32), true},
		{strings.Repeat("f", 32), true},
		{"7a91d0c4e8524b36a39f186d027c5e81", true},

		{"", false},
		{strings.Repeat("a", 31), false},
		{strings.Repeat("a", 33), false},
		{strings.Repeat("A", 32), false},
		{strings.Repeat("g", 32), false},
		{strings.Repeat(" ", 32), false},
		{strings.Repeat("a", 31) + "\n", false},
	}

	for _, tt := range tests {
		if validPerformanceSampleID(tt.value) != tt.valid {
			t.Fatalf(
				"unexpected validation result for %q",
				tt.value,
			)
		}
	}
}

func TestReportPerformanceSuccess(t *testing.T) {
	request := testPerformanceReportingRequest(t)

	expected := proto.Clone(request)

	var calls int
	var rpcContext context.Context

	client := &Client{
		service: &performanceRPCStub{
			report: func(
				ctx context.Context,
				actual *rackosv1.ReportPerformanceRequest,
			) (*rackosv1.ReportPerformanceResponse, error) {
				calls++
				rpcContext = ctx

				if !proto.Equal(actual, expected) {
					t.Fatal("incorrect transmitted request")
				}

				deadline, exists := ctx.Deadline()

				if !exists {
					t.Fatal("reporting RPC has no deadline")
				}

				remaining := time.Until(deadline)

				if remaining <= 0 ||
					remaining > performanceReportTimeout {
					t.Fatal("invalid reporting deadline")
				}

				return testPerformanceAcknowledgement(
					actual.SampleId,
				), nil
			},
		},
	}

	receipt, err := client.ReportPerformance(
		context.Background(),
		"test-agent-token",
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !receipt.Equal(testPerformanceReceipt()) {
		t.Fatal("incorrect acknowledgement receipt")
	}

	if calls != 1 {
		t.Fatal("incorrect RPC call count")
	}

	if !proto.Equal(request, expected) {
		t.Fatal("reporting modified original request")
	}

	if rpcContext.Err() != context.Canceled {
		t.Fatal("RPC timeout context was not released")
	}
}

func TestReportPerformanceAuthorizationMetadata(t *testing.T) {
	request := testPerformanceReportingRequest(t)

	ctx := metadata.NewOutgoingContext(
		context.Background(),
		metadata.Pairs(
			"authorization", "Bearer stale-token",
			"authorization", "Bearer duplicate-token",
			"x-request-id", "test-request",
		),
	)

	client := &Client{
		service: &performanceRPCStub{
			report: func(
				ctx context.Context,
				actual *rackosv1.ReportPerformanceRequest,
			) (*rackosv1.ReportPerformanceResponse, error) {
				md, exists := metadata.FromOutgoingContext(
					ctx,
				)

				if !exists {
					t.Fatal("missing outgoing metadata")
				}

				authorization := md.Get("authorization")

				if len(authorization) != 1 {
					t.Fatal("expected exactly one authorization value")
				}

				if authorization[0] != "Bearer test-agent-token" {
					t.Fatal("incorrect authorization metadata")
				}

				requestID := md.Get("x-request-id")

				if len(requestID) != 1 ||
					requestID[0] != "test-request" {
					t.Fatal("unrelated metadata was lost")
				}

				return testPerformanceAcknowledgement(
					actual.SampleId,
				), nil
			},
		},
	}

	_, err := client.ReportPerformance(
		ctx,
		"test-agent-token",
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	// The original context metadata must not change.
	md, _ := metadata.FromOutgoingContext(ctx)

	if len(md.Get("authorization")) != 2 {
		t.Fatal("original context metadata was modified")
	}
}

func TestReportPerformanceInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		token  string
		change func(
			*rackosv1.ReportPerformanceRequest,
		) *rackosv1.ReportPerformanceRequest
	}{
		{
			name: "missing credential",
		},
		{
			name:  "whitespace credential",
			token: " ",
		},
		{
			name:  "missing request",
			token: "test-token",

			change: func(
				r *rackosv1.ReportPerformanceRequest,
			) *rackosv1.ReportPerformanceRequest {
				return nil
			},
		},
		{
			name:  "missing device ID",
			token: "test-token",

			change: func(
				r *rackosv1.ReportPerformanceRequest,
			) *rackosv1.ReportPerformanceRequest {
				r.DeviceId = ""
				return r
			},
		},
		{
			name:  "invalid sample ID",
			token: "test-token",

			change: func(
				r *rackosv1.ReportPerformanceRequest,
			) *rackosv1.ReportPerformanceRequest {
				r.SampleId = "invalid"
				return r
			},
		},
		{
			name:  "missing snapshot",
			token: "test-token",

			change: func(
				r *rackosv1.ReportPerformanceRequest,
			) *rackosv1.ReportPerformanceRequest {
				r.Snapshot = nil
				return r
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := testPerformanceReportingRequest(t)

			if tt.change != nil {
				request = tt.change(request)
			}

			// No service is configured. Invalid local
			// input must be rejected before any RPC.
			client := &Client{}

			receipt, err := client.ReportPerformance(
				context.Background(),
				tt.token,
				request,
			)

			requirePerformanceReportingError(
				t,
				err,
				ErrInvalidPerformanceReport,
			)

			if !receipt.IsZero() {
				t.Fatal("invalid report returned a receipt")
			}
		})
	}
}

func TestReportPerformanceOversizedRequest(t *testing.T) {
	request := testPerformanceReportingRequest(t)

	unknown := protowire.AppendTag(
		nil,
		1000,
		protowire.BytesType,
	)

	unknown = protowire.AppendBytes(
		unknown,
		make(
			[]byte,
			maxPerformanceRequestBytes,
		),
	)

	request.Snapshot.
		ProtoReflect().
		SetUnknown(unknown)

	// Oversized reports must be rejected locally.
	client := &Client{}

	receipt, err := client.ReportPerformance(
		context.Background(),
		"test-token",
		request,
	)

	requirePerformanceReportingError(
		t,
		err,
		ErrPerformanceReportTooLarge,
	)

	if !receipt.IsZero() {
		t.Fatal("oversized report returned a receipt")
	}
}

func TestReportPerformanceRPCError(t *testing.T) {
	request := testPerformanceReportingRequest(t)

	for _, code := range []codes.Code{
		codes.Unauthenticated,
		codes.PermissionDenied,
		codes.InvalidArgument,
		codes.AlreadyExists,
		codes.ResourceExhausted,
		codes.Unimplemented,
		codes.Unavailable,
		codes.DeadlineExceeded,
		codes.Internal,
		codes.Aborted,
		codes.Canceled,
		codes.Unknown,
	} {
		t.Run(code.String(), func(t *testing.T) {
			expected := status.Error(
				code,
				"simulated RPC error",
			)

			calls := 0

			client := &Client{
				service: &performanceRPCStub{
					report: func(
						context.Context,
						*rackosv1.ReportPerformanceRequest,
					) (*rackosv1.ReportPerformanceResponse, error) {
						calls++
						return nil, expected
					},
				},
			}

			receipt, err := client.ReportPerformance(
				context.Background(),
				"test-token",
				request,
			)

			if status.Code(err) != code {
				t.Fatal("RPC error status changed")
			}

			if err != expected {
				t.Fatal("RPC error was replaced")
			}

			if !receipt.IsZero() {
				t.Fatal("failed RPC returned receipt")
			}

			if calls != 1 {
				t.Fatal("reporting client performed an unexpected retry")
			}
		})
	}
}

func TestReportPerformanceCanceledContext(t *testing.T) {
	request := testPerformanceReportingRequest(t)

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	client := &Client{}

	receipt, err := client.ReportPerformance(
		ctx,
		"test-token",
		request,
	)

	requirePerformanceReportingError(
		t,
		err,
		context.Canceled,
	)

	if !receipt.IsZero() {
		t.Fatal("canceled request returned receipt")
	}
}

func TestReportPerformanceParentDeadline(t *testing.T) {
	request := testPerformanceReportingRequest(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Second,
	)
	defer cancel()

	expectedDeadline, _ := ctx.Deadline()

	client := &Client{
		service: &performanceRPCStub{
			report: func(
				ctx context.Context,
				actual *rackosv1.ReportPerformanceRequest,
			) (*rackosv1.ReportPerformanceResponse, error) {
				deadline, exists := ctx.Deadline()

				if !exists ||
					!deadline.Equal(expectedDeadline) {
					t.Fatal("parent deadline was not preserved")
				}

				return testPerformanceAcknowledgement(
					actual.SampleId,
				), nil
			},
		},
	}

	_, err := client.ReportPerformance(
		ctx,
		"test-token",
		request,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestReportPerformanceInvalidAcknowledgement(t *testing.T) {
	tests := []struct {
		name   string
		change func(
			*rackosv1.ReportPerformanceResponse,
		) *rackosv1.ReportPerformanceResponse
	}{
		{
			name: "nil response",

			change: func(
				r *rackosv1.ReportPerformanceResponse,
			) *rackosv1.ReportPerformanceResponse {
				return nil
			},
		},
		{
			name: "not accepted",

			change: func(
				r *rackosv1.ReportPerformanceResponse,
			) *rackosv1.ReportPerformanceResponse {
				r.Accepted = false
				return r
			},
		},
		{
			name: "incorrect sample ID",

			change: func(
				r *rackosv1.ReportPerformanceResponse,
			) *rackosv1.ReportPerformanceResponse {
				r.SampleId = strings.Repeat("b", 32)
				return r
			},
		},
		{
			name: "missing sample ID",

			change: func(
				r *rackosv1.ReportPerformanceResponse,
			) *rackosv1.ReportPerformanceResponse {
				r.SampleId = ""
				return r
			},
		},
		{
			name: "missing receipt",

			change: func(
				r *rackosv1.ReportPerformanceResponse,
			) *rackosv1.ReportPerformanceResponse {
				r.ReceivedAt = nil
				return r
			},
		},
		{
			name: "invalid receipt",

			change: func(
				r *rackosv1.ReportPerformanceResponse,
			) *rackosv1.ReportPerformanceResponse {
				r.ReceivedAt.Nanos = 1000000000
				return r
			},
		},
		{
			name: "Unix epoch receipt",

			change: func(
				r *rackosv1.ReportPerformanceResponse,
			) *rackosv1.ReportPerformanceResponse {
				r.ReceivedAt = timestamppb.New(
					time.Unix(0, 0),
				)
				return r
			},
		},
		{
			name: "negative receipt",

			change: func(
				r *rackosv1.ReportPerformanceResponse,
			) *rackosv1.ReportPerformanceResponse {
				r.ReceivedAt = timestamppb.New(
					time.Unix(-1, 0),
				)
				return r
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := testPerformanceReportingRequest(t)

			// Fixed identity ensures the mismatch test
			// cannot accidentally generate a match.
			request.SampleId = strings.Repeat("a", 32)

			response := testPerformanceAcknowledgement(
				request.SampleId,
			)

			response = tt.change(response)

			client := &Client{
				service: &performanceRPCStub{
					report: func(
						context.Context,
						*rackosv1.ReportPerformanceRequest,
					) (*rackosv1.ReportPerformanceResponse, error) {
						return response, nil
					},
				},
			}

			receipt, err := client.ReportPerformance(
				context.Background(),
				"test-token",
				request,
			)

			requirePerformanceReportingError(
				t,
				err,
				ErrInvalidPerformanceAcknowledgement,
			)

			if !receipt.IsZero() {
				t.Fatal("invalid acknowledgement returned receipt")
			}
		})
	}
}

func TestReportPerformanceStableRetry(t *testing.T) {
	request := testPerformanceReportingRequest(t)

	expected := proto.Clone(request)

	var calls int

	client := &Client{
		service: &performanceRPCStub{
			report: func(
				ctx context.Context,
				actual *rackosv1.ReportPerformanceRequest,
			) (*rackosv1.ReportPerformanceResponse, error) {
				calls++

				if !proto.Equal(actual, expected) {
					t.Fatal("retry changed the pending report")
				}

				if calls == 1 {
					return nil, status.Error(
						codes.Unavailable,
						"simulated acknowledgement loss",
					)
				}

				return testPerformanceAcknowledgement(
					actual.SampleId,
				), nil
			},
		},
	}

	receipt, err := client.ReportPerformance(
		context.Background(),
		"test-token",
		request,
	)

	if status.Code(err) != codes.Unavailable {
		t.Fatal("expected temporary reporting failure")
	}

	if !receipt.IsZero() {
		t.Fatal("failed report returned receipt")
	}

	if calls != 1 {
		t.Fatal("client retried without caller instruction")
	}

	// Explicitly resend the same pending request.
	receipt, err = client.ReportPerformance(
		context.Background(),
		"test-token",
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	if calls != 2 {
		t.Fatal("incorrect retry call count")
	}

	if !receipt.Equal(testPerformanceReceipt()) {
		t.Fatal("retry returned incorrect receipt")
	}

	if !proto.Equal(request, expected) {
		t.Fatal("reporting changed the original request")
	}
}
