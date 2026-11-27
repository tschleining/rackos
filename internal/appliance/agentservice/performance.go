package agentservice

import (
	"context"
	"errors"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/appliance/credentials"
	"github.com/tschleining/rackos/internal/appliance/performance"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ReportPerformance authenticates a registered agent
// and persists a historical performance snapshot.
//
// Authentication and persistence occur within the same
// database transaction.
//
// Successful acceptance indicates that the snapshot
// has been committed or is an identical retry of an
// already stored sample.
func (s *Service) ReportPerformance(
	ctx context.Context,
	req *rackosv1.ReportPerformanceRequest,
) (*rackosv1.ReportPerformanceResponse, error) {
	ctx, cancel := context.WithTimeout(
		ctx,
		requestTimeout,
	)
	defer cancel()

	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}

	receivedAt := time.Now().UTC()

	token, err := bearerToken(ctx)
	if err != nil {
		return nil, err
	}

	deviceID := req.GetDeviceId()

	if deviceID == "" {
		return nil, status.Error(
			codes.Unauthenticated,
			"invalid agent credentials",
		)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, internalError(ctx)
	}
	defer tx.Rollback()

	// Authenticate within the same transaction used
	// to persist the performance snapshot.
	if err := credentials.Authenticate(
		ctx,
		tx,
		deviceID,
		token,
	); err != nil {
		if errors.Is(
			err,
			credentials.ErrUnauthenticated,
		) {
			return nil, status.Error(
				codes.Unauthenticated,
				"invalid agent credentials",
			)
		}

		return nil, internalError(ctx)
	}

	repository := performance.NewRepository(
		s.db,
	).WithTx(tx)

	acceptedAt, err := repository.Insert(
		ctx,
		req,
		receivedAt,
	)
	if err != nil {
		if ctx.Err() != nil {
			return nil, internalError(ctx)
		}

		switch {
		case errors.Is(
			err,
			performance.ErrInvalidPerformance,
		):
			return nil, status.Error(
				codes.InvalidArgument,
				"invalid performance snapshot",
			)

		case errors.Is(
			err,
			performance.ErrPerformanceTooLarge,
		):
			return nil, status.Error(
				codes.ResourceExhausted,
				"performance request exceeds maximum size",
			)

		case errors.Is(
			err,
			performance.ErrPerformanceConflict,
		):
			return nil, status.Error(
				codes.AlreadyExists,
				"conflicting performance sample",
			)

		default:
			return nil, internalError(ctx)
		}
	}

	// Never acknowledge a report before the
	// database transaction has committed.
	if err := tx.Commit(); err != nil {
		return nil, internalError(ctx)
	}

	return &rackosv1.ReportPerformanceResponse{
		Accepted:   true,
		SampleId:   req.GetSampleId(),
		ReceivedAt: timestamppb.New(acceptedAt),
	}, nil
}
