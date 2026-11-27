package metrics

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseLinuxMountLine(t *testing.T) {
	mount, err := parseLinuxMountLine(
		"36 25 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw",
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := linuxMount{
		ID:             36,
		ParentID:       25,
		Major:          8,
		Minor:          1,
		Root:           "/",
		MountPoint:     "/",
		Source:         "/dev/sda1",
		FilesystemType: "ext4",
		ReadOnly:       false,
	}

	if mount != expected {
		t.Fatalf(
			"expected %+v, got %+v",
			expected,
			mount,
		)
	}
}

func TestParseLinuxMountLineEscapes(t *testing.T) {
	mount, err := parseLinuxMountLine(
		`42 36 8:2 /Data\040Files /mnt/Media\040Library rw - ext4 /dev/disk\0431 rw`,
	)
	if err != nil {
		t.Fatal(err)
	}

	if mount.Root != "/Data Files" {
		t.Fatalf("unexpected root: %q", mount.Root)
	}

	if mount.MountPoint != "/mnt/Media Library" {
		t.Fatalf(
			"unexpected mount point: %q",
			mount.MountPoint,
		)
	}

	if mount.Source != "/dev/disk#1" {
		t.Fatalf(
			"unexpected source: %q",
			mount.Source,
		)
	}
}

func TestParseLinuxMountReadOnly(t *testing.T) {
	tests := []struct {
		name         string
		mountOptions string
		superOptions string
		expected     bool
	}{
		{"writable", "rw", "rw", false},
		{"readonly mount", "ro", "rw", true},
		{"readonly superblock", "rw", "ro", true},
		{"both readonly", "ro", "ro", true},
		{"exact option matching", "rw,errors=remount-ro", "rw", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := "36 25 8:1 / / " +
				tt.mountOptions +
				" - ext4 /dev/sda1 " +
				tt.superOptions

			mount, err := parseLinuxMountLine(line)
			if err != nil {
				t.Fatal(err)
			}

			if mount.ReadOnly != tt.expected {
				t.Fatalf(
					"expected read-only=%v, got %v",
					tt.expected,
					mount.ReadOnly,
				)
			}
		})
	}
}

func TestParseLinuxMountLineInvalid(t *testing.T) {
	tests := []string{
		"",
		"36 25",
		"36 25 8:1 / / rw ext4 /dev/sda1 rw",
		"0 25 8:1 / / rw - ext4 /dev/sda1 rw",
		"36 0 8:1 / / rw - ext4 /dev/sda1 rw",
		"invalid 25 8:1 / / rw - ext4 /dev/sda1 rw",
		"36 25 8 / / rw - ext4 /dev/sda1 rw",
		"36 25 x:1 / / rw - ext4 /dev/sda1 rw",
		"36 25 8:x / / rw - ext4 /dev/sda1 rw",
		"36 25 4294967296:1 / / rw - ext4 /dev/sda1 rw",
		"36 25 8:1 / relative rw - ext4 /dev/sda1 rw",
		"36 25 8:1 / / rw - ext4 /dev/sda1",
		"36 25 8:1 / / rw - ext4 /dev/sda1 rw extra",
		`36 25 8:1 / /mnt/bad\999 rw - ext4 /dev/sda1 rw`,
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			mount, err := parseLinuxMountLine(input)

			if !errors.Is(err, ErrInvalidMountInfo) {
				t.Fatalf(
					"expected ErrInvalidMountInfo, got %v",
					err,
				)
			}

			if mount != (linuxMount{}) {
				t.Fatal("invalid input returned mount")
			}
		})
	}
}

func TestDecodeLinuxMountField(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/mnt/data", "/mnt/data"},
		{`/mnt/My\040Drive`, "/mnt/My Drive"},
		{`/mnt/a\011b`, "/mnt/a\tb"},
		{`/mnt/a\012b`, "/mnt/a\nb"},
		{`/mnt/a\134b`, `/mnt/a\b`},
		{`/mnt/a\043b`, "/mnt/a#b"},

		// Escape decoding must not be recursive.
		{`/mnt/\134040`, `/mnt/\040`},

		// Unicode whitespace is valid path content.
		{"/mnt/a\u00a0b", "/mnt/a\u00a0b"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			actual, err := decodeLinuxMountField(tt.input)
			if err != nil {
				t.Fatal(err)
			}

			if actual != tt.expected {
				t.Fatalf(
					"expected %q, got %q",
					tt.expected,
					actual,
				)
			}
		})
	}
}

