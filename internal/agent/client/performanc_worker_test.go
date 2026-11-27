package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/agent/metrics"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// performanceWorkerHarness provides deterministic
// collection, reporting, waiting and time progression.
type performanceWorkerHarness struct {
	t *testing.T

	now time.Time

	collections int
	reports     []*rackosv1.ReportPerformanceRequest
	waits       []time.Duration

	collectionErrors []error
	reportErrors     []error

	maxWaits int
}

func newPerformanceWorkerHarness(
	t *testing.T,
	maxWaits int,
) *performanceWorkerHarness {
	t.Helper()

	return &performanceWorkerHarness{
		t: t,
		now: time.Date(
			2026, 1, 1,
			12, 0, 0, 0,
			time.UTC,
		),
		maxWaits: maxWaits,
	}
}

func (h *performanceWorkerHarness) collect(
	ctx context.Context,
) (metrics.Snapshot, error) {
	h.t.Helper()

	if err := ctx.Err(); err != nil {
		h.t.Fatal(err)
	}

	deadline, exists := ctx.Deadline()

	if !exists ||
		time.Until(deadline) > performanceCollectionLimit {
		h.t.Fatal("invalid collection deadline")
	}

	index := h.collections

	h.collections++

	if index < len(h.collectionErrors) {
		if err := h.collectionErrors[index]; err != nil {
			return metrics.Snapshot{}, err
		}
	}

	snapshot := testPerformanceConversionSnapshot()

	snapshot.CollectedAt = h.now

	return snapshot, nil
}

func (h *performanceWorkerHarness) report(
	ctx context.Context,
	request *rackosv1.ReportPerformanceRequest,
) (time.Time, error) {
	h.t.Helper()

	if err := ctx.Err(); err != nil {
		h.t.Fatal(err)
	}

	deadline, exists := ctx.Deadline()

	if !exists ||
		time.Until(deadline) > performanceReportTimeout {
		h.t.Fatal("invalid reporting deadline")
	}

	index := len(h.reports)

	h.reports = append(
		h.reports,
		proto.Clone(request).(*rackosv1.ReportPerformanceRequest),
	)

	if index < len(h.reportErrors) {
		if err := h.reportErrors[index]; err != nil {
			return time.Time{}, err
		}
	}

	return h.now, nil
}

func (h *performanceWorkerHarness) wait(
	ctx context.Context,
	delay time.Duration,
) bool {
	h.t.Helper()

	if ctx.Err() != nil {
		return false
	}

	if delay <= 0 {
		h.t.Fatal("nonpositive wait interval")
	}

	h.waits = append(
		h.waits,
		delay,
	)

	h.now = h.now.Add(delay)

	return len(h.waits) < h.maxWaits
}

func (h *performanceWorkerHarness) run() error {
	h.t.Helper()

	return runPerformanceReporting(
		context.Background(),
		performanceReportingDeviceID,
		h.collect,
		h.report,
		h.wait,
		func() time.Time {
			return h.now
		},
		testPerformanceWorkerLogger(),
	)
}

func testPerformanceWorkerLogger() *slog.Logger {
	return slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)
}

func (h *performanceWorkerHarness) requireWaits(
	expected ...time.Duration,
) {
	h.t.Helper()

	if !slices.Equal(h.waits, expected) {
		h.t.Fatalf(
			"expected waits %v, got %v",
			expected,
			h.waits,
		)
	}
}

func (h *performanceWorkerHarness) requireCounts(
	collections int,
	reports int,
) {
	h.t.Helper()

	if h.collections != collections {
		h.t.Fatalf(
			"expected %d collections, got %d",
			collections,
			h.collections,
		)
	}

	if len(h.reports) != reports {
		h.t.Fatalf(
			"expected %d reports, got %d",
			reports,
			len(h.reports),
		)
	}
}

