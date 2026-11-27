package performance

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"google.golang.org/protobuf/proto"
)

const (
	MaxRequestBytes = 512 * 1024

	maxFilesystemRecords       = 256
	maxDiskRecords             = 256
	maxDiskRateRecords         = 256
	maxNetworkInterfaceRecords = 256
	maxNetworkRateRecords      = 256

	maxMetadataLength = 1024
	sampleIDLength    = 32

	minSampleIntervalMillis = 1
	maxSampleIntervalMillis = 60000

	maxFutureClockSkew = 5 * time.Minute

	percentageTolerance = 0.000001
)

var (
	ErrInvalidPerformance = errors.New(
		"invalid performance snapshot",
	)

	ErrPerformanceTooLarge = errors.New(
		"performance request exceeds maximum size",
	)
)

// ValidateRequest validates a complete performance
// reporting request.
//
// Authentication must be performed separately before
// accepting or persisting the supplied measurements.
func ValidateRequest(
	request *rackosv1.ReportPerformanceRequest,
	now time.Time,
) error {
	if request == nil {
		return invalid("request is required")
	}

	if proto.Size(request) > MaxRequestBytes {
		return ErrPerformanceTooLarge
	}

	if err := validateText(
		"device_id",
		request.DeviceId,
		true,
		false,
	); err != nil {
		return err
	}

	if err := ValidateSampleID(request.SampleId); err != nil {
		return err
	}

	return Validate(request.Snapshot, now)
}

// ValidateSampleID checks the format of an agent-generated
// performance sample identifier.
func ValidateSampleID(sampleID string) error {
	if len(sampleID) != sampleIDLength {
		return invalid("sample_id must contain 32 hexadecimal characters")
	}

	for _, character := range sampleID {
		if character >= '0' && character <= '9' {
			continue
		}

		if character >= 'a' && character <= 'f' {
			continue
		}

		return invalid("sample_id must use lowercase hexadecimal")
	}

	return nil
}

// Validate checks a complete performance snapshot.
//
// The supplied time must originate from the appliance
// clock. Validation does not authenticate the agent
// or establish the accuracy of its measurements.
func Validate(
	snapshot *rackosv1.PerformanceSnapshot,
	now time.Time,
) error {
	if snapshot == nil {
		return invalid("snapshot is required")
	}

	if err := validateTimestamp(snapshot, now); err != nil {
		return err
	}

	if err := validateCPU(snapshot.Cpu); err != nil {
		return err
	}

	if err := validateMemory(snapshot.Memory); err != nil {
		return err
	}

	if err := validateStorage(snapshot.Storage); err != nil {
		return err
	}

	if err := validateNetwork(snapshot.Network); err != nil {
		return err
	}

	return nil
}

// validateTimestamp checks collection time validity
// without rejecting legitimate historical samples.
func validateTimestamp(
	snapshot *rackosv1.PerformanceSnapshot,
	now time.Time,
) error {
	if snapshot.CollectedAt == nil {
		return invalid("collected_at is required")
	}

	if err := snapshot.CollectedAt.CheckValid(); err != nil {
		return invalid("collected_at is invalid")
	}

	collectedAt := snapshot.CollectedAt.AsTime()

	if collectedAt.UnixMilli() <= 0 {
		return invalid("collected_at must be positive")
	}

	if collectedAt.After(now.Add(maxFutureClockSkew)) {
		return invalid("collection timestamp is too far in the future")
	}

	return nil
}

func validateCPU(cpu *rackosv1.CPUPerformance) error {
	if cpu == nil {
		return invalid("cpu measurements are required")
	}

	if err := validatePercentage(
		"cpu.usage_percent",
		cpu.UsagePercent,
	); err != nil {
		return err
	}

	return validateInterval(
		"cpu.sample_interval_millis",
		cpu.SampleIntervalMillis,
	)
}

func validateMemory(
	memory *rackosv1.MemoryPerformance,
) error {
	if memory == nil {
		return invalid("memory measurements are required")
	}

	if memory.TotalBytes == 0 {
		return invalid("memory.total_bytes must be positive")
	}

	if memory.AvailableBytes > memory.TotalBytes {
		return invalid("memory.available_bytes exceeds total capacity")
	}

	expectedUsed := memory.TotalBytes - memory.AvailableBytes

	if memory.UsedBytes != expectedUsed {
		return invalid("memory.used_bytes is inconsistent")
	}

	return validateCapacityPercentage(
		"memory.usage_percent",
		memory.UsagePercent,
		memory.UsedBytes,
		memory.TotalBytes,
	)
}

