package agentservice

import (
	"context"
	"errors"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/appliance/credentials"
	"github.com/tschleining/rackos/internal/appliance/inventory"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ReportInventory authenticates a registered agent and
// validates its reported hardware inventory.
//
// Inventory persistence will be implemented separately.
// Successful acceptance currently indicates validation only.
func (s *Service) ReportInventory(
	ctx context.Context,
	req *rackosv1.ReportInventoryRequest,
) (*rackosv1.ReportInventoryResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}

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

	if err := credentials.Authenticate(
		ctx, s.db, deviceID, token,
	); err != nil {
		if errors.Is(err, credentials.ErrUnauthenticated) {
			return nil, status.Error(
				codes.Unauthenticated,
				"invalid agent credentials",
			)
		}

		return nil, internalError(ctx)
	}

	if err := inventory.Validate(
		req.GetInventory(),
		time.Now().UTC(),
	); err != nil {
		if errors.Is(err, inventory.ErrInvalidInventory) {
			return nil, status.Error(
				codes.InvalidArgument,
				err.Error(),
			)
		}

		return nil, internalError(ctx)
	}

	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}

	// Persistence will be introduced in milestone 3.3.
	// Inventory reporting does not modify device records
	// or heartbeat timestamps.
	return &rackosv1.ReportInventoryResponse{
		Accepted: true,
	}, nil
}