func TestPerformanceWorkerNormalSchedule(t *testing.T) {
	h := newPerformanceWorkerHarness(t, 3)

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	h.requireCounts(3, 3)

	h.requireWaits(
		time.Minute,
		time.Minute,
		time.Minute,
	)

	seen := make(map[string]struct{})

	for i, request := range h.reports {
		if !validPerformanceSampleID(request.SampleId) {
			t.Fatal("invalid sample identity")
		}

		if _, exists := seen[request.SampleId]; exists {
			t.Fatal("new collection reused sample identity")
		}

		seen[request.SampleId] = struct{}{}

		if i == 0 {
			continue
		}

		previous := h.reports[i-1].
			Snapshot.CollectedAt.AsTime()

		current := request.Snapshot.
			CollectedAt.AsTime()

		if current.Sub(previous) != time.Minute {
			t.Fatal("incorrect collection schedule")
		}
	}
}

func TestPerformanceWorkerStableRetry(t *testing.T) {
	h := newPerformanceWorkerHarness(t, 4)

	h.reportErrors = []error{
		status.Error(
			codes.Unavailable,
			"temporary failure",
		),
		status.Error(
			codes.DeadlineExceeded,
			"temporary failure",
		),
		status.Error(
			codes.Internal,
			"temporary failure",
		),
		nil,
	}

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	h.requireCounts(1, 4)

	h.requireWaits(
		5*time.Second,
		10*time.Second,
		20*time.Second,
		time.Minute,
	)

	for _, request := range h.reports {
		if !proto.Equal(
			request,
			h.reports[0],
		) {
			t.Fatal("retry changed pending request")
		}
	}
}

func TestPerformanceWorkerRetryMaximum(t *testing.T) {
	h := newPerformanceWorkerHarness(t, 7)

	for i := 0; i < 7; i++ {
		h.reportErrors = append(
			h.reportErrors,
			status.Error(
				codes.Unavailable,
				"temporary failure",
			),
		)
	}

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	h.requireCounts(1, 7)

	h.requireWaits(
		5*time.Second,
		10*time.Second,
		20*time.Second,
		40*time.Second,
		time.Minute,
		time.Minute,
		time.Minute,
	)
}

func TestPerformanceWorkerRetryResetAfterSuccess(t *testing.T) {
	h := newPerformanceWorkerHarness(t, 5)

	temporary := status.Error(
		codes.Unavailable,
		"temporary failure",
	)

	h.reportErrors = []error{
		temporary,
		temporary,
		nil,
		temporary,
		nil,
	}

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	h.requireCounts(2, 5)

	h.requireWaits(
		5*time.Second,
		10*time.Second,
		time.Minute,
		5*time.Second,
		time.Minute,
	)

	if !proto.Equal(h.reports[0], h.reports[2]) {
		t.Fatal("first pending request changed")
	}

	if !proto.Equal(h.reports[3], h.reports[4]) {
		t.Fatal("second pending request changed")
	}

	if h.reports[0].SampleId == h.reports[3].SampleId {
		t.Fatal("new collection reused old sample identity")
	}
}

func TestPerformanceWorkerCollectionFailure(t *testing.T) {
	h := newPerformanceWorkerHarness(t, 4)

	h.collectionErrors = []error{
		errors.New("collection failed"),
		errors.New("collection failed"),
		nil,
		nil,
	}

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	h.requireCounts(4, 2)

	h.requireWaits(
		5*time.Second,
		10*time.Second,
		time.Minute,
		time.Minute,
	)
}

func TestPerformanceWorkerUnsupportedPlatform(t *testing.T) {
	h := newPerformanceWorkerHarness(t, 1)

	h.collectionErrors = []error{
		fmt.Errorf(
			"collect metrics: %w",
			metrics.ErrUnsupportedPlatform,
		),
	}

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	h.requireCounts(1, 0)
	h.requireWaits()
}

