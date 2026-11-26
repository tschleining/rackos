package inventory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

// Inventory represents a snapshot of a managed
// device's system and hardware configuration.
//
// Platform-specific fields are omitted when unavailable.
type Inventory struct {
	Hostname        string     `json:"hostname"`
	OS              string     `json:"os"`
	Architecture    string     `json:"architecture"`
	LogicalCPUs     int        `json:"logical_cpus"`
	HostLogicalCPUs int        `json:"host_logical_cpus,omitempty"`
	PhysicalCores   int        `json:"physical_cores,omitempty"`
	CPUModel        string     `json:"cpu_model,omitempty"`
	CPUManufacturer string     `json:"cpu_manufacturer,omitempty"`
	MemoryBytes     uint64     `json:"memory_bytes,omitempty"`
	HardwareModel   string     `json:"hardware_model,omitempty"`
	OSVersion       string     `json:"os_version,omitempty"`
	KernelVersion   string     `json:"kernel_version,omitempty"`
	BootTime        *time.Time `json:"boot_time,omitempty"`
	CollectedAt     time.Time  `json:"collected_at"`
}

// Collect retrieves the system inventory of the
// machine on which the RackOS agent is running.
//
// Collection is independent of enrollment, network
// connectivity, and appliance availability.
func Collect(ctx context.Context) (Inventory, error) {
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}

	hostname, err := os.Hostname()
	if err != nil {
		return Inventory{}, fmt.Errorf(
			"collect hostname: %w",
			err,
		)
	}

	hostname = strings.TrimSpace(hostname)

	if hostname == "" {
		return Inventory{}, errors.New(
			"system hostname is empty",
		)
	}

	logicalCPUs := runtime.NumCPU()

	if logicalCPUs < 1 {
		return Inventory{}, errors.New(
			"invalid logical CPU count",
		)
	}

	hardware, err := collectPlatformHardware(ctx)
	if err != nil {
		return Inventory{}, fmt.Errorf(
			"collect platform hardware: %w",
			err,
		)
	}

	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}

	result := Inventory{
		Hostname:        hostname,
		OS:              runtime.GOOS,
		Architecture:    runtime.GOARCH,
		LogicalCPUs:     logicalCPUs,
		HostLogicalCPUs: hardware.HostLogicalCPUs,
		PhysicalCores:   hardware.PhysicalCPUs,
		CPUModel:        hardware.CPUModel,
		CPUManufacturer: hardware.CPUManufacturer,
		MemoryBytes:     hardware.MemoryBytes,
		HardwareModel:   hardware.HardwareModel,
		OSVersion:       hardware.OSVersion,
		KernelVersion:   hardware.KernelVersion,
		CollectedAt:     time.Now().UTC(),
	}

	if !hardware.BootTime.IsZero() {
		bootTime := hardware.BootTime.UTC()
		result.BootTime = &bootTime
	}

	return result, nil
}
