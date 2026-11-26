package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// executor allows the repository to operate using either
// a database connection or an existing transaction.
type executor interface {
	ExecContext(
		ctx context.Context,
		query string,
		args ...any,
	) (sql.Result, error)

	QueryRowContext(
		ctx context.Context,
		query string,
		args ...any,
	) *sql.Row
}

// Repository manages persistent hardware inventory.
type Repository struct {
	db executor
}

// NewRepository creates an inventory repository backed
// by the appliance database.
func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db: db,
	}
}

// WithTx creates a repository bound to an existing
// database transaction.
//
// The caller remains responsible for committing or
// rolling back the transaction.
func (r *Repository) WithTx(tx *sql.Tx) *Repository {
	return &Repository{
		db: tx,
	}
}

const inventoryColumns = `
	device_id,
	hostname,
	os,
	architecture,
	logical_cpus,
	host_logical_cpus,
	physical_cores,
	cpu_model,
	cpu_manufacturer,
	memory_bytes,
	hardware_model,
	os_version,
	kernel_version,
	boot_time,
	collected_at,
	received_at
`

// Get retrieves the latest stored inventory snapshot
// for a registered device.
//
// ErrNotFound is returned when no inventory exists.
func (r *Repository) Get(
	ctx context.Context,
	deviceID string,
) (Inventory, error) {
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}

	if strings.TrimSpace(deviceID) == "" {
		return Inventory{}, fmt.Errorf(
			"%w: device ID is required",
			ErrInvalidInventory,
		)
	}

	query := `
		SELECT ` + inventoryColumns + `
		FROM device_inventory
		WHERE device_id = ?
	`

	result, err := scanInventory(
		r.db.QueryRowContext(
			ctx,
			query,
			deviceID,
		),
	)

	if errors.Is(err, sql.ErrNoRows) {
		return Inventory{}, ErrNotFound
	}

	if err != nil {
		return Inventory{}, fmt.Errorf(
			"get device inventory: %w",
			err,
		)
	}

	return result, nil
}

// scanner allows inventory records to be decoded from
// both individual SQL rows and future result sets.
type scanner interface {
	Scan(dest ...any) error
}

// scanInventory converts a SQLite inventory record into
// the persistent appliance inventory model.
func scanInventory(row scanner) (Inventory, error) {
	var result Inventory

	var (
		bootTime    int64
		collectedAt int64
		receivedAt  int64
	)

	err := row.Scan(
		&result.DeviceID,
		&result.Hostname,
		&result.OS,
		&result.Architecture,
		&result.LogicalCPUs,
		&result.HostLogicalCPUs,
		&result.PhysicalCores,
		&result.CPUModel,
		&result.CPUManufacturer,
		&result.MemoryBytes,
		&result.HardwareModel,
		&result.OSVersion,
		&result.KernelVersion,
		&bootTime,
		&collectedAt,
		&receivedAt,
	)
	if err != nil {
		return Inventory{}, err
	}

	if collectedAt <= 0 ||
		receivedAt <= 0 ||
		bootTime < 0 ||
		bootTime > collectedAt {
		return Inventory{}, errors.New(
			"stored inventory contains invalid timestamps",
		)
	}

	if bootTime != 0 {
		boot := time.UnixMilli(bootTime).UTC()
		result.BootTime = &boot
	}

	result.CollectedAt = time.UnixMilli(
		collectedAt,
	).UTC()

	result.ReceivedAt = time.UnixMilli(
		receivedAt,
	).UTC()

	return result, nil
}