func validateStorage(
	storage *rackosv1.StoragePerformance,
) error {
	if storage == nil {
		return invalid("storage measurements are required")
	}

	if len(storage.Filesystems) > maxFilesystemRecords {
		return invalid("too many filesystem records")
	}

	if len(storage.Disks) > maxDiskRecords {
		return invalid("too many disk records")
	}

	if len(storage.DiskRates) > maxDiskRateRecords {
		return invalid("too many disk rate records")
	}

	for _, filesystem := range storage.Filesystems {
		if err := validateFilesystem(filesystem); err != nil {
			return err
		}
	}

	disks := make(map[string]struct{}, len(storage.Disks))

	for _, disk := range storage.Disks {
		if disk == nil {
			return invalid("disk record is required")
		}

		if err := validateText(
			"disk.device",
			disk.Device,
			true,
			false,
		); err != nil {
			return err
		}

		if _, exists := disks[disk.Device]; exists {
			return invalid("duplicate disk device")
		}

		disks[disk.Device] = struct{}{}
	}

	seenRates := make(map[string]struct{}, len(storage.DiskRates))

	for _, rate := range storage.DiskRates {
		if err := validateDiskRate(rate); err != nil {
			return err
		}

		if _, exists := disks[rate.Device]; !exists {
			return invalid("disk rate has no matching disk")
		}

		if _, exists := seenRates[rate.Device]; exists {
			return invalid("duplicate disk rate")
		}

		seenRates[rate.Device] = struct{}{}
	}

	return nil
}

func validateFilesystem(
	filesystem *rackosv1.FilesystemPerformance,
) error {
	if filesystem == nil {
		return invalid("filesystem record is required")
	}

	// Filesystem paths may legitimately contain spaces,
	// including leading or trailing spaces.
	if err := validateText(
		"filesystem.source",
		filesystem.Source,
		false,
		true,
	); err != nil {
		return err
	}

	if err := validateText(
		"filesystem.mount_point",
		filesystem.MountPoint,
		true,
		true,
	); err != nil {
		return err
	}

	if err := validateText(
		"filesystem.filesystem_type",
		filesystem.FilesystemType,
		true,
		false,
	); err != nil {
		return err
	}

	return validateFilesystemCapacity(filesystem.Capacity)
}

func validateFilesystemCapacity(
	capacity *rackosv1.FilesystemCapacityPerformance,
) error {
	if capacity == nil {
		return invalid("filesystem capacity is required")
	}

	if capacity.TotalBytes == 0 {
		return invalid("filesystem.total_bytes must be positive")
	}

	if capacity.FreeBytes > capacity.TotalBytes {
		return invalid("filesystem.free_bytes exceeds total capacity")
	}

	if capacity.AvailableBytes > capacity.FreeBytes {
		return invalid("filesystem.available_bytes exceeds free capacity")
	}

	expectedUsed := capacity.TotalBytes - capacity.FreeBytes
	expectedReserved := capacity.FreeBytes - capacity.AvailableBytes

	if capacity.UsedBytes != expectedUsed {
		return invalid("filesystem.used_bytes is inconsistent")
	}

	if capacity.ReservedBytes != expectedReserved {
		return invalid("filesystem.reserved_bytes is inconsistent")
	}

	return validateCapacityPercentage(
		"filesystem.usage_percent",
		capacity.UsagePercent,
		capacity.UsedBytes,
		capacity.TotalBytes,
	)
}

func validateDiskRate(rate *rackosv1.DiskRate) error {
	if rate == nil {
		return invalid("disk rate record is required")
	}

	if err := validateText(
		"disk_rate.device",
		rate.Device,
		true,
		false,
	); err != nil {
		return err
	}

	if err := validateInterval(
		"disk_rate.sample_interval_millis",
		rate.SampleIntervalMillis,
	); err != nil {
		return err
	}

	return validateRates(
		"disk_rate",
		rate.ReadBytesPerSecond,
		rate.WriteBytesPerSecond,
		rate.ReadOperationsPerSecond,
		rate.WriteOperationsPerSecond,
	)
}

