package client

import (
	"context"
	"errors"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func testMonitoringContext(
	t *testing.T,
) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)

	t.Cleanup(cancel)

	return ctx
}

func TestMonitoringWorkersCanceledBeforeStartup(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	// Neither worker should be invoked.
	err := runMonitoringWorkers(
		ctx,
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMonitoringWorkersGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(
		testMonitoringContext(t),
	)
	defer cancel()

	performanceStarted := make(chan struct{})
	performanceStopped := make(chan struct{})

	coreStopped := false

	err := runMonitoringWorkers(
		ctx,
		func(ctx context.Context) error {
			// Ensure both workers are running before
			// canceling the parent context.
			select {
			case <-performanceStarted:
			case <-ctx.Done():
				return ctx.Err()
			}

			cancel()

			<-ctx.Done()

			coreStopped = true

			return nil
		},
		func(ctx context.Context) error {
			close(performanceStarted)

			<-ctx.Done()

			close(performanceStopped)

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !coreStopped {
		t.Fatal("core monitoring did not stop")
	}

	select {
	case <-performanceStopped:
	default:
		t.Fatal("supervisor returned before performance stopped")
	}
}

func TestMonitoringWorkersCoreFailure(t *testing.T) {
	ctx := testMonitoringContext(t)

	expected := errors.New(
		"simulated core monitoring failure",
	)

	performanceStarted := make(chan struct{})
	performanceStopped := make(chan struct{})

	err := runMonitoringWorkers(
		ctx,
		func(ctx context.Context) error {
			select {
			case <-performanceStarted:
			case <-ctx.Done():
				return ctx.Err()
			}

			return expected
		},
		func(ctx context.Context) error {
			close(performanceStarted)

			<-ctx.Done()

			close(performanceStopped)

			return nil
		},
	)

	if !errors.Is(err, expected) {
		t.Fatalf(
			"expected core failure, got %v",
			err,
		)
	}

	select {
	case <-performanceStopped:
	default:
		t.Fatal("performance worker was not stopped")
	}

	if ctx.Err() != nil {
		t.Fatal("supervisor canceled its parent context")
	}
}

func TestMonitoringWorkersPerformanceAuthenticationFailure(
	t *testing.T,
) {
	for _, code := range []codes.Code{
		codes.Unauthenticated,
		codes.PermissionDenied,
	} {
		t.Run(code.String(), func(t *testing.T) {
			ctx := testMonitoringContext(t)

			coreStarted := make(chan struct{})

			coreStopped := false

			expected := status.Error(
				code,
				"performance authentication rejected",
			)

			err := runMonitoringWorkers(
				ctx,
				func(ctx context.Context) error {
					close(coreStarted)

					<-ctx.Done()

					coreStopped = true

					return nil
				},
				func(ctx context.Context) error {
					select {
					case <-coreStarted:
					case <-ctx.Done():
						return ctx.Err()
					}

					return expected
				},
			)

			if status.Code(err) != code {
				t.Fatalf(
					"expected status %s, got %v",
					code,
					err,
				)
			}

			if !coreStopped {
				t.Fatal("core monitoring was not stopped")
			}

			if ctx.Err() != nil {
				t.Fatal("supervisor canceled its parent context")
			}
		})
	}
}

func TestMonitoringWorkersOptionalPerformanceExit(
	t *testing.T,
) {
	ctx := testMonitoringContext(t)

	performanceReturning := make(chan struct{})

	coreContinued := false

	err := runMonitoringWorkers(
		ctx,
		func(ctx context.Context) error {
			select {
			case <-performanceReturning:
			case <-ctx.Done():
				return errors.New(
					"core monitoring canceled prematurely",
				)
			}

			// Allow the optional worker to exit.
			// Core monitoring must remain active.
			timer := time.NewTimer(
				25 * time.Millisecond,
			)
			defer timer.Stop()

			select {
			case <-ctx.Done():
				return errors.New(
					"optional performance exit canceled core monitoring",
				)

			case <-timer.C:
				if ctx.Err() != nil {
					return errors.New(
						"core monitoring was canceled",
					)
				}

				coreContinued = true

				return nil
			}
		},
		func(context.Context) error {
			close(performanceReturning)

			// Simulate unsupported performance collection
			// or an Unimplemented reporting RPC.
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !coreContinued {
		t.Fatal("core monitoring did not continue")
	}
}

func TestMonitoringWorkersNormalCoreExit(t *testing.T) {
	ctx := testMonitoringContext(t)

	performanceStarted := make(chan struct{})
	performanceStopped := make(chan struct{})

	err := runMonitoringWorkers(
		ctx,
		func(ctx context.Context) error {
			select {
			case <-performanceStarted:
			case <-ctx.Done():
				return ctx.Err()
			}

			return nil
		},
		func(ctx context.Context) error {
			close(performanceStarted)

			<-ctx.Done()

			close(performanceStopped)

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case <-performanceStopped:
	default:
		t.Fatal("performance worker continued after core exit")
	}
}

func TestMonitoringWorkersBothFail(t *testing.T) {
	ctx := testMonitoringContext(t)

	coreFailure := errors.New(
		"core monitoring failed",
	)

	performanceFailure := status.Error(
		codes.Unauthenticated,
		"performance authentication rejected",
	)

	performanceStarted := make(chan struct{})

	err := runMonitoringWorkers(
		ctx,
		func(ctx context.Context) error {
			select {
			case <-performanceStarted:
			case <-ctx.Done():
			}

			return coreFailure
		},
		func(context.Context) error {
			close(performanceStarted)

			return performanceFailure
		},
	)

	// A fatal performance error takes precedence
	// when both workers return an error.
	if !errors.Is(err, performanceFailure) {
		t.Fatalf(
			"expected performance failure, got %v",
			err,
		)
	}
}

func TestMonitoringWorkersCancelsPendingPerformance(
	t *testing.T,
) {
	ctx := testMonitoringContext(t)

	h := newPerformanceWorkerHarness(t, 1)

	retryStarted := make(chan struct{})

	coreFailure := errors.New(
		"simulated heartbeat failure",
	)

	err := runMonitoringWorkers(
		ctx,
		func(ctx context.Context) error {
			// Wait until performance reporting has
			// encountered a temporary RPC failure.
			select {
			case <-retryStarted:
			case <-ctx.Done():
				return ctx.Err()
			}

			return coreFailure
		},
		func(ctx context.Context) error {
			return runPerformanceReporting(
				ctx,
				performanceReportingDeviceID,
				h.collect,
				func(
					ctx context.Context,
					request *rackosv1.ReportPerformanceRequest,
				) (time.Time, error) {
					_, err := h.report(
						ctx,
						request,
					)
					if err != nil {
						return time.Time{}, err
					}

					return time.Time{}, status.Error(
						codes.Unavailable,
						"simulated appliance outage",
					)
				},
				func(
					ctx context.Context,
					delay time.Duration,
				) bool {
					h.waits = append(
						h.waits,
						delay,
					)

					close(retryStarted)

					// Core failure must interrupt
					// the pending retry.
					<-ctx.Done()

					return false
				},
				func() time.Time {
					return h.now
				},
				testPerformanceWorkerLogger(),
			)
		},
	)

	if !errors.Is(err, coreFailure) {
		t.Fatalf(
			"expected core failure, got %v",
			err,
		)
	}

	// The pending performance report must not be
	// retried after monitoring terminates.
	h.requireCounts(1, 1)

	h.requireWaits(
		performanceRetryInitial,
	)
}
