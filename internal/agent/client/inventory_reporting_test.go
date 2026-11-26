package client

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/agent/inventory"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type reportingTestHarness struct {
	collectionErrors []error
	reportingErrors  []error

	collectionCalls int
	reportingCalls  int

	collected []inventory.Inventory
	reported  []inventory.Inventory
	delays    []time.Duration

	maxWaits int
}

func reportingTestLogger() *slog.Logger {
	return slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)
}

func (h *reportingTestHarness) collect(
	ctx context.Context,
) (inventory.Inventory, error) {
	index := h.collectionCalls
	h.collectionCalls++

	if err := ctx.Err(); err != nil {
		return inventory.Inventory{}, err
	}

	if index < len(h.collectionErrors) {
		if err := h.collectionErrors[index]; err != nil {
			return inventory.Inventory{}, err
		}
	}

	collectedAt := time.Unix(
		1700000000+int64(index),
		0,
	).UTC()

	bootTime := collectedAt.Add(-24 * time.Hour)

	snapshot := inventory.Inventory{
		Hostname:        "inventory-test.local",
		OS:              "darwin",
		Architecture:    "arm64",
		LogicalCPUs:     14,
		HostLogicalCPUs: 14,
		PhysicalCores:   14,
		CPUModel:        "Apple M4 Pro",
		CPUManufacturer: "Apple",
		MemoryBytes:     25769803776,
		HardwareModel:   "Mac16,8",
		OSVersion:       "26.6.2",
		KernelVersion:   "25.6.0",
		BootTime:        &bootTime,
		CollectedAt:     collectedAt,
	}

	h.collected = append(h.collected, snapshot)

	return snapshot, nil
}

func (h *reportingTestHarness) report(
	ctx context.Context,
	snapshot inventory.Inventory,
) error {
	index := h.reportingCalls
	h.reportingCalls++

	h.reported = append(h.reported, snapshot)

	if err := ctx.Err(); err != nil {
		return err
	}

	if index < len(h.reportingErrors) {
		return h.reportingErrors[index]
	}

	return nil
}

func (h *reportingTestHarness) wait(
	ctx context.Context,
	delay time.Duration,
) bool {
	h.delays = append(h.delays, delay)

	if ctx.Err() != nil {
		return false
	}

	// Stop the scheduler after the requested number
	// of simulated waiting periods.
	return len(h.delays) < h.maxWaits
}

func (h *reportingTestHarness) run() error {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	return runInventoryReporting(
		ctx,
		h.collect,
		h.report,
		h.wait,
		reportingTestLogger(),
	)
}

func requireReportingDelays(
	t *testing.T,
	actual []time.Duration,
	expected []time.Duration,
) {
	t.Helper()

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf(
			"unexpected reporting delays:\n"+
				"expected: %v\nactual:   %v",
			expected,
			actual,
		)
	}
}

func requireReportingCalls(
	t *testing.T,
	h *reportingTestHarness,
	collections int,
	reports int,
) {
	t.Helper()

	if h.collectionCalls != collections {
		t.Fatalf(
			"expected %d collection calls, got %d",
			collections,
			h.collectionCalls,
		)
	}

	if h.reportingCalls != reports {
		t.Fatalf(
			"expected %d reporting calls, got %d",
			reports,
			h.reportingCalls,
		)
	}
}

func TestInventoryReportingStartupAndRefresh(t *testing.T) {
	h := &reportingTestHarness{
		maxWaits: 3,
	}

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	requireReportingCalls(t, h, 3, 3)

	requireReportingDelays(
		t,
		h.delays,
		[]time.Duration{
			6 * time.Hour,
			6 * time.Hour,
			6 * time.Hour,
		},
	)

	for i := range h.reported {
		if !reflect.DeepEqual(
			h.collected[i],
			h.reported[i],
		) {
			t.Fatal("reported snapshot differs from collection")
		}

		if i > 0 &&
			!h.reported[i].CollectedAt.After(
				h.reported[i-1].CollectedAt,
			) {
			t.Fatal("refresh did not collect a newer snapshot")
		}
	}
}

func TestInventoryReportingCollectionFailure(t *testing.T) {
	h := &reportingTestHarness{
		collectionErrors: []error{
			errors.New("collection failed"),
			errors.New("collection failed again"),
			nil,
		},
		maxWaits: 3,
	}

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	requireReportingCalls(t, h, 3, 1)

	requireReportingDelays(
		t,
		h.delays,
		[]time.Duration{
			5 * time.Second,
			10 * time.Second,
			6 * time.Hour,
		},
	)

	if !reflect.DeepEqual(
		h.collected[0],
		h.reported[0],
	) {
		t.Fatal("incorrect snapshot reported after recovery")
	}
}