func TestPerformanceWorkerInvalidAcknowledgement(t *testing.T) {
	h := newPerformanceWorkerHarness(t, 3)

	h.reportErrors = []error{
		ErrInvalidPerformanceAcknowledgement,
		fmt.Errorf(
			"invalid receipt: %w",
			ErrInvalidPerformanceAcknowledgement,
		),
		nil,
	}

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	h.requireCounts(1, 3)

	h.requireWaits(
		5*time.Second,
		10*time.Second,
		time.Minute,
	)

	for _, request := range h.reports {
		if !proto.Equal(
			request,
			h.reports[0],
		) {
			t.Fatal("invalid acknowledgement changed pending sample")
		}
	}
}

func TestPerformanceWorkerPermanentRejection(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			"invalid argument",
			status.Error(
				codes.InvalidArgument,
				"invalid sample",
			),
		},
		{
			"conflicting sample",
			status.Error(
				codes.AlreadyExists,
				"conflicting sample",
			),
		},
		{
			"failed precondition",
			status.Error(
				codes.FailedPrecondition,
				"rejected sample",
			),
		},
		{
			"local invalid report",
			ErrInvalidPerformanceReport,
		},
		{
			"local oversized report",
			ErrPerformanceReportTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newPerformanceWorkerHarness(t, 2)

			h.reportErrors = []error{
				tt.err,
				nil,
			}

			if err := h.run(); err != nil {
				t.Fatal(err)
			}

			h.requireCounts(2, 2)

			h.requireWaits(
				time.Minute,
				time.Minute,
			)

			if h.reports[0].SampleId ==
				h.reports[1].SampleId {
				t.Fatal("rejected sample was reused")
			}
		})
	}
}

func TestPerformanceWorkerAuthenticationFailure(t *testing.T) {
	for _, code := range []codes.Code{
		codes.Unauthenticated,
		codes.PermissionDenied,
	} {
		t.Run(code.String(), func(t *testing.T) {
			h := newPerformanceWorkerHarness(t, 1)

			h.reportErrors = []error{
				status.Error(
					code,
					"authentication failed",
				),
			}

			err := h.run()

			if status.Code(err) != code {
				t.Fatalf(
					"expected %s, got %v",
					code,
					err,
				)
			}

			h.requireCounts(1, 1)
			h.requireWaits()
		})
	}
}

func TestPerformanceWorkerUnimplemented(t *testing.T) {
	h := newPerformanceWorkerHarness(t, 1)

	h.reportErrors = []error{
		status.Error(
			codes.Unimplemented,
			"unsupported RPC",
		),
	}

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	h.requireCounts(1, 1)
	h.requireWaits()
}

func TestPerformanceWorkerPendingExpiration(t *testing.T) {
	const failedAttempts = 18

	h := newPerformanceWorkerHarness(
		t,
		failedAttempts+1,
	)

	startedAt := h.now

	for i := 0; i < failedAttempts; i++ {
		h.reportErrors = append(
			h.reportErrors,
			status.Error(
				codes.Unavailable,
				"appliance offline",
			),
		)
	}

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	h.requireCounts(
		2,
		failedAttempts+1,
	)

	original := h.reports[0]

	for _, request := range h.reports[:failedAttempts] {
		if !proto.Equal(request, original) {
			t.Fatal("pending request changed before expiration")
		}
	}

	replacement := h.reports[failedAttempts]

	if replacement.SampleId == original.SampleId {
		t.Fatal("expired sample identity was reused")
	}

	expectedCollection := startedAt.Add(
		performanceMaxPendingAge,
	)

	if !replacement.Snapshot.CollectedAt.
		AsTime().Equal(expectedCollection) {
		t.Fatal("replacement was not collected at expiration")
	}

	var elapsed time.Duration

	for _, delay := range h.waits[:failedAttempts] {
		elapsed += delay
	}

	if elapsed != performanceMaxPendingAge {
		t.Fatalf(
			"expected expiration after %v, got %v",
			performanceMaxPendingAge,
			elapsed,
		)
	}

	if h.waits[failedAttempts-1] != 45*time.Second {
		t.Fatal("final retry delay was not capped by pending lifetime")
	}

	if h.waits[failedAttempts] != time.Minute {
		t.Fatal("normal reporting did not resume")
	}
}