func TestDecodeLinuxMountFieldInvalid(t *testing.T) {
	tests := []string{
		"",
		`/mnt/\`,
		`/mnt/\04`,
		`/mnt/\999`,
		`/mnt/\400`,
		`/mnt/\000`,
		"/mnt/\x00",
	}

	for _, input := range tests {
		_, err := decodeLinuxMountField(input)

		if !errors.Is(err, ErrInvalidMountInfo) {
			t.Fatalf(
				"input %q: expected invalid mount error, got %v",
				input,
				err,
			)
		}
	}
}

func TestParseLinuxMountInfo(t *testing.T) {
	const input = `36 25 8:1 / / rw - ext4 /dev/sda1 rw
37 36 0:5 / /proc rw - proc proc rw
38 36 8:2 / /mnt/data rw - xfs /dev/sdb1 rw
39 36 0:6 / /sys rw - sysfs sysfs rw
40 36 0:7 / /run rw - tmpfs tmpfs rw
`

	mounts, err := parseLinuxMountInfo(
		context.Background(),
		strings.NewReader(input),
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(mounts) != 5 {
		t.Fatalf("expected 5 mounts, got %d", len(mounts))
	}

	filtered := filterLinuxMounts(mounts)

	if len(filtered) != 2 {
		t.Fatalf(
			"expected 2 monitored mounts, got %d",
			len(filtered),
		)
	}

	if filtered[0].MountPoint != "/" ||
		filtered[1].MountPoint != "/mnt/data" {
		t.Fatalf(
			"unexpected filtered mounts: %+v",
			filtered,
		)
	}
}

func TestParseLinuxMountInfoDuplicateID(t *testing.T) {
	const input = `36 25 8:1 / / rw - ext4 /dev/sda1 rw
36 25 8:2 / /mnt/data rw - xfs /dev/sdb1 rw
`

	mounts, err := parseLinuxMountInfo(
		context.Background(),
		strings.NewReader(input),
	)

	if !errors.Is(err, ErrInvalidMountInfo) {
		t.Fatalf(
			"expected ErrInvalidMountInfo, got %v",
			err,
		)
	}

	if mounts != nil {
		t.Fatal("duplicate mount ID returned results")
	}
}

func TestFilterLinuxMounts(t *testing.T) {
	mounts := []linuxMount{
		{
			ID:             1,
			MountPoint:     "/mnt/z",
			FilesystemType: "zfs",
		},
		{
			ID:             2,
			MountPoint:     "/",
			FilesystemType: "overlay",
		},
		{
			ID:             3,
			MountPoint:     "/mnt/a",
			FilesystemType: "btrfs",
		},
		{
			ID:             4,
			MountPoint:     "/var/lib/container",
			FilesystemType: "overlay",
		},
		{
			ID:             5,
			MountPoint:     "/mnt/remote",
			FilesystemType: "nfs4",
		},
		{
			ID:             6,
			MountPoint:     "/sys",
			FilesystemType: "sysfs",
		},
		{
			ID:             7,
			MountPoint:     "/mnt/unknown",
			FilesystemType: "unknown",
		},
	}

	filtered := filterLinuxMounts(mounts)

	var paths []string

	for _, mount := range filtered {
		paths = append(paths, mount.MountPoint)
	}

	expected := []string{
		"/",
		"/mnt/a",
		"/mnt/z",
	}

	if !reflect.DeepEqual(paths, expected) {
		t.Fatalf(
			"expected %v, got %v",
			expected,
			paths,
		)
	}
}

func TestFilterLinuxMountsEmpty(t *testing.T) {
	mounts := filterLinuxMounts(nil)

	if mounts == nil || len(mounts) != 0 {
		t.Fatal("expected initialized empty mount slice")
	}
}

func TestDiscoverLinuxMountsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	mounts, err := discoverLinuxMounts(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if mounts != nil {
		t.Fatal("canceled discovery returned mounts")
	}
}

func TestDiscoverLinuxMountsNative(t *testing.T) {
	mounts, err := discoverLinuxMounts(
		context.Background(),
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Discovered %d monitored mounts", len(mounts))

	for _, mount := range mounts {
		if !isMonitoredLinuxFilesystem(mount) {
			t.Fatal("unexpected filesystem in discovery")
		}

		t.Logf(
			"mount=%q source=%q type=%q device=%d:%d readonly=%v",
			mount.MountPoint,
			mount.Source,
			mount.FilesystemType,
			mount.Major,
			mount.Minor,
			mount.ReadOnly,
		)
	}
}
