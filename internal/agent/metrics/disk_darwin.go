package metrics

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/ebitengine/purego"
)

const (
	darwinDiskIOKitLibrary = "/System/Library/Frameworks/IOKit.framework/IOKit"

	darwinDiskCoreFoundationLibrary = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"

	darwinDiskUTF8Encoding = uint32(0x08000100)
	darwinDiskSInt64Type   = int64(4)
)

var errInvalidDarwinDiskStats = errors.New(
	"invalid macOS disk statistics",
)

// darwinDiskAPI contains the native IOKit and
// Core Foundation functions required for disk
// performance collection.
type darwinDiskAPI struct {
	serviceMatching  func(string) uintptr
	matchingServices func(uint32, uintptr, *uint32) int32
	iteratorNext     func(uint32) uint32
	iteratorIsValid  func(uint32) uint32
	objectRelease    func(uint32) int32
	objectConformsTo func(uint32, string) uint32

	getParentEntry func(uint32, string, *uint32) int32
	getRegistryID  func(uint32, *uint64) int32
	createProperty func(uint32, uintptr, uintptr, uint32) uintptr

	createString func(uintptr, string, uint32) uintptr
	getCString   func(uintptr, *byte, int64, uint32) uint8

	getTypeID           func(uintptr) uintptr
	getStringTypeID     func() uintptr
	getBooleanTypeID    func() uintptr
	getDictionaryTypeID func() uintptr
	getNumberTypeID     func() uintptr

	getBooleanValue    func(uintptr) uint8
	getDictionaryValue func(uintptr, uintptr) uintptr
	getNumberValue     func(uintptr, int64, *int64) uint8
	numberIsFloat      func(uintptr) uint8

	release func(uintptr)
}

type darwinDiskRecord struct {
	Usage    DiskUsage
	DriverID uint64
}

// Native framework handles remain loaded for the
// lifetime of the process.
var getDarwinDiskAPI = sync.OnceValues(
	loadDarwinDiskAPI,
)

// loadDarwinDiskAPI initializes the native function
// bindings without requiring CGO.
func loadDarwinDiskAPI() (*darwinDiskAPI, error) {
	coreFoundation, err := purego.Dlopen(
		darwinDiskCoreFoundationLibrary,
		purego.RTLD_NOW|purego.RTLD_LOCAL,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"load Core Foundation: %w",
			err,
		)
	}

	ioKit, err := purego.Dlopen(
		darwinDiskIOKitLibrary,
		purego.RTLD_NOW|purego.RTLD_LOCAL,
	)
	if err != nil {
		_ = purego.Dlclose(coreFoundation)

		return nil, fmt.Errorf(
			"load IOKit: %w",
			err,
		)
	}

	initialized := false

	defer func() {
		if !initialized {
			_ = purego.Dlclose(ioKit)
			_ = purego.Dlclose(coreFoundation)
		}
	}()

	api := &darwinDiskAPI{}

	bindings := []struct {
		library uintptr
		name    string
		target  any
	}{
		{ioKit, "IOServiceMatching", &api.serviceMatching},
		{ioKit, "IOServiceGetMatchingServices", &api.matchingServices},
		{ioKit, "IOIteratorNext", &api.iteratorNext},
		{ioKit, "IOIteratorIsValid", &api.iteratorIsValid},
		{ioKit, "IOObjectRelease", &api.objectRelease},
		{ioKit, "IOObjectConformsTo", &api.objectConformsTo},
		{ioKit, "IORegistryEntryGetParentEntry", &api.getParentEntry},
		{ioKit, "IORegistryEntryGetRegistryEntryID", &api.getRegistryID},
		{ioKit, "IORegistryEntryCreateCFProperty", &api.createProperty},

		{coreFoundation, "CFStringCreateWithCString", &api.createString},
		{coreFoundation, "CFStringGetCString", &api.getCString},
		{coreFoundation, "CFGetTypeID", &api.getTypeID},
		{coreFoundation, "CFStringGetTypeID", &api.getStringTypeID},
		{coreFoundation, "CFBooleanGetTypeID", &api.getBooleanTypeID},
		{coreFoundation, "CFDictionaryGetTypeID", &api.getDictionaryTypeID},
		{coreFoundation, "CFNumberGetTypeID", &api.getNumberTypeID},
		{coreFoundation, "CFBooleanGetValue", &api.getBooleanValue},
		{coreFoundation, "CFDictionaryGetValue", &api.getDictionaryValue},
		{coreFoundation, "CFNumberGetValue", &api.getNumberValue},
		{coreFoundation, "CFNumberIsFloatType", &api.numberIsFloat},
		{coreFoundation, "CFRelease", &api.release},
	}

	for _, binding := range bindings {
		symbol, err := purego.Dlsym(
			binding.library,
			binding.name,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"resolve native function %s: %w",
				binding.name,
				err,
			)
		}

		purego.RegisterFunc(
			binding.target,
			symbol,
		)
	}

	initialized = true

	return api, nil
}

