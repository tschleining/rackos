package metrics

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
	"unsafe"
)

// testDarwinDiskAPI creates synthetic native
// functions and tracks released native references.
func testDarwinDiskAPI(
	t *testing.T,
) (
	*darwinDiskAPI,
	map[uint32]int,
	map[uintptr]int,
) {
	t.Helper()

	releasedObjects := make(map[uint32]int)
	releasedValues := make(map[uintptr]int)

	keys := map[string]uintptr{
		"Whole":              101,
		"BSD Name":           102,
		"Statistics":         103,
		"Bytes (Read)":       104,
		"Bytes (Write)":      105,
		"Operations (Read)":  106,
		"Operations (Write)": 107,
	}

	// Media 10: eligible whole disk.
	// Media 11: partition.
	// Media 12: unrelated logical whole media.
	media := []uint32{10, 11, 12}
	index := 0

	api := &darwinDiskAPI{
		serviceMatching: func(class string) uintptr {
			if class != "IOMedia" {
				t.Fatal("unexpected matching class")
			}

			return 1
		},

		matchingServices: func(
			port uint32,
			matching uintptr,
			iterator *uint32,
		) int32 {
			if port != 0 || matching != 1 {
				t.Fatal("unexpected matching arguments")
			}

			*iterator = 2
			return 0
		},

		iteratorNext: func(iterator uint32) uint32 {
			if index >= len(media) {
				return 0
			}

			value := media[index]
			index++

			return value
		},

		iteratorIsValid: func(uint32) uint32 {
			return 1
		},

		objectRelease: func(object uint32) int32 {
			releasedObjects[object]++
			return 0
		},

		objectConformsTo: func(
			object uint32,
			class string,
		) uint32 {
			if class == "IOBlockStorageDriver" &&
				object == 20 {
				return 1
			}

			return 0
		},

		getParentEntry: func(
			entry uint32,
			plane string,
			parent *uint32,
		) int32 {
			if plane != "IOService" {
				t.Fatal("unexpected registry plane")
			}

			switch entry {
			case 10:
				*parent = 20

			case 12:
				*parent = 22

			default:
				t.Fatal("unexpected parent lookup")
			}

			return 0
		},

		getRegistryID: func(
			entry uint32,
			id *uint64,
		) int32 {
			*id = 900
			return 0
		},

		createProperty: func(
			entry uint32,
			key uintptr,
			allocator uintptr,
			options uint32,
		) uintptr {
			switch key {
			case 101:
				if entry == 11 {
					return 208
				}

				return 201

			case 102:
				return 202

			case 103:
				return 203

			default:
				return 0
			}
		},

		createString: func(
			allocator uintptr,
			value string,
			encoding uint32,
		) uintptr {
			return keys[value]
		},

		getCString: func(
			value uintptr,
			buffer *byte,
			size int64,
			encoding uint32,
		) uint8 {
			if value != 202 || size < 6 {
				return 0
			}

			target := unsafe.Slice(
				buffer,
				int(size),
			)

			copy(target, "disk0\x00")

			return 1
		},

		getTypeID: func(value uintptr) uintptr {
			switch value {
			case 201, 208:
				return 1
			case 202:
				return 2
			case 203:
				return 3
			case 204, 205, 206, 207:
				return 4
			default:
				return 0
			}
		},

		getBooleanTypeID: func() uintptr {
			return 1
		},

		getStringTypeID: func() uintptr {
			return 2
		},

		getDictionaryTypeID: func() uintptr {
			return 3
		},

		getNumberTypeID: func() uintptr {
			return 4
		},

		getBooleanValue: func(value uintptr) uint8 {
			if value == 201 {
				return 1
			}

			return 0
		},

		getDictionaryValue: func(
			dictionary uintptr,
			key uintptr,
		) uintptr {
			if dictionary != 203 {
				t.Fatal("unexpected dictionary")
			}

			return key + 100
		},

		getNumberValue: func(
			number uintptr,
			kind int64,
			value *int64,
		) uint8 {
			if kind != darwinDiskSInt64Type {
				t.Fatal("unexpected number type")
			}

			switch number {
			case 204:
				*value = 4096000
			case 205:
				*value = 2048000
			case 206:
				*value = 1000
			case 207:
				*value = 500
			default:
				return 0
			}

			return 1
		},

		numberIsFloat: func(uintptr) uint8 {
			return 0
		},

		release: func(value uintptr) {
			releasedValues[value]++
		},
	}

	return api, releasedObjects, releasedValues
}