func TestPerformanceWorkerExpirationDuringReport(t *testing.T) {
	h := newPerformanceWorkerHarness(t, 1)

	err := runPerformanceReporting(
		context.Background(),
		performanceReportingDeviceID,
		h.collect,
		func(
			ctx context.Context,
			request *rackosv1.ReportPerformanceRequest,
		) (time.Time, error) {
			receipt, err := h.report(ctx, request)
			if err != nil {
				return time.Time{}, err
			}

			if len(h.reports) == 1 {
				h.now = h.now.Add(
					performanceMaxPendingAge,
				)

				return time.Time{}, status.Error(
					codes.DeadlineExceeded,
					"report expired",
				)
			}

			return receipt, nil
		},
		h.wait,
		func() time.Time {
			return h.now
		},
		testPerformanceWorkerLogger(),
	)
	if err != nil {
		t.Fatal(err)
	}

	h.requireCounts(2, 2)
	h.requireWaits(time.Minute)

	if h.reports[0].SampleId == h.reports[1].SampleId {
		t.Fatal("expired sample was transmitted again")
	}
}

func TestPerformanceWorkerConstructionFailure(t *testing.T) {
	h := newPerformanceWorkerHarness(t, 2)

	err := runPerformanceReporting(
		context.Background(),
		performanceReportingDeviceID,
		func(
			ctx context.Context,
		) (metrics.Snapshot, error) {
			snapshot, err := h.collect(ctx)
			if err != nil {
				return snapshot, err
			}

			if h.collections == 1 {
				// Missing storage prevents request
				// construction.
				snapshot.Storage = nil
			}

			return snapshot, nil
		},
		h.report,
		h.wait,
		func() time.Time {
			return h.now
		},
		testPerformanceWorkerLogger(),
	)
	if err != nil {
		t.Fatal(err)
	}

	h.requireCounts(2, 1)

	h.requireWaits(
		time.Minute,
		time.Minute,
	)
}