// readDiskUsage collects cumulative disk activity
// for eligible macOS storage devices.
func readDiskUsage(
	ctx context.Context,
) ([]DiskUsage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	api, err := getDarwinDiskAPI()
	if err != nil {
		return nil, err
	}

	return collectDarwinDisks(ctx, api)
}

// collectDarwinDisks enumerates native media objects
// and collects their backing driver statistics.
func collectDarwinDisks(
	ctx context.Context,
	api *darwinDiskAPI,
) ([]DiskUsage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	matching := api.serviceMatching("IOMedia")
	if matching == 0 {
		return nil, fmt.Errorf(
			"%w: unable to create media matching dictionary",
			errInvalidDarwinDiskStats,
		)
	}

	var iterator uint32

	// This call consumes the matching dictionary.
	status := api.matchingServices(
		0,
		matching,
		&iterator,
	)

	if status != 0 || iterator == 0 {
		if iterator != 0 {
			_ = api.objectRelease(iterator)
		}

		return nil, fmt.Errorf(
			"enumerate IOKit media: status %#x",
			uint32(status),
		)
	}

	defer api.objectRelease(iterator)

	disks := make([]DiskUsage, 0)

	seenNames := make(map[string]struct{})
	seenDrivers := make(map[uint64]struct{})

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		media := api.iteratorNext(iterator)
		if media == 0 {
			break
		}

		record, included, err := readDarwinDiskMedia(
			api,
			media,
		)

		// Release each media object immediately,
		// rather than accumulating deferred releases.
		_ = api.objectRelease(media)

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if err != nil {
			return nil, err
		}

		if !included {
			continue
		}

		if _, exists := seenNames[record.Usage.Device]; exists {
			return nil, fmt.Errorf(
				"%w: duplicate disk %q",
				errInvalidDarwinDiskStats,
				record.Usage.Device,
			)
		}

		if _, exists := seenDrivers[record.DriverID]; exists {
			return nil, fmt.Errorf(
				"%w: multiple media share a storage driver",
				errInvalidDarwinDiskStats,
			)
		}

		seenNames[record.Usage.Device] = struct{}{}
		seenDrivers[record.DriverID] = struct{}{}

		disks = append(
			disks,
			record.Usage,
		)
	}

	if api.iteratorIsValid(iterator) == 0 {
		return nil, fmt.Errorf(
			"%w: media iterator became invalid",
			errInvalidDarwinDiskStats,
		)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sort.Slice(disks, func(i, j int) bool {
		return disks[i].Device < disks[j].Device
	})

	return disks, nil
}

// readDarwinDiskMedia checks whether a media object
// represents an eligible whole disk and retrieves
// its immediate storage driver's statistics.
func readDarwinDiskMedia(
	api *darwinDiskAPI,
	media uint32,
) (darwinDiskRecord, bool, error) {
	whole, err := api.property(media, "Whole")
	if err != nil {
		return darwinDiskRecord{}, false, err
	}

	if whole == 0 {
		return darwinDiskRecord{}, false, nil
	}

	defer api.release(whole)

	if api.getTypeID(whole) != api.getBooleanTypeID() {
		return darwinDiskRecord{}, false, fmt.Errorf(
			"%w: invalid whole-media property",
			errInvalidDarwinDiskStats,
		)
	}

	if api.getBooleanValue(whole) == 0 {
		return darwinDiskRecord{}, false, nil
	}

	var parent uint32

	status := api.getParentEntry(
		media,
		"IOService",
		&parent,
	)

	if status != 0 || parent == 0 {
		if parent != 0 {
			_ = api.objectRelease(parent)
		}

		return darwinDiskRecord{}, false, fmt.Errorf(
			"read media parent: status %#x",
			uint32(status),
		)
	}

	defer api.objectRelease(parent)

	// Do not collect synthesized APFS media,
	// partitions, or unrelated logical storage
	// objects without a direct block driver.
	if api.objectConformsTo(
		parent,
		"IOBlockStorageDriver",
	) == 0 {
		return darwinDiskRecord{}, false, nil
	}

	nameRef, err := api.property(
		media,
		"BSD Name",
	)
	if err != nil {
		return darwinDiskRecord{}, false, err
	}

	// A newly attached device may not yet have
	// a BSD device name.
	if nameRef == 0 {
		return darwinDiskRecord{}, false, nil
	}

	defer api.release(nameRef)

	name, err := api.stringValue(nameRef)
	if err != nil {
		return darwinDiskRecord{}, false, err
	}

	var driverID uint64

	status = api.getRegistryID(
		parent,
		&driverID,
	)

	if status != 0 || driverID == 0 {
		return darwinDiskRecord{}, false, fmt.Errorf(
			"read storage driver identity: status %#x",
			uint32(status),
		)
	}

	statistics, err := api.property(
		parent,
		"Statistics",
	)
	if err != nil {
		return darwinDiskRecord{}, false, err
	}

	if statistics == 0 {
		return darwinDiskRecord{}, false, fmt.Errorf(
			"%w: missing statistics for %q",
			errInvalidDarwinDiskStats,
			name,
		)
	}

	defer api.release(statistics)

	usage, err := api.diskCounters(
		name,
		statistics,
	)
	if err != nil {
		return darwinDiskRecord{}, false, err
	}

	return darwinDiskRecord{
		Usage:    usage,
		DriverID: driverID,
	}, true, nil
}