func TestInventoryReportingPreservesPendingSnapshot(t *testing.T) {
	h := &reportingTestHarness{
		reportingErrors: []error{
			status.Error(
				codes.Unavailable,
				"connection unavailable",
			),
			status.Error(
				codes.DeadlineExceeded,
				"request timed out",
			),
			status.Error(
				codes.Internal,
				"temporary appliance failure",
			),
			nil,
		},
		maxWaits: 4,
	}

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	requireReportingCalls(t, h, 1, 4)

	requireReportingDelays(
		t,
		h.delays,
		[]time.Duration{
			5 * time.Second,
			10 * time.Second,
			20 * time.Second,
			6 * time.Hour,
		},
	)

	for _, reported := range h.reported {
		if !reflect.DeepEqual(
			reported,
			h.collected[0],
		) {
			t.Fatal("temporary failure changed pending snapshot")
		}
	}
}

func TestInventoryReportingRetryCap(t *testing.T) {
	const attempts = 9

	failures := make([]error, attempts)

	for i := range failures {
		failures[i] = status.Error(
			codes.Unavailable,
			"appliance unavailable",
		)
	}

	h := &reportingTestHarness{
		reportingErrors: failures,
		maxWaits:        attempts,
	}

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	requireReportingCalls(t, h, 1, attempts)

	requireReportingDelays(
		t,
		h.delays,
		[]time.Duration{
			5 * time.Second,
			10 * time.Second,
			20 * time.Second,
			40 * time.Second,
			80 * time.Second,
			160 * time.Second,
			300 * time.Second,
			300 * time.Second,
			300 * time.Second,
		},
	)
}

func TestInventoryReportingBackoffReset(t *testing.T) {
	unavailable := status.Error(
		codes.Unavailable,
		"appliance unavailable",
	)

	h := &reportingTestHarness{
		reportingErrors: []error{
			unavailable,
			unavailable,
			nil,
			unavailable,
			nil,
		},
		maxWaits: 5,
	}

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	requireReportingCalls(t, h, 2, 5)

	requireReportingDelays(
		t,
		h.delays,
		[]time.Duration{
			5 * time.Second,
			10 * time.Second,
			6 * time.Hour,
			5 * time.Second,
			6 * time.Hour,
		},
	)
}

func TestInventoryReportingTemporaryStatuses(t *testing.T) {
	codesToTest := []codes.Code{
		codes.Unavailable,
		codes.DeadlineExceeded,
		codes.Internal,
		codes.ResourceExhausted,
		codes.Aborted,
		codes.Canceled,
		codes.Unknown,
	}

	for _, code := range codesToTest {
		t.Run(code.String(), func(t *testing.T) {
			h := &reportingTestHarness{
				reportingErrors: []error{
					status.Error(
						code,
						"temporary failure",
					),
					nil,
				},
				maxWaits: 2,
			}

			if err := h.run(); err != nil {
				t.Fatal(err)
			}

			requireReportingCalls(t, h, 1, 2)

			requireReportingDelays(
				t,
				h.delays,
				[]time.Duration{
					5 * time.Second,
					6 * time.Hour,
				},
			)

			if !reflect.DeepEqual(
				h.reported[0],
				h.reported[1],
			) {
				t.Fatal("retry did not preserve snapshot")
			}
		})
	}
}

func TestInventoryReportingRejectedSnapshot(t *testing.T) {
	codesToTest := []codes.Code{
		codes.InvalidArgument,
		codes.FailedPrecondition,
		codes.AlreadyExists,
		codes.OutOfRange,
	}

	for _, code := range codesToTest {
		t.Run(code.String(), func(t *testing.T) {
			h := &reportingTestHarness{
				reportingErrors: []error{
					status.Error(
						code,
						"inventory rejected",
					),
					nil,
				},
				maxWaits: 2,
			}

			if err := h.run(); err != nil {
				t.Fatal(err)
			}

			requireReportingCalls(t, h, 2, 2)

			requireReportingDelays(
				t,
				h.delays,
				[]time.Duration{
					5 * time.Minute,
					6 * time.Hour,
				},
			)

			if !h.reported[1].CollectedAt.After(
				h.reported[0].CollectedAt,
			) {
				t.Fatal("rejected inventory was not recollected")
			}
		})
	}
}

func TestInventoryReportingLocalFailure(t *testing.T) {
	h := &reportingTestHarness{
		reportingErrors: []error{
			errors.New("inventory conversion failed"),
			nil,
		},
		maxWaits: 2,
	}

	if err := h.run(); err != nil {
		t.Fatal(err)
	}

	requireReportingCalls(t, h, 2, 2)

	requireReportingDelays(
		t,
		h.delays,
		[]time.Duration{
			5 * time.Minute,
			6 * time.Hour,
		},
	)

	if !h.reported[1].CollectedAt.After(
		h.reported[0].CollectedAt,
	) {
		t.Fatal("local failure did not trigger recollection")
	}
}

func TestInventoryReportingAuthenticationFailure(t *testing.T) {
	for _, code := range []codes.Code{
		codes.Unauthenticated,
		codes.PermissionDenied,
	} {
		t.Run(code.String(), func(t *testing.T) {
			h := &reportingTestHarness{
				reportingErrors: []error{
					status.Error(
						code,
						"credential rejected",
					),
				},
				maxWaits: 1,
			}

			err := h.run()

			if status.Code(err) != code {
				t.Fatalf(
					"expected %s, got %v",
					code,
					err,
				)
			}

			requireReportingCalls(t, h, 1, 1)

			if len(h.delays) != 0 {
				t.Fatal("authentication failure was retried")
			}
		})
	}
}