func TestDarwinDiskCounters(t *testing.T) {
	api, _, released := testDarwinDiskAPI(t)

	usage, err := api.diskCounters(
		"disk0",
		203,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := DiskUsage{
		Device:          "disk0",
		ReadBytes:       4096000,
		WriteBytes:      2048000,
		ReadOperations:  1000,
		WriteOperations: 500,
	}

	if usage != expected {
		t.Fatalf(
			"expected %+v, got %+v",
			expected,
			usage,
		)
	}

	// Each created key must be released.
	for _, key := range []uintptr{
		104, 105, 106, 107,
	} {
		if released[key] != 1 {
			t.Fatalf(
				"statistics key %d was not released",
				key,
			)
		}
	}

	// Borrowed dictionary values must not
	// be released independently.
	for _, value := range []uintptr{
		204, 205, 206, 207,
	} {
		if released[value] != 0 {
			t.Fatal("borrowed counter was released")
		}
	}
}

func TestDarwinDiskCountersInvalid(t *testing.T) {
	tests := []struct {
		name   string
		change func(*darwinDiskAPI)
	}{
		{
			name: "missing counter",
			change: func(api *darwinDiskAPI) {
				api.getDictionaryValue = func(
					uintptr,
					uintptr,
				) uintptr {
					return 0
				}
			},
		},
		{
			name: "incorrect counter type",
			change: func(api *darwinDiskAPI) {
				api.getDictionaryValue = func(
					uintptr,
					uintptr,
				) uintptr {
					return 202
				}
			},
		},
		{
			name: "floating point counter",
			change: func(api *darwinDiskAPI) {
				api.numberIsFloat = func(uintptr) uint8 {
					return 1
				}
			},
		},
		{
			name: "negative counter",
			change: func(api *darwinDiskAPI) {
				api.getNumberValue = func(
					number uintptr,
					kind int64,
					value *int64,
				) uint8 {
					*value = -1
					return 1
				}
			},
		},
		{
			name: "failed integer conversion",
			change: func(api *darwinDiskAPI) {
				api.getNumberValue = func(
					uintptr,
					int64,
					*int64,
				) uint8 {
					return 0
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, _, _ := testDarwinDiskAPI(t)

			tt.change(api)

			disk, err := api.diskCounters(
				"disk0",
				203,
			)

			if !errors.Is(err, errInvalidDarwinDiskStats) {
				t.Fatalf(
					"expected invalid disk statistics, got %v",
					err,
				)
			}

			if disk != (DiskUsage{}) {
				t.Fatal("invalid counters returned disk usage")
			}
		})
	}
}

func TestDarwinDiskCounterBoundaries(t *testing.T) {
	for _, expected := range []int64{
		0,
		1,
		math.MaxInt64,
	} {
		api, _, _ := testDarwinDiskAPI(t)

		api.getNumberValue = func(
			number uintptr,
			kind int64,
			value *int64,
		) uint8 {
			*value = expected
			return 1
		}

		value, err := api.dictionaryCounter(
			203,
			"Bytes (Read)",
		)
		if err != nil {
			t.Fatal(err)
		}

		if value != uint64(expected) {
			t.Fatalf(
				"expected %d, got %d",
				expected,
				value,
			)
		}
	}
}

func TestDarwinDiskCountersInvalidDictionary(t *testing.T) {
	api, _, _ := testDarwinDiskAPI(t)

	for _, dictionary := range []uintptr{
		0,
		202,
		204,
	} {
		disk, err := api.diskCounters(
			"disk0",
			dictionary,
		)

		if !errors.Is(err, errInvalidDarwinDiskStats) {
			t.Fatalf(
				"expected invalid dictionary, got %v",
				err,
			)
		}

		if disk != (DiskUsage{}) {
			t.Fatal("invalid dictionary returned disk")
		}
	}
}

func TestReadDarwinDiskMediaPartition(t *testing.T) {
	api, _, released := testDarwinDiskAPI(t)

	record, included, err := readDarwinDiskMedia(
		api,
		11,
	)
	if err != nil {
		t.Fatal(err)
	}

	if included || record != (darwinDiskRecord{}) {
		t.Fatal("partition was included")
	}

	if released[208] != 1 {
		t.Fatal("whole-media property was not released")
	}
}

func TestReadDarwinDiskMediaLogicalDevice(t *testing.T) {
	api, objects, values := testDarwinDiskAPI(t)

	record, included, err := readDarwinDiskMedia(
		api,
		12,
	)
	if err != nil {
		t.Fatal(err)
	}

	if included || record != (darwinDiskRecord{}) {
		t.Fatal("unrelated logical device was included")
	}

	if objects[22] != 1 {
		t.Fatal("parent object was not released")
	}

	if values[201] != 1 {
		t.Fatal("whole-media property was not released")
	}
}

func TestCollectDarwinDisksEmpty(t *testing.T) {
	api, released, _ := testDarwinDiskAPI(t)

	api.iteratorNext = func(uint32) uint32 {
		return 0
	}

	disks, err := collectDarwinDisks(
		context.Background(),
		api,
	)
	if err != nil {
		t.Fatal(err)
	}

	if disks == nil || len(disks) != 0 {
		t.Fatal("expected initialized empty disk slice")
	}

	if released[2] != 1 {
		t.Fatal("iterator was not released")
	}
}

func TestCollectDarwinDisksInvalidIterator(t *testing.T) {
	api, released, _ := testDarwinDiskAPI(t)

	api.iteratorNext = func(uint32) uint32 {
		return 0
	}

	api.iteratorIsValid = func(uint32) uint32 {
		return 0
	}

	disks, err := collectDarwinDisks(
		context.Background(),
		api,
	)

	if !errors.Is(err, errInvalidDarwinDiskStats) {
		t.Fatalf("expected invalid iterator, got %v", err)
	}

	if disks != nil {
		t.Fatal("invalid iterator returned disk usage")
	}

	if released[2] != 1 {
		t.Fatal("iterator was not released")
	}
}

func TestCollectDarwinDisksCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	api, released, _ := testDarwinDiskAPI(t)

	api.iteratorNext = func(uint32) uint32 {
		cancel()
		return 0
	}

	disks, err := collectDarwinDisks(
		ctx,
		api,
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	if disks != nil {
		t.Fatal("canceled collection returned disks")
	}

	if released[2] != 1 {
		t.Fatal("iterator was not released")
	}
}

func TestReadDarwinDiskUsageCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	disks, err := readDiskUsage(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	if disks != nil {
		t.Fatal("canceled collection returned disks")
	}
}

func TestReadDarwinDiskUsageNative(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	disks, err := readDiskUsage(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if disks == nil {
		t.Fatal("disk slice is nil")
	}

	if len(disks) == 0 {
		t.Skip("no eligible native disks discovered")
	}

	t.Logf("Collected %d macOS disks", len(disks))

	for i, disk := range disks {
		if disk.Device == "" {
			t.Fatal("empty disk device name")
		}

		if i > 0 &&
			disks[i-1].Device >= disk.Device {
			t.Fatal("invalid disk ordering or uniqueness")
		}

		t.Logf(
			"device=%q read_bytes=%d write_bytes=%d read_operations=%d write_operations=%d",
			disk.Device,
			disk.ReadBytes,
			disk.WriteBytes,
			disk.ReadOperations,
			disk.WriteOperations,
		)
	}
}

func TestCollectDarwinDisks(t *testing.T) {
	api, objects, values := testDarwinDiskAPI(t)

	disks, err := collectDarwinDisks(
		context.Background(),
		api,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := DiskUsage{
		Device:          "disk0",
		ReadBytes:       4096000,
		WriteBytes:      2048000,
		ReadOperations:  1000,
		WriteOperations: 500,
	}

	if len(disks) != 1 || disks[0] != expected {
		t.Fatalf("unexpected disks: %+v", disks)
	}

	// Iterator, media, and parent objects
	// must each be released exactly once.
	for _, object := range []uint32{
		2, 10, 11, 12, 20, 22,
	} {
		if objects[object] != 1 {
			t.Fatalf(
				"object %d released %d times",
				object,
				objects[object],
			)
		}
	}

	// Matching dictionary ownership was
	// transferred to IOKit.
	if values[1] != 0 {
		t.Fatal("matching dictionary released twice")
	}

	expectedReleases := map[uintptr]int{
		101: 3,
		102: 1,
		103: 1,
		104: 1,
		105: 1,
		106: 1,
		107: 1,
		201: 2,
		202: 1,
		203: 1,
		208: 1,
	}

	for value, expected := range expectedReleases {
		if values[value] != expected {
			t.Fatalf(
				"value %d: expected %d releases, got %d",
				value,
				expected,
				values[value],
			)
		}
	}

	for _, value := range []uintptr{
		204, 205, 206, 207,
	} {
		if values[value] != 0 {
			t.Fatal("borrowed counter released")
		}
	}
}
