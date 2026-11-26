package inventory

import (
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New(
		"device inventory not found",
	)

	ErrStaleInventory = errors.New(
		"inventory snapshot is older than stored inventory",
	)

	ErrInventoryConflict = errors.New(
		"inventory snapshot conflicts with stored inventory",
	)
)

// Inventory represents the latest hardware inventory
// snapshot stored for a registered device.
//
// Optional numeric fields use zero when unavailable.
// Optional text fields use an empty string when unavailable.
//
// All timestamps are represented in UTC.
type Inventory struct {
	DeviceID string `json:"device_id"`

	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`

	LogicalCPUs     uint32 `json:"logical_cpus"`
	HostLogicalCPUs uint32 `json:"host_logical_cpus,omitempty"`
	PhysicalCores   uint32 `json:"physical_cores,omitempty"`

	CPUModel        string `json:"cpu_model,omitempty"`
	CPUManufacturer string `json:"cpu_manufacturer,omitempty"`

	MemoryBytes uint64 `json:"memory_bytes,omitempty"`

	HardwareModel string `json:"hardware_model,omitempty"`
	OSVersion     string `json:"os_version,omitempty"`
	KernelVersion string `json:"kernel_version,omitempty"`

	BootTime *time.Time `json:"boot_time,omitempty"`

	CollectedAt time.Time `json:"collected_at"`
	ReceivedAt  time.Time `json:"received_at"`
}