func TestInventoryReportingOlderAppliance(t *testing.T) {
	h := &reportingTestHarness{
		reportingErrors: []error{
			status.Error(
				codes.Unimplemented,
				"inventory not supported",
			),
		},
		maxWaits: 1,
	}

	if err := h.run(); err != nil {
		t.Fatalf(
			"older appliance caused fatal error: %v",
			err,
		)
	}

	requireReportingCalls(t, h, 1, 1)

	if len(h.delays) != 0 {
		t.Fatal("unsupported RPC was retried")
	}
}

func TestInventoryReportingCanceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	h := &reportingTestHarness{
		maxWaits: 1,
	}

	err := runInventoryReporting(
		ctx,
		h.collect,
		h.report,
		h.wait,
		reportingTestLogger(),
	)

	if err != nil {
		t.Fatal(err)
	}

	requireReportingCalls(t, h, 0, 0)

	if len(h.delays) != 0 {
		t.Fatal("canceled scheduler attempted to wait")
	}
}

func TestInventoryReportingCanceledDuringCollection(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	h := &reportingTestHarness{
		maxWaits: 1,
	}

	err := runInventoryReporting(
		ctx,
		func(
			collectionCtx context.Context,
		) (inventory.Inventory, error) {
			cancel()

			return h.collect(collectionCtx)
		},
		h.report,
		h.wait,
		reportingTestLogger(),
	)

	if err != nil {
		t.Fatal(err)
	}

	requireReportingCalls(t, h, 1, 0)

	if len(h.delays) != 0 {
		t.Fatal("canceled collection was retried")
	}
}

func TestInventoryReportingCanceledDuringReport(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	h := &reportingTestHarness{
		maxWaits: 1,
	}

	err := runInventoryReporting(
		ctx,
		h.collect,
		func(
			reportCtx context.Context,
			snapshot inventory.Inventory,
		) error {
			cancel()

			return h.report(reportCtx, snapshot)
		},
		h.wait,
		reportingTestLogger(),
	)

	if err != nil {
		t.Fatal(err)
	}

	requireReportingCalls(t, h, 1, 1)

	if len(h.delays) != 0 {
		t.Fatal("canceled report was retried")
	}
}

func TestInventoryReportingCollectionDeadline(t *testing.T) {
	h := &reportingTestHarness{
		maxWaits: 1,
	}

	err := runInventoryReporting(
		context.Background(),
		func(
			ctx context.Context,
		) (inventory.Inventory, error) {
			deadline, ok := ctx.Deadline()

			if !ok {
				t.Fatal("collection context has no deadline")
			}

			remaining := time.Until(deadline)

			if remaining <= 0 ||
				remaining > inventoryCollectionLimit {
				t.Fatalf(
					"invalid collection deadline: %v",
					remaining,
				)
			}

			return h.collect(ctx)
		},
		h.report,
		h.wait,
		reportingTestLogger(),
	)

	if err != nil {
		t.Fatal(err)
	}

	requireReportingCalls(t, h, 1, 1)
}

func TestNextInventoryRetry(t *testing.T) {
	tests := []struct {
		name     string
		current  time.Duration
		expected time.Duration
	}{
		{
			"zero delay",
			0,
			5 * time.Second,
		},
		{
			"negative delay",
			-time.Second,
			5 * time.Second,
		},
		{
			"initial delay",
			5 * time.Second,
			10 * time.Second,
		},
		{
			"normal doubling",
			40 * time.Second,
			80 * time.Second,
		},
		{
			"half maximum",
			150 * time.Second,
			300 * time.Second,
		},
		{
			"above half maximum",
			160 * time.Second,
			300 * time.Second,
		},
		{
			"maximum delay",
			300 * time.Second,
			300 * time.Second,
		},
		{
			"above maximum",
			time.Hour,
			300 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := nextInventoryRetry(tt.current)

			if actual != tt.expected {
				t.Fatalf(
					"expected %v, got %v",
					tt.expected,
					actual,
				)
			}
		})
	}
}

func TestWaitForInventoryElapsed(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Second,
	)
	defer cancel()

	if !waitForInventory(ctx, 0) {
		t.Fatal("elapsed timer returned false")
	}
}

func TestWaitForInventoryCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	if waitForInventory(ctx, 6*time.Hour) {
		t.Fatal("canceled wait returned true")
	}
}

func TestWaitForInventoryCancellationWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	done := make(chan bool, 1)

	go func() {
		done <- waitForInventory(ctx, 6*time.Hour)
	}()

	cancel()

	select {
	case result := <-done:
		if result {
			t.Fatal("canceled wait returned true")
		}

	case <-time.After(time.Second):
		t.Fatal("inventory wait did not stop after cancellation")
	}
}
