package client

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/agent/metrics"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	performanceRefreshInterval = time.Minute
	performanceCollectionLimit = 10 * time.Second

	performanceRetryInitial = 5 * time.Second
	performanceRetryMaximum = time.Minute

	performanceMaxPendingAge = 15 * time.Minute
)

type performanceReportAction uint8

const (
	performanceRetry performanceReportAction = iota
	performanceDiscard
	performanceStop
	performanceUnsupported
)

// RunPerformance runs periodic performance collection
// and reporting until the context is canceled or
// a fatal reporting error occurs.
//
// Only one performance report is retained in memory.
func (c *Client) RunPerformance(
	ctx context.Context,
	deviceID string,
	token string,
	logger *slog.Logger,
) error {
	if ctx.Err() != nil {
		return nil
	}

	if err := validatePerformanceDeviceID(deviceID); err != nil {
		return err
	}

	if strings.TrimSpace(token) == "" {
		return ErrInvalidPerformanceReport
	}

	return runPerformanceReporting(
		ctx,
		deviceID,
		metrics.Collect,
		func(
			ctx context.Context,
			request *rackosv1.ReportPerformanceRequest,
		) (time.Time, error) {
			return c.ReportPerformance(
				ctx,
				token,
				request,
			)
		},
		wait,
		time.Now,
		logger,
	)
}

// runPerformanceReporting collects and reports
// performance measurements sequentially.
//
// Collection, reporting, waiting and clock functions
// are injected for deterministic scheduler testing.
func runPerformanceReporting(
	ctx context.Context,
	deviceID string,
	collect func(context.Context) (metrics.Snapshot, error),
	report func(
		context.Context,
		*rackosv1.ReportPerformanceRequest,
	) (time.Time, error),
	wait func(context.Context, time.Duration) bool,
	now func() time.Time,
	logger *slog.Logger,
) error {
	retryDelay := performanceRetryInitial

	var (
		pending      *rackosv1.ReportPerformanceRequest
		pendingSince time.Time
	)

	for {
		if ctx.Err() != nil {
			return nil
		}

		// Pending age is measured using the local
		// monotonic clock in production.
		if pending != nil &&
			now().Sub(pendingSince) >= performanceMaxPendingAge {
			logger.Warn(
				"pending performance report expired",
			)

			pending = nil
			retryDelay = performanceRetryInitial
		}

		if pending == nil {
			collectionCtx, cancel := context.WithTimeout(
				ctx,
				performanceCollectionLimit,
			)

			snapshot, err := collect(collectionCtx)

			collectionErr := collectionCtx.Err()
			cancel()

			if ctx.Err() != nil {
				return nil
			}

			// Do not accept a collection that
			// completed after its deadline.
			if err == nil && collectionErr != nil {
				err = collectionErr
			}

			if errors.Is(
				err,
				metrics.ErrUnsupportedPlatform,
			) {
				logger.Warn(
					"performance collection is unsupported on this platform",
				)

				return nil
			}

			if err != nil {
				logger.Warn(
					"performance collection failed",
					"retry_in", retryDelay,
				)

				if !wait(ctx, retryDelay) {
					return nil
				}

				retryDelay = nextPerformanceRetry(
					retryDelay,
				)

				continue
			}

			request, err := NewPerformanceRequest(
				deviceID,
				snapshot,
			)
			if err != nil {
				logger.Warn(
					"performance report construction failed",
					"recollect_in",
					performanceRefreshInterval,
				)

				retryDelay = performanceRetryInitial

				if !wait(
					ctx,
					performanceRefreshInterval,
				) {
					return nil
				}

				continue
			}

			pending = request
			pendingSince = now()

			retryDelay = performanceRetryInitial
		}

		// Do not begin a report that has already
		// exceeded its maximum pending age.
		remaining := performanceMaxPendingAge -
			now().Sub(pendingSince)

		if remaining <= 0 {
			continue
		}

		// Bound the RPC by both the reporting timeout
		// and the remaining lifetime of this sample.
		reportCtx, cancel := context.WithTimeout(
			ctx,
			min(performanceReportTimeout, remaining),
		)

		_, err := report(
			reportCtx,
			pending,
		)

		cancel()

		if ctx.Err() != nil {
			return nil
		}

		if err == nil {
			logger.Debug(
				"performance report acknowledged",
				"next_report_in",
				performanceRefreshInterval,
			)

			pending = nil
			retryDelay = performanceRetryInitial

			if !wait(
				ctx,
				performanceRefreshInterval,
			) {
				return nil
			}

			continue
		}

		action := classifyPerformanceReportError(err)

		switch action {
		case performanceStop:
			code := status.Code(err)

			logger.Error(
				"performance authentication rejected",
				"code", code.String(),
			)

			return status.Error(
				code,
				"performance authentication rejected",
			)

		case performanceUnsupported:
			logger.Warn(
				"appliance does not support performance reporting",
			)

			return nil

		case performanceDiscard:
			logger.Warn(
				"performance report rejected",
				"code", status.Code(err).String(),
				"recollect_in",
				performanceRefreshInterval,
			)

			pending = nil
			retryDelay = performanceRetryInitial

			if !wait(
				ctx,
				performanceRefreshInterval,
			) {
				return nil
			}

		case performanceRetry:
			// Preserve the pending request and its
			// original sample ID for the next attempt.
			remaining := performanceMaxPendingAge -
				now().Sub(pendingSince)

			if remaining <= 0 {
				continue
			}

			delay := min(
				retryDelay,
				remaining,
			)

			logger.Warn(
				"temporary performance reporting failure",
				"code", status.Code(err).String(),
				"retry_in", delay,
			)

			if !wait(ctx, delay) {
				return nil
			}

			retryDelay = nextPerformanceRetry(
				retryDelay,
			)
		}
	}
}

// classifyPerformanceReportError determines whether
// a failed report should be retried or discarded.
//
// Authentication failures are fatal. Unsupported
// protocol versions disable performance reporting.
func classifyPerformanceReportError(
	err error,
) performanceReportAction {
	switch {
	case errors.Is(err, ErrInvalidPerformanceReport),
		errors.Is(err, ErrPerformanceReportTooLarge):

		return performanceDiscard

	case errors.Is(
		err,
		ErrInvalidPerformanceAcknowledgement,
	):
		// The appliance might already have committed
		// the sample. Preserve its original identity.
		return performanceRetry

	case errors.Is(err, context.Canceled),
		errors.Is(err, context.DeadlineExceeded):

		// Parent cancellation is handled separately
		// by the reporting worker.
		return performanceRetry
	}

	switch status.Code(err) {
	case codes.Unauthenticated,
		codes.PermissionDenied:

		return performanceStop

	case codes.Unimplemented:
		return performanceUnsupported

	case codes.InvalidArgument,
		codes.AlreadyExists,
		codes.FailedPrecondition:

		return performanceDiscard

	case codes.Unavailable,
		codes.DeadlineExceeded,
		codes.Internal,
		codes.ResourceExhausted,
		codes.Aborted,
		codes.Canceled,
		codes.Unknown:

		return performanceRetry

	default:
		return performanceDiscard
	}
}

// nextPerformanceRetry doubles the retry interval
// without exceeding the configured maximum.
func nextPerformanceRetry(
	current time.Duration,
) time.Duration {
	if current <= 0 {
		return performanceRetryInitial
	}

	if current >= performanceRetryMaximum/2 {
		return performanceRetryMaximum
	}

	return current * 2
}
