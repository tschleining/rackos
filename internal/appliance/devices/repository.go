package devices

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	ErrNotFound      = errors.New("device not found")
	ErrAlreadyExists = errors.New("device already exists")
	ErrInvalidDevice = errors.New("invalid device")
)

var idPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

const deviceColumns = `
	id, hostname, os, architecture, agent_version,
	created_at, updated_at, last_seen_at
`

type Repository struct {
	db  *sql.DB
	now func() time.Time
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db:  db,
		now: time.Now,
	}
}

func (r *Repository) Create(
	ctx context.Context,
	id string,
	info DeviceInfo,
) (Device, error) {
	if !idPattern.MatchString(id) {
		return Device{}, fmt.Errorf(
			"%w: invalid device ID", ErrInvalidDevice,
		)
	}

	info.Hostname = strings.TrimSpace(info.Hostname)
	info.OS = strings.TrimSpace(info.OS)
	info.Architecture = strings.TrimSpace(info.Architecture)
	info.AgentVersion = strings.TrimSpace(info.AgentVersion)

	if info.Hostname == "" ||
		info.OS == "" ||
		info.Architecture == "" ||
		info.AgentVersion == "" {
		return Device{}, fmt.Errorf(
			"%w: device information cannot be empty",
			ErrInvalidDevice,
		)
	}

	now := r.now().UnixMilli()

	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO devices (
			id, hostname, os, architecture,
			agent_version, created_at,
			updated_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING`,
		id,
		info.Hostname,
		info.OS,
		info.Architecture,
		info.AgentVersion,
		now,
		now,
		now,
	)

	if err != nil {
		return Device{}, fmt.Errorf("create device: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return Device{}, fmt.Errorf("read insert result: %w", err)
	}

	if affected == 0 {
		return Device{}, ErrAlreadyExists
	}

	timestamp := time.UnixMilli(now).UTC()

	return Device{
		ID:         id,
		DeviceInfo: info,
		CreatedAt:  timestamp,
		UpdatedAt:  timestamp,
		LastSeenAt: timestamp,
	}, nil
}

func (r *Repository) Get(
	ctx context.Context,
	id string,
) (Device, error) {
	row := r.db.QueryRowContext(
		ctx,
		"SELECT "+deviceColumns+" FROM devices WHERE id = ?",
		id,
	)

	device, err := scanDevice(row)

	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}

	if err != nil {
		return Device{}, fmt.Errorf("get device: %w", err)
	}

	return device, nil
}

func (r *Repository) List(
	ctx context.Context,
) ([]Device, error) {
	rows, err := r.db.QueryContext(
		ctx,
		"SELECT "+deviceColumns+
			" FROM devices ORDER BY created_at, id",
	)

	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	devices := make([]Device, 0)

	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}

		devices = append(devices, device)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate devices: %w", err)
	}

	return devices, nil
}

func (r *Repository) Heartbeat(
	ctx context.Context,
	id string,
) error {
	now := r.now().UnixMilli()

	result, err := r.db.ExecContext(
		ctx,
		`UPDATE devices
		 SET last_seen_at = MAX(last_seen_at, ?),
		     updated_at = MAX(updated_at, ?)
		 WHERE id = ?`,
		now,
		now,
		id,
	)

	if err != nil {
		return fmt.Errorf("update heartbeat: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read heartbeat result: %w", err)
	}

	if affected == 0 {
		return ErrNotFound
	}

	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanDevice(row scanner) (Device, error) {
	var device Device
	var createdAt int64
	var updatedAt int64
	var lastSeenAt int64

	err := row.Scan(
		&device.ID,
		&device.Hostname,
		&device.OS,
		&device.Architecture,
		&device.AgentVersion,
		&createdAt,
		&updatedAt,
		&lastSeenAt,
	)

	if err != nil {
		return Device{}, err
	}

	device.CreatedAt = time.UnixMilli(createdAt).UTC()
	device.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	device.LastSeenAt = time.UnixMilli(lastSeenAt).UTC()

	return device, nil
}