func TestPerformanceWorkerCanceledBeforeStartup(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	// No callbacks should be invoked.
	err := runPerformanceReporting(
		ctx,
		performanceReportingDeviceID,
		nil,
		nil,
		nil,
		nil,
		testPerformanceWorkerLogger(),
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPerformanceWorkerCancellationDuringCollection(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	h := newPerformanceWorkerHarness(t, 1)

	err := runPerformanceReporting(
		ctx,
		performanceReportingDeviceID,
		func(
			ctx context.Context,
		) (metrics.Snapshot, error) {
			snapshot, err := h.collect(ctx)

			cancel()

			return snapshot, err
		},
		h.report,
		h.wait,
		func() time.Time {
			return h.now
		},
		testPerformanceWorkerLogger(),
	)
	if err != nil {
		t.Fatal(err)
	}

	h.requireCounts(1, 0)
	h.requireWaits()
}

func TestPerformanceWorkerCancellationDuringReport(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	h := newPerformanceWorkerHarness(t, 1)

	err := runPerformanceReporting(
		ctx,
		performanceReportingDeviceID,
		h.collect,
		func(
			ctx context.Context,
			request *rackosv1.ReportPerformanceRequest,
		) (time.Time, error) {
			h.report(ctx, request)

			cancel()

			return time.Time{}, context.Canceled
		},
		h.wait,
		func() time.Time {
			return h.now
		},
		testPerformanceWorkerLogger(),
	)
	if err != nil {
		t.Fatal(err)
	}

	h.requireCounts(1, 1)
	h.requireWaits()
}

func TestPerformanceWorkerCancellationDuringWait(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	h := newPerformanceWorkerHarness(t, 1)

	err := runPerformanceReporting(
		ctx,
		performanceReportingDeviceID,
		h.collect,
		h.report,
		func(
			ctx context.Context,
			delay time.Duration,
		) bool {
			h.wait(ctx, delay)

			cancel()

			return false
		},
		func() time.Time {
			return h.now
		},
		testPerformanceWorkerLogger(),
	)
	if err != nil {
		t.Fatal(err)
	}

	h.requireCounts(1, 1)
	h.requireWaits(time.Minute)
}

func TestPerformanceWorkerErrorClassification(t *testing.T) {
	tests := []struct {
		err      error
		expected performanceReportAction
	}{
		{
			ErrInvalidPerformanceReport,
			performanceDiscard,
		},
		{
			ErrPerformanceReportTooLarge,
			performanceDiscard,
		},
		{
			ErrInvalidPerformanceAcknowledgement,
			performanceRetry,
		},
		{
			context.Canceled,
			performanceRetry,
		},
		{
			context.DeadlineExceeded,
			performanceRetry,
		},
		{
			status.Error(codes.Unauthenticated, ""),
			performanceStop,
		},
		{
			status.Error(codes.PermissionDenied, ""),
			performanceStop,
		},
		{
			status.Error(codes.Unimplemented, ""),
			performanceUnsupported,
		},
		{
			status.Error(codes.InvalidArgument, ""),
			performanceDiscard,
		},
		{
			status.Error(codes.AlreadyExists, ""),
			performanceDiscard,
		},
		{
			status.Error(codes.FailedPrecondition, ""),
			performanceDiscard,
		},
		{
			status.Error(codes.Unavailable, ""),
			performanceRetry,
		},
		{
			status.Error(codes.DeadlineExceeded, ""),
			performanceRetry,
		},
		{
			status.Error(codes.Internal, ""),
			performanceRetry,
		},
		{
			status.Error(codes.ResourceExhausted, ""),
			performanceRetry,
		},
		{
			status.Error(codes.Aborted, ""),
			performanceRetry,
		},
		{
			status.Error(codes.Canceled, ""),
			performanceRetry,
		},
		{
			status.Error(codes.Unknown, ""),
			performanceRetry,
		},
		{
			status.Error(codes.OutOfRange, ""),
			performanceDiscard,
		},
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			actual := classifyPerformanceReportError(
				tt.err,
			)

			if actual != tt.expected {
				t.Fatalf(
					"expected action %d, got %d",
					tt.expected,
					actual,
				)
			}
		})
	}
}

func TestNextPerformanceRetry(t *testing.T) {
	tests := []struct {
		current  time.Duration
		expected time.Duration
	}{
		{0, 5 * time.Second},
		{-time.Second, 5 * time.Second},
		{5 * time.Second, 10 * time.Second},
		{10 * time.Second, 20 * time.Second},
		{20 * time.Second, 40 * time.Second},
		{30 * time.Second, time.Minute},
		{40 * time.Second, time.Minute},
		{time.Minute, time.Minute},
		{time.Hour, time.Minute},
	}

	for _, tt := range tests {
		actual := nextPerformanceRetry(
			tt.current,
		)

		if actual != tt.expected {
			t.Fatalf(
				"retry %v: expected %v, got %v",
				tt.current,
				tt.expected,
				actual,
			)
		}
	}
}

func TestRunPerformanceInvalidConfiguration(t *testing.T) {
	client := &Client{}

	tests := []struct {
		deviceID string
		token    string
	}{
		{"", "test-token"},
		{performanceReportingDeviceID, ""},
		{performanceReportingDeviceID, " "},
	}

	for _, tt := range tests {
		err := client.RunPerformance(
			context.Background(),
			tt.deviceID,
			tt.token,
			testPerformanceWorkerLogger(),
		)

		if !errors.Is(
			err,
			ErrInvalidPerformanceReport,
		) {
			t.Fatalf(
				"expected invalid configuration error, got %v",
				err,
			)
		}
	}
}
