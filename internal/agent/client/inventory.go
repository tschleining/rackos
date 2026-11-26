package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/agent/inventory"
	"google.golang.org/grpc/metadata"
)

// inventoryToProto converts a locally collected inventory
// snapshot into the agent reporting protocol.
func inventoryToProto(
	snapshot inventory.Inventory,
) (*rackosv1.HardwareInventory, error) {
	logicalCPUs, err := inventoryCPUCount(
		"logical_cpus",
		snapshot.LogicalCPUs,
		true,
	)
	if err != nil {
		return nil, err
	}

	hostLogicalCPUs, err := inventoryCPUCount(
		"host_logical_cpus",
		snapshot.HostLogicalCPUs,
		false,
	)
	if err != nil {
		return nil, err
	}

	physicalCores, err := inventoryCPUCount(
		"physical_cores",
		snapshot.PhysicalCores,
		false,
	)
	if err != nil {
		return nil, err
	}

	if hostLogicalCPUs != 0 &&
		physicalCores > hostLogicalCPUs {
		return nil, fmt.Errorf(
			"physical cores exceed host logical CPUs",
		)
	}

	collectedAt, err := inventoryTimestamp(
		"collected_at",
		snapshot.CollectedAt,
	)
	if err != nil {
		return nil, err
	}

	var bootTime int64

	if snapshot.BootTime != nil {
		bootTime, err = inventoryTimestamp(
			"boot_time",
			*snapshot.BootTime,
		)
		if err != nil {
			return nil, err
		}

		if bootTime > collectedAt {
			return nil, fmt.Errorf(
				"boot time occurs after inventory collection",
			)
		}
	}

	return &rackosv1.HardwareInventory{
		Hostname:              snapshot.Hostname,
		Os:                    snapshot.OS,
		Architecture:          snapshot.Architecture,
		LogicalCpus:           logicalCPUs,
		HostLogicalCpus:       hostLogicalCPUs,
		PhysicalCores:         physicalCores,
		CpuModel:              snapshot.CPUModel,
		CpuManufacturer:       snapshot.CPUManufacturer,
		MemoryBytes:           snapshot.MemoryBytes,
		HardwareModel:         snapshot.HardwareModel,
		OsVersion:             snapshot.OSVersion,
		KernelVersion:         snapshot.KernelVersion,
		BootTimeUnixMillis:    bootTime,
		CollectedAtUnixMillis: collectedAt,
	}, nil
}

// inventoryCPUCount safely converts a signed Go integer
// into an unsigned Protocol Buffers CPU count.
func inventoryCPUCount(
	name string,
	value int,
	required bool,
) (uint32, error) {
	const maxUint32 = uint64(1<<32 - 1)

	if value < 0 || uint64(value) > maxUint32 {
		return 0, fmt.Errorf(
			"%s is outside supported integer range",
			name,
		)
	}

	if required && value == 0 {
		return 0, fmt.Errorf(
			"%s must be positive",
			name,
		)
	}

	return uint32(value), nil
}

// inventoryTimestamp converts a timestamp into Unix
// milliseconds while rejecting unsupported dates.
func inventoryTimestamp(
	name string,
	value time.Time,
) (int64, error) {
	value = value.UTC()

	if value.IsZero() ||
		value.Year() < 1970 ||
		value.Year() > 9999 {
		return 0, fmt.Errorf(
			"%s is outside supported timestamp range",
			name,
		)
	}

	millis := value.UnixMilli()

	if millis <= 0 {
		return 0, fmt.Errorf(
			"%s must be a positive Unix timestamp",
			name,
		)
	}

	return millis, nil
}

const inventoryReportTimeout = 10 * time.Second

// ReportInventory sends a hardware inventory snapshot
// to the appliance using the agent's existing credential.
//
// Successful acceptance currently indicates validation,
// not persistent inventory storage.
func (c *Client) ReportInventory(
	ctx context.Context,
	deviceID string,
	token string,
	snapshot inventory.Inventory,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if deviceID == "" {
		return errors.New("device ID is required")
	}

	if token == "" {
		return errors.New("agent credential is required")
	}

	message, err := inventoryToProto(snapshot)
	if err != nil {
		return fmt.Errorf(
			"convert hardware inventory: %w",
			err,
		)
	}

	ctx, cancel := context.WithTimeout(
		ctx,
		inventoryReportTimeout,
	)
	defer cancel()

	// Preserve existing outgoing metadata while
	// ensuring exactly one authorization value.
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()

	md.Set(
		"authorization",
		"Bearer "+token,
	)

	ctx = metadata.NewOutgoingContext(ctx, md)

	response, err := c.service.ReportInventory(
		ctx,
		&rackosv1.ReportInventoryRequest{
			DeviceId:  deviceID,
			Inventory: message,
		},
	)
	if err != nil {
		return err
	}

	if response == nil || !response.GetAccepted() {
		return errors.New(
			"appliance did not accept hardware inventory",
		)
	}

	return nil
}