// property retrieves an owned Core Foundation
// reference. The caller must release nonzero results.
func (api *darwinDiskAPI) property(
	entry uint32,
	name string,
) (uintptr, error) {
	key := api.createString(
		0,
		name,
		darwinDiskUTF8Encoding,
	)
	if key == 0 {
		return 0, fmt.Errorf(
			"create property key %q",
			name,
		)
	}

	defer api.release(key)

	return api.createProperty(
		entry,
		key,
		0,
		0,
	), nil
}

// stringValue validates and converts a native
// Core Foundation string into a Go string.
func (api *darwinDiskAPI) stringValue(
	value uintptr,
) (string, error) {
	if value == 0 ||
		api.getTypeID(value) != api.getStringTypeID() {
		return "", fmt.Errorf(
			"%w: invalid device name property",
			errInvalidDarwinDiskStats,
		)
	}

	var buffer [256]byte

	if api.getCString(
		value,
		&buffer[0],
		int64(len(buffer)),
		darwinDiskUTF8Encoding,
	) == 0 {
		return "", fmt.Errorf(
			"%w: unable to decode device name",
			errInvalidDarwinDiskStats,
		)
	}

	end := bytes.IndexByte(
		buffer[:],
		0,
	)

	if end <= 0 {
		return "", fmt.Errorf(
			"%w: empty or malformed device name",
			errInvalidDarwinDiskStats,
		)
	}

	return string(buffer[:end]), nil
}

// diskCounters retrieves the four required
// cumulative storage performance counters.
func (api *darwinDiskAPI) diskCounters(
	name string,
	statistics uintptr,
) (DiskUsage, error) {
	if statistics == 0 ||
		api.getTypeID(statistics) != api.getDictionaryTypeID() {
		return DiskUsage{}, fmt.Errorf(
			"%w: invalid statistics dictionary for %q",
			errInvalidDarwinDiskStats,
			name,
		)
	}

	usage := DiskUsage{
		Device: name,
	}

	counters := []struct {
		key    string
		target *uint64
	}{
		{"Bytes (Read)", &usage.ReadBytes},
		{"Bytes (Write)", &usage.WriteBytes},
		{"Operations (Read)", &usage.ReadOperations},
		{"Operations (Write)", &usage.WriteOperations},
	}

	for _, counter := range counters {
		value, err := api.dictionaryCounter(
			statistics,
			counter.key,
		)
		if err != nil {
			return DiskUsage{}, fmt.Errorf(
				"disk %q: %w",
				name,
				err,
			)
		}

		*counter.target = value
	}

	return usage, nil
}

// dictionaryCounter retrieves a validated,
// nonnegative integer counter from a CFDictionary.
func (api *darwinDiskAPI) dictionaryCounter(
	dictionary uintptr,
	name string,
) (uint64, error) {
	key := api.createString(
		0,
		name,
		darwinDiskUTF8Encoding,
	)
	if key == 0 {
		return 0, fmt.Errorf(
			"create statistics key %q",
			name,
		)
	}

	defer api.release(key)

	// CFDictionaryGetValue returns a borrowed
	// reference. Do not release this value.
	number := api.getDictionaryValue(
		dictionary,
		key,
	)

	if number == 0 ||
		api.getTypeID(number) != api.getNumberTypeID() {
		return 0, fmt.Errorf(
			"%w: missing or invalid counter %q",
			errInvalidDarwinDiskStats,
			name,
		)
	}

	if api.numberIsFloat(number) != 0 {
		return 0, fmt.Errorf(
			"%w: floating-point counter %q",
			errInvalidDarwinDiskStats,
			name,
		)
	}

	var value int64

	if api.getNumberValue(
		number,
		darwinDiskSInt64Type,
		&value,
	) == 0 || value < 0 {
		return 0, fmt.Errorf(
			"%w: invalid integer counter %q",
			errInvalidDarwinDiskStats,
			name,
		)
	}

	return uint64(value), nil
}