func validateNetwork(
	network *rackosv1.NetworkPerformance,
) error {
	if network == nil {
		return invalid("network measurements are required")
	}

	if len(network.Interfaces) > maxNetworkInterfaceRecords {
		return invalid("too many network interface records")
	}

	if len(network.InterfaceRates) > maxNetworkRateRecords {
		return invalid("too many network rate records")
	}

	interfaces := make(
		map[string]uint32,
		len(network.Interfaces),
	)

	indexes := make(
		map[uint32]struct{},
		len(network.Interfaces),
	)

	for _, iface := range network.Interfaces {
		if err := validateNetworkInterface(iface); err != nil {
			return err
		}

		if _, exists := interfaces[iface.Name]; exists {
			return invalid("duplicate network interface name")
		}

		if _, exists := indexes[iface.Index]; exists {
			return invalid("duplicate network interface index")
		}

		interfaces[iface.Name] = iface.Index
		indexes[iface.Index] = struct{}{}
	}

	seenRates := make(
		map[string]struct{},
		len(network.InterfaceRates),
	)

	for _, rate := range network.InterfaceRates {
		if err := validateNetworkRate(rate); err != nil {
			return err
		}

		index, exists := interfaces[rate.Name]

		if !exists || index != rate.Index {
			return invalid("network rate has no matching interface")
		}

		if _, exists := seenRates[rate.Name]; exists {
			return invalid("duplicate network interface rate")
		}

		seenRates[rate.Name] = struct{}{}
	}

	return nil
}

func validateNetworkInterface(
	iface *rackosv1.NetworkInterfacePerformance,
) error {
	if iface == nil {
		return invalid("network interface record is required")
	}

	if err := validateText(
		"network_interface.name",
		iface.Name,
		true,
		false,
	); err != nil {
		return err
	}

	if iface.Index == 0 {
		return invalid("network interface index must be positive")
	}

	if iface.LinkSpeedBitsPerSecond != nil {
		speed := *iface.LinkSpeedBitsPerSecond

		if speed == 0 || speed == math.MaxUint64 {
			return invalid("network link speed is invalid")
		}

		if !iface.Up || iface.Loopback {
			return invalid("network interface cannot report link speed")
		}
	}

	// A zero MTU is permitted.
	//
	// Dropped-packet counters may be absent or contain
	// any unsigned 64-bit value, including zero.

	return nil
}

func validateNetworkRate(
	rate *rackosv1.NetworkInterfaceRate,
) error {
	if rate == nil {
		return invalid("network rate record is required")
	}

	if err := validateText(
		"network_rate.name",
		rate.Name,
		true,
		false,
	); err != nil {
		return err
	}

	if rate.Index == 0 {
		return invalid("network rate index must be positive")
	}

	if err := validateInterval(
		"network_rate.sample_interval_millis",
		rate.SampleIntervalMillis,
	); err != nil {
		return err
	}

	return validateRates(
		"network_rate",
		rate.ReceiveBytesPerSecond,
		rate.TransmitBytesPerSecond,
		rate.ReceivePacketsPerSecond,
		rate.TransmitPacketsPerSecond,
	)
}

func validatePercentage(
	name string,
	value float64,
) error {
	if math.IsNaN(value) ||
		math.IsInf(value, 0) ||
		value < 0 ||
		value > 100 {
		return invalid(name + " is outside supported range")
	}

	return nil
}

func validateCapacityPercentage(
	name string,
	value float64,
	used uint64,
	total uint64,
) error {
	if err := validatePercentage(name, value); err != nil {
		return err
	}

	if total == 0 || used > total {
		return invalid(name + " has invalid capacity accounting")
	}

	expected := float64(used) / float64(total) * 100

	if math.Abs(value-expected) > percentageTolerance {
		return invalid(name + " is inconsistent with capacity")
	}

	return nil
}

func validateInterval(name string, millis int64) error {
	if millis < minSampleIntervalMillis ||
		millis > maxSampleIntervalMillis {
		return invalid(name + " is outside supported range")
	}

	return nil
}

func validateRates(name string, values ...float64) error {
	for _, value := range values {
		if math.IsNaN(value) ||
			math.IsInf(value, 0) ||
			value < 0 {
			return invalid(name + " contains invalid activity rates")
		}
	}

	return nil
}

func validateText(
	name string,
	value string,
	required bool,
	allowOuterWhitespace bool,
) error {
	if value == "" {
		if required {
			return invalid(name + " is required")
		}

		return nil
	}

	if !utf8.ValidString(value) {
		return invalid(name + " contains invalid UTF-8")
	}

	if len(value) > maxMetadataLength {
		return invalid(name + " exceeds maximum length")
	}

	if !allowOuterWhitespace &&
		strings.TrimSpace(value) != value {
		return invalid(name + " contains leading or trailing whitespace")
	}

	for _, character := range value {
		if unicode.IsControl(character) {
			return invalid(name + " contains control characters")
		}
	}

	return nil
}

func invalid(message string) error {
	return fmt.Errorf(
		"%w: %s",
		ErrInvalidPerformance,
		message,
	)
}
