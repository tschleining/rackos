package inventory

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
)

const (
	maxHostnameLength     = 255
	maxPlatformLength     = 64
	maxHardwareTextLength = 256
	maxVersionLength      = 128

	maxCPUCount    = 1 << 20
	maxMemoryBytes = uint64(1<<63 - 1)

	maxFutureClockSkew = 5 * time.Minute
)

var ErrInvalidInventory = errors.New(
	"invalid hardware inventory",
)

// Validate checks an agent-supplied hardware inventory
// snapshot before it is accepted by the appliance.
//
// Validation does not authenticate the reporting device
// or establish the accuracy of its hardware claims.
//
// The supplied time must come from the appliance clock.
func Validate(
	snapshot *rackosv1.HardwareInventory,
	now time.Time,
) error {
	if snapshot == nil {
		return invalid("inventory is required")
	}

	fields := []struct {
		name     string
		value    string
		max      int
		required bool
	}{
		{
			name:     "hostname",
			value:    snapshot.GetHostname(),
			max:      maxHostnameLength,
			required: true,
		},
		{
			name:     "os",
			value:    snapshot.GetOs(),
			max:      maxPlatformLength,
			required: true,
		},
		{
			name:     "architecture",
			value:    snapshot.GetArchitecture(),
			max:      maxPlatformLength,
			required: true,
		},
		{
			name:  "cpu_model",
			value: snapshot.GetCpuModel(),
			max:   maxHardwareTextLength,
		},
		{
			name:  "cpu_manufacturer",
			value: snapshot.GetCpuManufacturer(),
			max:   maxHardwareTextLength,
		},
		{
			name:  "hardware_model",
			value: snapshot.GetHardwareModel(),
			max:   maxHardwareTextLength,
		},
		{
			name:  "os_version",
			value: snapshot.GetOsVersion(),
			max:   maxVersionLength,
		},
		{
			name:  "kernel_version",
			value: snapshot.GetKernelVersion(),
			max:   maxVersionLength,
		},
	}

	for _, field := range fields {
		if err := validateText(
			field.name,
			field.value,
			field.max,
			field.required,
		); err != nil {
			return err
		}
	}

	if err := validateCPUCounts(snapshot); err != nil {
		return err
	}

	if snapshot.GetMemoryBytes() > maxMemoryBytes {
		return invalid("memory_bytes exceeds supported range")
	}

	if err := validateTimestamps(snapshot, now); err != nil {
		return err
	}

	return nil
}

func validateText(
	name string,
	value string,
	maxLength int,
	required bool,
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

	if len(value) > maxLength {
		return invalid(name + " exceeds maximum length")
	}

	if strings.TrimSpace(value) != value {
		return invalid(
			name + " contains leading or trailing whitespace",
		)
	}

	for _, character := range value {
		if unicode.IsControl(character) {
			return invalid(
				name + " contains control characters",
			)
		}
	}

	return nil
}

func validateCPUCounts(
	snapshot *rackosv1.HardwareInventory,
) error {
	logical := snapshot.GetLogicalCpus()
	hostLogical := snapshot.GetHostLogicalCpus()
	physical := snapshot.GetPhysicalCores()

	if logical == 0 || logical > maxCPUCount {
		return invalid(
			"logical_cpus is outside supported range",
		)
	}

	if hostLogical > maxCPUCount {
		return invalid(
			"host_logical_cpus exceeds supported range",
		)
	}

	if physical > maxCPUCount {
		return invalid(
			"physical_cores exceeds supported range",
		)
	}

	if hostLogical != 0 && physical > hostLogical {
		return invalid(
			"physical_cores exceeds host_logical_cpus",
		)
	}

	return nil
}

func validateTimestamps(
	snapshot *rackosv1.HardwareInventory,
	now time.Time,
) error {
	collected := snapshot.GetCollectedAtUnixMillis()
	boot := snapshot.GetBootTimeUnixMillis()

	if collected <= 0 {
		return invalid(
			"collected_at_unix_millis must be positive",
		)
	}

	latestAllowed := now.Add(
		maxFutureClockSkew,
	).UnixMilli()

	if collected > latestAllowed {
		return invalid(
			"collection timestamp is too far in the future",
		)
	}

	if boot < 0 {
		return invalid(
			"boot_time_unix_millis cannot be negative",
		)
	}

	// A zero boot timestamp indicates unavailable
	// information. Otherwise, boot must not occur
	// after inventory collection.
	if boot != 0 && boot > collected {
		return invalid(
			"boot timestamp occurs after collection",
		)
	}

	return nil
}

func invalid(message string) error {
	return fmt.Errorf(
		"%w: %s",
		ErrInvalidInventory,
		message,
	)
}
