package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
)

// Upsert inserts or replaces a device's latest inventory.
//
// Older snapshots are rejected. Identical retries are
// accepted without modifying the original record.
//
// ReceivedAt must be supplied by the appliance, not
// by the reporting agent.
//
// When bound to an existing transaction, the caller
// remains responsible for committing or rolling back.
func (r *Repository) Upsert(
	ctx context.Context,
	record Inventory,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	normalized, err := normalizeInventory(record)
	if err != nil {
		return err
	}

	switch db := r.db.(type) {
	case *sql.DB:
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf(
				"begin inventory transaction: %w",
				err,
			)
		}
		defer tx.Rollback()

		if err := r.WithTx(tx).upsert(
			ctx,
			normalized,
		); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf(
				"commit inventory transaction: %w",
				err,
			)
		}

		return nil

	case *sql.Tx:
		return r.upsert(ctx, normalized)

	default:
		return fmt.Errorf(
			"unsupported inventory repository executor",
		)
	}
}

// upsert performs the inventory write inside an existing
// transaction.
func (r *Repository) upsert(
	ctx context.Context,
	record Inventory,
) error {
	var bootTime int64

	if record.BootTime != nil {
		bootTime = record.BootTime.UnixMilli()
	}

	query := `
		INSERT INTO device_inventory (
			` + inventoryColumns + `
		)
		VALUES (
			?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?
		)
		ON CONFLICT(device_id) DO UPDATE SET
			hostname = excluded.hostname,
			os = excluded.os,
			architecture = excluded.architecture,
			logical_cpus = excluded.logical_cpus,
			host_logical_cpus = excluded.host_logical_cpus,
			physical_cores = excluded.physical_cores,
			cpu_model = excluded.cpu_model,
			cpu_manufacturer = excluded.cpu_manufacturer,
			memory_bytes = excluded.memory_bytes,
			hardware_model = excluded.hardware_model,
			os_version = excluded.os_version,
			kernel_version = excluded.kernel_version,
			boot_time = excluded.boot_time,
			collected_at = excluded.collected_at,
			received_at = excluded.received_at
		WHERE excluded.collected_at >
			device_inventory.collected_at
	`

	result, err := r.db.ExecContext(
		ctx,
		query,
		record.DeviceID,
		record.Hostname,
		record.OS,
		record.Architecture,
		record.LogicalCPUs,
		record.HostLogicalCPUs,
		record.PhysicalCores,
		record.CPUModel,
		record.CPUManufacturer,
		int64(record.MemoryBytes),
		record.HardwareModel,
		record.OSVersion,
		record.KernelVersion,
		bootTime,
		record.CollectedAt.UnixMilli(),
		record.ReceivedAt.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf(
			"upsert device inventory: %w",
			err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf(
			"read inventory affected rows: %w",
			err,
		)
	}

	switch affected {
	case 1:
		return nil

	case 0:
		return r.resolveInventoryConflict(
			ctx,
			record,
		)

	default:
		return fmt.Errorf(
			"unexpected inventory affected row count: %d",
			affected,
		)
	}
}

// resolveInventoryConflict determines why SQLite did
// not insert or replace an inventory record.
func (r *Repository) resolveInventoryConflict(
	ctx context.Context,
	incoming Inventory,
) error {
	existing, err := r.Get(
		ctx,
		incoming.DeviceID,
	)
	if err != nil {
		return fmt.Errorf(
			"resolve inventory conflict: %w",
			err,
		)
	}

	if incoming.CollectedAt.Before(
		existing.CollectedAt,
	) {
		return ErrStaleInventory
	}

	if incoming.CollectedAt.Equal(
		existing.CollectedAt,
	) {
		if sameInventorySnapshot(
			incoming,
			existing,
		) {
			return nil
		}

		return ErrInventoryConflict
	}

	return fmt.Errorf(
		"inventory write was unexpectedly skipped",
	)
}

// sameInventorySnapshot compares hardware snapshots
// while ignoring their appliance receipt timestamps.
func sameInventorySnapshot(
	first Inventory,
	second Inventory,
) bool {
	if (first.BootTime == nil) !=
		(second.BootTime == nil) {
		return false
	}

	if first.BootTime != nil &&
		!first.BootTime.Equal(*second.BootTime) {
		return false
	}

	// Ignore pointer identity and receipt timestamps.
	first.BootTime = nil
	second.BootTime = nil

	first.ReceivedAt = time.Time{}
	second.ReceivedAt = time.Time{}

	return first == second
}

// normalizeInventory validates the persistent model
// and normalizes timestamps to SQLite's millisecond
// precision.
func normalizeInventory(
	record Inventory,
) (Inventory, error) {
	if record.DeviceID == "" ||
		strings.TrimSpace(record.DeviceID) != record.DeviceID {
		return Inventory{}, invalid(
			"device ID is required and must not contain surrounding whitespace",
		)
	}

	collectedAt, err := normalizeInventoryTime(
		"collected_at",
		record.CollectedAt,
	)
	if err != nil {
		return Inventory{}, err
	}

	receivedAt, err := normalizeInventoryTime(
		"received_at",
		record.ReceivedAt,
	)
	if err != nil {
		return Inventory{}, err
	}

	record.CollectedAt = collectedAt
	record.ReceivedAt = receivedAt

	var bootTime int64

	if record.BootTime != nil {
		normalizedBoot, err := normalizeInventoryTime(
			"boot_time",
			*record.BootTime,
		)
		if err != nil {
			return Inventory{}, err
		}

		record.BootTime = &normalizedBoot
		bootTime = normalizedBoot.UnixMilli()
	}

	// Reuse the existing inventory validator so protocol
	// acceptance and persistent storage enforce the
	// same hardware and timestamp constraints.
	snapshot := &rackosv1.HardwareInventory{
		Hostname:              record.Hostname,
		Os:                    record.OS,
		Architecture:          record.Architecture,
		LogicalCpus:           record.LogicalCPUs,
		HostLogicalCpus:       record.HostLogicalCPUs,
		PhysicalCores:         record.PhysicalCores,
		CpuModel:              record.CPUModel,
		CpuManufacturer:       record.CPUManufacturer,
		MemoryBytes:           record.MemoryBytes,
		HardwareModel:         record.HardwareModel,
		OsVersion:             record.OSVersion,
		KernelVersion:         record.KernelVersion,
		BootTimeUnixMillis:    bootTime,
		CollectedAtUnixMillis: collectedAt.UnixMilli(),
	}

	if err := Validate(
		snapshot,
		receivedAt,
	); err != nil {
		return Inventory{}, err
	}

	return record, nil
}

func normalizeInventoryTime(
	name string,
	value time.Time,
) (time.Time, error) {
	value = value.UTC()

	if value.IsZero() ||
		value.Year() < 1970 ||
		value.Year() > 9999 {
		return time.Time{}, invalid(
			name + " is outside supported timestamp range",
		)
	}

	millis := value.UnixMilli()

	if millis <= 0 {
		return time.Time{}, invalid(
			name + " must be a positive Unix timestamp",
		)
	}

	return time.UnixMilli(millis).UTC(), nil
}
