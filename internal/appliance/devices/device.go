package devices

import "time"

const DefaultOfflineTimeout = 90 * time.Second

type Status string

const (
	StatusOnline  Status = "online"
	StatusOffline Status = "offline"
)

type DeviceInfo struct {
	Hostname     string
	OS           string
	Architecture string
	AgentVersion string
}

type Device struct {
	ID string

	DeviceInfo

	CreatedAt  time.Time
	UpdatedAt  time.Time
	LastSeenAt time.Time
}

func (d Device) Status(
	now time.Time,
	timeout time.Duration,
) Status {
	if timeout <= 0 || d.LastSeenAt.IsZero() {
		return StatusOffline
	}

	age := now.Sub(d.LastSeenAt)

	if age >= 0 && age < timeout {
		return StatusOnline
	}

	return StatusOffline
}
