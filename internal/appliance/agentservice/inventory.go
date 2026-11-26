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
// persists its latest hardware inventory snapshot.
//
// Authentication and persistence occur within the same
// database transaction.
//
// Successful acceptance indicates that the inventory
// has been committed or is an identical retry of an
// already stored snapshot.
func (s *Service) ReportInventory(
	ctx context.Context,
	req *rackosv1.ReportInventoryRequest,
) (*rackosv1.ReportInventoryResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
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
	// to persist the inventory snapshot.
	if err := credentials.Authenticate(
		ctx, tx, deviceID, token,
	); err != nil {
		if errors.Is(err, credentials.ErrUnauthenticated) {
			return nil, status.Error(
				codes.Unauthenticated,
				"invalid agent credentials",
			)
		}

		return nil, internalError(ctx)
	}

	snapshot := req.GetInventory()

	if err := inventory.Validate(
		snapshot,
		receivedAt,
	); err != nil {
		if errors.Is(err, inventory.ErrInvalidInventory) {
			return nil, status.Error(
				codes.InvalidArgument,
				err.Error(),
			)
		}

		return nil, internalError(ctx)
	}

	record := inventoryFromProto(
		deviceID,
		snapshot,
		receivedAt,
	)

	repository := inventory.NewRepository(s.db).WithTx(tx)

	if err := repository.Upsert(
		ctx,
		record,
	); err != nil {
		if ctx.Err() != nil {
			return nil, internalError(ctx)
		}

		switch {
		case errors.Is(err, inventory.ErrInvalidInventory):
			return nil, status.Error(
				codes.InvalidArgument,
				"invalid hardware inventory",
			)

		case errors.Is(err, inventory.ErrStaleInventory):
			return nil, status.Error(
				codes.FailedPrecondition,
				"inventory snapshot is older than stored inventory",
			)

		case errors.Is(err, inventory.ErrInventoryConflict):
			return nil, status.Error(
				codes.AlreadyExists,
				"conflicting inventory collection timestamp",
			)

		default:
			return nil, internalError(ctx)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, internalError(ctx)
	}

	return &rackosv1.ReportInventoryResponse{
		Accepted: true,
	}, nil
}

// inventoryFromProto converts a validated transport
// snapshot into the persistent appliance model.
//
// Device identity comes from the authenticated request.
// ReceivedAt comes exclusively from the appliance clock.
func inventoryFromProto(
	deviceID string,
	snapshot *rackosv1.HardwareInventory,
	receivedAt time.Time,
) inventory.Inventory {
	record := inventory.Inventory{
		DeviceID:        deviceID,
		Hostname:        snapshot.GetHostname(),
		OS:              snapshot.GetOs(),
		Architecture:    snapshot.GetArchitecture(),
		LogicalCPUs:     snapshot.GetLogicalCpus(),
		HostLogicalCPUs: snapshot.GetHostLogicalCpus(),
		PhysicalCores:   snapshot.GetPhysicalCores(),
		CPUModel:        snapshot.GetCpuModel(),
		CPUManufacturer: snapshot.GetCpuManufacturer(),
		MemoryBytes:     snapshot.GetMemoryBytes(),
		HardwareModel:   snapshot.GetHardwareModel(),
		OSVersion:       snapshot.GetOsVersion(),
		KernelVersion:   snapshot.GetKernelVersion(),

		CollectedAt: time.UnixMilli(
			snapshot.GetCollectedAtUnixMillis(),
		).UTC(),

		ReceivedAt: receivedAt.UTC(),
	}

	if snapshot.GetBootTimeUnixMillis() != 0 {
		bootTime := time.UnixMilli(
			snapshot.GetBootTimeUnixMillis(),
		).UTC()

		record.BootTime = &bootTime
	}

	return record
}
