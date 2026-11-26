package inventory

import "time"

// hardwareDetails contains platform-specific system
// and hardware inventory information.
//
// Empty strings and zero numeric values indicate
// information that could not be determined.
type hardwareDetails struct {
	CPUModel        string
	CPUManufacturer string
	PhysicalCPUs    int
	HostLogicalCPUs int
	MemoryBytes     uint64
	HardwareModel   string
	OSVersion       string
	KernelVersion   string
	BootTime        time.Time
}
