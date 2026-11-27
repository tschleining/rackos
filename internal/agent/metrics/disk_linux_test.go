package metrics

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestParseLinuxDiskCounters(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected DiskUsage
	}{
		{
			name: "standard disk statistics",
			input: "8 0 sda 1000 50 8000 200 " +
				"500 25 4000 100 0 300 350",
			expected: DiskUsage{
				Device:          "sda",
				ReadBytes:       4096000,
				WriteBytes:      2048000,
				ReadOperations:  1000,
				WriteOperations: 500,
			},
		},
		{
			name: "NVMe extended statistics",
			input: "259 0 nvme0n1 100 0 200 0 " +
				"50 0 100 0 0 0 0 10 0 20 0 5 0",
			expected: DiskUsage{
				Device:          "nvme0n1",
				ReadBytes:       102400,
				WriteBytes:      51200,
				ReadOperations:  100,
				WriteOperations: 50,
			},
		},
		{
			name:  "inactive disk",
			input: "8 16 sdb 0 0 0 0 0 0 0 0 0 0 0",
			expected: DiskUsage{
				Device: "sdb",
			},
		},
		{
			name: "read activity only",
			input: "8 0 sda 10 0 20 0 " +
				"0 0 0 0 0 0 0",
			expected: DiskUsage{
				Device:         "sda",
				ReadBytes:      10240,
				ReadOperations: 10,
			},
		},
		{
			name: "write activity only",
			input: "8 0 sda 0 0 0 0 " +
				"10 0 20 0 0 0 0",
			expected: DiskUsage{
				Device:          "sda",
				WriteBytes:      10240,
				WriteOperations: 10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disk, err := parseLinuxDiskCounters(
				strings.Fields(tt.input),
			)
			if err != nil {
				t.Fatal(err)
			}

			if disk != tt.expected {
				t.Fatalf(
					"expected %+v, got %+v",
					tt.expected,
					disk,
				)
			}
		})
	}
}

func TestParseLinuxDiskCountersInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "empty input",
		},
		{
			name:  "incomplete counters",
			input: "8 0 sda 100 0 200",
		},
		{
			name: "negative counter",
			input: "8 0 sda -1 0 200 0 " +
				"50 0 100 0 0 0 0",
		},
		{
			name: "non-numeric counter",
			input: "8 0 sda invalid 0 200 0 " +
				"50 0 100 0 0 0 0",
		},
		{
			name: "invalid unused base counter",
			input: "8 0 sda 100 invalid 200 0 " +
				"50 0 100 0 0 0 0",
		},
		{
			name: "integer parsing overflow",
			input: "8 0 sda 18446744073709551616 " +
				"0 200 0 50 0 100 0 0 0 0",
		},
		{
			name: "read byte conversion overflow",
			input: "8 0 sda 100 0 36028797018963968 " +
				"0 50 0 100 0 0 0 0",
		},
		{
			name: "write byte conversion overflow",
			input: "8 0 sda 100 0 200 0 50 0 " +
				"36028797018963968 0 0 0 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			disk, err := parseLinuxDiskCounters(
				strings.Fields(tt.input),
			)

			if !errors.Is(err, errInvalidLinuxDiskStats) {
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

func TestLinuxDiskSectorsToBytes(t *testing.T) {
	tests := []struct {
		sectors  uint64
		expected uint64
	}{
		{0, 0},
		{1, 512},
		{8, 4096},
		{2048, 1048576},
		{
			math.MaxUint64 / 512,
			math.MaxUint64 - 511,
		},
	}

	for _, tt := range tests {
		actual, err := linuxDiskSectorsToBytes(
			tt.sectors,
		)
		if err != nil {
			t.Fatal(err)
		}

		if actual != tt.expected {
			t.Fatalf(
				"expected %d bytes, got %d",
				tt.expected,
				actual,
			)
		}
	}
}

func TestLinuxDiskSectorsToBytesOverflow(t *testing.T) {
	value, err := linuxDiskSectorsToBytes(
		math.MaxUint64/512 + 1,
	)

	if !errors.Is(err, errInvalidLinuxDiskStats) {
		t.Fatalf("expected overflow error, got %v", err)
	}

	if value != 0 {
		t.Fatal("overflow returned a byte count")
	}
}

func TestParseLinuxDiskDeviceInvalid(t *testing.T) {
	tests := [][2]string{
		{"", "0"},
		{"8", ""},
		{"invalid", "0"},
		{"8", "invalid"},
		{"-1", "0"},
		{"8", "-1"},
		{"4294967296", "0"},
		{"8", "4294967296"},
	}

	for _, tt := range tests {
		device, err := parseLinuxDiskDevice(
			tt[0],
			tt[1],
		)

		if !errors.Is(err, errInvalidLinuxDiskStats) {
			t.Fatalf(
				"expected invalid device identity, got %v",
				err,
			)
		}

		if device != (linuxDiskDevice{}) {
			t.Fatal("invalid identity returned device")
		}
	}
}

func TestParseLinuxDiskStats(t *testing.T) {
	const input = `
8 16 sdb 100 0 200 0 50 0 100 0 0 0 0
8 0 sda 1000 0 8000 0 500 0 4000 0 0 0 0
8 1 sda1 100 0 800 0 50 0 400 0 0 0 0
7 0 loop0 50 0 100 0 10 0 20 0 0 0 0
253 0 dm-0 100 0 800 0 50 0 400 0 0 0 0
`

	devices := map[string]linuxDiskDevice{
		"sda": {Major: 8, Minor: 0},
		"sdb": {Major: 8, Minor: 16},
	}

	disks, err := parseLinuxDiskStats(
		context.Background(),
		strings.NewReader(input),
		devices,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := []DiskUsage{
		{
			Device:          "sda",
			ReadBytes:       4096000,
			WriteBytes:      2048000,
			ReadOperations:  1000,
			WriteOperations: 500,
		},
		{
			Device:          "sdb",
			ReadBytes:       102400,
			WriteBytes:      51200,
			ReadOperations:  100,
			WriteOperations: 50,
		},
	}

	if !reflect.DeepEqual(disks, expected) {
		t.Fatalf(
			"expected %+v, got %+v",
			expected,
			disks,
		)
	}
}

func TestParseLinuxDiskStatsIdentityChanged(t *testing.T) {
	const input = "8 16 sda 100 0 200 0 " +
		"50 0 100 0 0 0 0"

	devices := map[string]linuxDiskDevice{
		"sda": {Major: 8, Minor: 0},
	}

	disks, err := parseLinuxDiskStats(
		context.Background(),
		strings.NewReader(input),
		devices,
	)
	if err != nil {
		t.Fatal(err)
	}

	if disks == nil || len(disks) != 0 {
		t.Fatal("changed device identity was not skipped")
	}
}

func TestParseLinuxDiskStatsDuplicate(t *testing.T) {
	const line = "8 0 sda 100 0 200 0 " +
		"50 0 100 0 0 0 0"

	devices := map[string]linuxDiskDevice{
		"sda": {Major: 8, Minor: 0},
	}

	disks, err := parseLinuxDiskStats(
		context.Background(),
		strings.NewReader(line+"\n"+line),
		devices,
	)

	if !errors.Is(err, errInvalidLinuxDiskStats) {
		t.Fatalf("expected duplicate disk error, got %v", err)
	}

	if disks != nil {
		t.Fatal("duplicate disk returned results")
	}
}

func TestParseLinuxDiskStatsMalformed(t *testing.T) {
	devices := map[string]linuxDiskDevice{
		"sda": {Major: 8, Minor: 0},
	}

	tests := []string{
		"8 0",
		"invalid 0 sda 100 0 200 0 50 0 100 0 0 0 0",
		"8 0 sda 100",
	}

	for _, input := range tests {
		disks, err := parseLinuxDiskStats(
			context.Background(),
			strings.NewReader(input),
			devices,
		)

		if !errors.Is(err, errInvalidLinuxDiskStats) {
			t.Fatalf(
				"expected malformed record error, got %v",
				err,
			)
		}

		if disks != nil {
			t.Fatal("malformed record returned results")
		}
	}
}

func TestParseLinuxDiskStatsEmpty(t *testing.T) {
	disks, err := parseLinuxDiskStats(
		context.Background(),
		strings.NewReader(""),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	if disks == nil || len(disks) != 0 {
		t.Fatal("expected initialized empty disk slice")
	}
}

func TestParseLinuxDiskStatsReadFailure(t *testing.T) {
	expectedError := errors.New("diskstats read failed")

	reader := io.MultiReader(
		strings.NewReader(
			"8 0 sda 100 0 200 0 50 0 100 0 0 0 0\n",
		),
		iotest.ErrReader(expectedError),
	)

	devices := map[string]linuxDiskDevice{
		"sda": {Major: 8, Minor: 0},
	}

	disks, err := parseLinuxDiskStats(
		context.Background(),
		reader,
		devices,
	)

	if !errors.Is(err, expectedError) {
		t.Fatalf("expected read error, got %v", err)
	}

	if disks != nil {
		t.Fatal("failed read returned partial results")
	}
}

func TestDiscoverLinuxDisks(t *testing.T) {
	sysRoot := t.TempDir()

	blockPath := filepath.Join(
		sysRoot,
		"block",
	)

	if err := os.MkdirAll(blockPath, 0700); err != nil {
		t.Fatal(err)
	}

	createTestLinuxDisk(
		t,
		sysRoot,
		"sda",
		"devices/pci0000/block/sda",
		"8:0\n",
	)

	createTestLinuxDisk(
		t,
		sysRoot,
		"nvme0n1",
		"devices/pci0000/nvme/nvme0n1",
		"259:0\n",
	)

	createTestLinuxDisk(
		t,
		sysRoot,
		"vda",
		"devices/pci0000/virtio/block/vda",
		"252:0\n",
	)

	for _, name := range []string{
		"loop0",
		"ram0",
		"zram0",
		"dm-0",
		"md0",
	} {
		createTestLinuxDisk(
			t,
			sysRoot,
			name,
			"devices/virtual/block/"+name,
			"1:0\n",
		)
	}

	// Partitions exist beneath a whole disk but
	// are not separately listed in /sys/block.
	partitionPath := filepath.Join(
		sysRoot,
		"devices/pci0000/block/sda/sda1",
	)

	if err := os.MkdirAll(
		partitionPath,
		0700,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(partitionPath, "dev"),
		[]byte("8:1\n"),
		0600,
	); err != nil {
		t.Fatal(err)
	}

	// Simulate a disconnected storage device.
	if err := os.Symlink(
		filepath.Join(sysRoot, "missing"),
		filepath.Join(blockPath, "removed"),
	); err != nil {
		t.Fatal(err)
	}

	devices, err := discoverLinuxDisks(
		context.Background(),
		sysRoot,
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := map[string]linuxDiskDevice{
		"sda":     {Major: 8, Minor: 0},
		"nvme0n1": {Major: 259, Minor: 0},
		"vda":     {Major: 252, Minor: 0},
	}

	if !reflect.DeepEqual(devices, expected) {
		t.Fatalf(
			"expected %+v, got %+v",
			expected,
			devices,
		)
	}
}

func TestDiscoverLinuxDisksInvalidIdentity(t *testing.T) {
	sysRoot := t.TempDir()

	if err := os.MkdirAll(
		filepath.Join(sysRoot, "block"),
		0700,
	); err != nil {
		t.Fatal(err)
	}

	createTestLinuxDisk(
		t,
		sysRoot,
		"sda",
		"devices/pci0000/block/sda",
		"invalid",
	)

	devices, err := discoverLinuxDisks(
		context.Background(),
		sysRoot,
	)

	if !errors.Is(err, errInvalidLinuxDiskStats) {
		t.Fatalf("expected invalid identity, got %v", err)
	}

	if devices != nil {
		t.Fatal("invalid identity returned devices")
	}
}

func TestDiscoverLinuxDisksMissingSysfs(t *testing.T) {
	devices, err := discoverLinuxDisks(
		context.Background(),
		t.TempDir(),
	)

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing sysfs error, got %v", err)
	}

	if devices != nil {
		t.Fatal("missing sysfs returned devices")
	}
}

func TestReadLinuxDiskUsageCanceled(t *testing.T) {
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

func TestReadLinuxDiskUsageNative(t *testing.T) {
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
		t.Skip("no eligible block devices visible")
	}

	t.Logf("Collected %d Linux disks", len(disks))

	for i, disk := range disks {
		if disk.Device == "" {
			t.Fatal("disk device name is empty")
		}

		if i > 0 &&
			disks[i-1].Device >= disk.Device {
			t.Fatal("disk ordering or uniqueness is invalid")
		}

		if disk.ReadBytes%512 != 0 ||
			disk.WriteBytes%512 != 0 {
			t.Fatal("disk byte counters are not sector aligned")
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

// createTestLinuxDisk builds a synthetic sysfs device
// directory and corresponding /sys/block symlink.
func createTestLinuxDisk(
	t *testing.T,
	sysRoot string,
	name string,
	relativePath string,
	identity string,
) {
	t.Helper()

	devicePath := filepath.Join(
		sysRoot,
		relativePath,
	)

	if err := os.MkdirAll(
		devicePath,
		0700,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(devicePath, "dev"),
		[]byte(identity),
		0600,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(
		devicePath,
		filepath.Join(sysRoot, "block", name),
	); err != nil {
		t.Fatal(err)
	}
}
