package metrics

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

const linuxMountInfoPath = "/proc/self/mountinfo"

var ErrInvalidMountInfo = errors.New(
	"invalid mount information",
)

// linuxMount represents a mount visible within
// the agent's current Linux mount namespace.
type linuxMount struct {
	ID       uint64
	ParentID uint64

	Major uint32
	Minor uint32

	Root       string
	MountPoint string

	Source         string
	FilesystemType string

	ReadOnly bool
}

// discoverLinuxMounts discovers mounted filesystems
// eligible for storage capacity monitoring.
func discoverLinuxMounts(
	ctx context.Context,
) ([]linuxMount, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	file, err := os.Open(linuxMountInfoPath)
	if err != nil {
		return nil, fmt.Errorf(
			"open mount information: %w",
			err,
		)
	}
	defer file.Close()

	mounts, err := parseLinuxMountInfo(ctx, file)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return filterLinuxMounts(mounts), nil
}

// parseLinuxMountInfo parses the complete mountinfo
// stream before applying filesystem filtering.
func parseLinuxMountInfo(
	ctx context.Context,
	reader io.Reader,
) ([]linuxMount, error) {
	scanner := bufio.NewScanner(reader)

	// Mount options can be lengthy, particularly
	// when OverlayFS contains many lower layers.
	scanner.Buffer(
		make([]byte, 64*1024),
		1024*1024,
	)

	mounts := make([]linuxMount, 0)
	seen := make(map[uint64]struct{})

	lineNumber := 0

	for scanner.Scan() {
		lineNumber++

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		mount, err := parseLinuxMountLine(
			scanner.Text(),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"mountinfo line %d: %w",
				lineNumber,
				err,
			)
		}

		if _, exists := seen[mount.ID]; exists {
			return nil, fmt.Errorf(
				"%w: duplicate mount ID %d",
				ErrInvalidMountInfo,
				mount.ID,
			)
		}

		seen[mount.ID] = struct{}{}
		mounts = append(mounts, mount)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf(
			"read mount information: %w",
			err,
		)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if len(mounts) == 0 {
		return nil, fmt.Errorf(
			"%w: mount information is empty",
			ErrInvalidMountInfo,
		)
	}

	return mounts, nil
}

// parseLinuxMountLine parses one mountinfo record.
//
// Record structure:
//
// mount-id parent-id major:minor root mount-point
// mount-options [optional-fields...] -
// filesystem-type source super-options
func parseLinuxMountLine(
	line string,
) (linuxMount, error) {
	// Only ASCII spaces separate mountinfo fields.
	// Unicode whitespace can be part of a path.
	fields := strings.FieldsFunc(
		line,
		func(r rune) bool {
			return r == ' '
		},
	)

	if len(fields) < 10 {
		return linuxMount{}, fmt.Errorf(
			"%w: insufficient mount fields",
			ErrInvalidMountInfo,
		)
	}

	separator := -1

	for i := 6; i < len(fields); i++ {
		if fields[i] == "-" {
			separator = i
			break
		}
	}

	if separator < 6 ||
		len(fields) != separator+4 {
		return linuxMount{}, fmt.Errorf(
			"%w: invalid mount field separator",
			ErrInvalidMountInfo,
		)
	}

	id, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || id == 0 {
		return linuxMount{}, fmt.Errorf(
			"%w: invalid mount ID",
			ErrInvalidMountInfo,
		)
	}

	parentID, err := strconv.ParseUint(
		fields[1],
		10,
		64,
	)
	if err != nil || parentID == 0 {
		return linuxMount{}, fmt.Errorf(
			"%w: invalid parent mount ID",
			ErrInvalidMountInfo,
		)
	}

	major, minor, err := parseLinuxMountDevice(
		fields[2],
	)
	if err != nil {
		return linuxMount{}, err
	}

	root, err := decodeLinuxMountField(fields[3])
	if err != nil {
		return linuxMount{}, err
	}

	mountPoint, err := decodeLinuxMountField(
		fields[4],
	)
	if err != nil {
		return linuxMount{}, err
	}

	if !strings.HasPrefix(mountPoint, "/") {
		return linuxMount{}, fmt.Errorf(
			"%w: mount point is not absolute",
			ErrInvalidMountInfo,
		)
	}

	filesystemType, err := decodeLinuxMountField(
		fields[separator+1],
	)
	if err != nil {
		return linuxMount{}, err
	}

	source, err := decodeLinuxMountField(
		fields[separator+2],
	)
	if err != nil {
		return linuxMount{}, err
	}

	mountOptions := fields[5]
	superOptions := fields[separator+3]

	// A mount is effectively read-only if either
	// the mount or its underlying superblock
	// is marked read-only.
	readOnly := hasLinuxMountOption(
		mountOptions,
		"ro",
	) || hasLinuxMountOption(
		superOptions,
		"ro",
	)

	return linuxMount{
		ID:             id,
		ParentID:       parentID,
		Major:          major,
		Minor:          minor,
		Root:           root,
		MountPoint:     mountPoint,
		Source:         source,
		FilesystemType: filesystemType,
		ReadOnly:       readOnly,
	}, nil
}

// parseLinuxMountDevice parses the kernel's
// major:minor device identifier.
func parseLinuxMountDevice(
	value string,
) (uint32, uint32, error) {
	majorText, minorText, found := strings.Cut(
		value,
		":",
	)

	if !found {
		return 0, 0, fmt.Errorf(
			"%w: missing device separator",
			ErrInvalidMountInfo,
		)
	}

	major, err := strconv.ParseUint(
		majorText,
		10,
		32,
	)
	if err != nil {
		return 0, 0, fmt.Errorf(
			"%w: invalid major device number",
			ErrInvalidMountInfo,
		)
	}

	minor, err := strconv.ParseUint(
		minorText,
		10,
		32,
	)
	if err != nil {
		return 0, 0, fmt.Errorf(
			"%w: invalid minor device number",
			ErrInvalidMountInfo,
		)
	}

	return uint32(major), uint32(minor), nil
}

// decodeLinuxMountField decodes octal escapes
// used in mountinfo fields.
//
// Common escapes include:
//
// \040 = space
// \011 = tab
// \012 = newline
// \134 = backslash
// \043 = number sign
func decodeLinuxMountField(
	value string,
) (string, error) {
	if value == "" {
		return "", fmt.Errorf(
			"%w: empty mount field",
			ErrInvalidMountInfo,
		)
	}

	var decoded strings.Builder

	decoded.Grow(len(value))

	for i := 0; i < len(value); i++ {
		if value[i] == 0 {
			return "", fmt.Errorf(
				"%w: null byte in mount field",
				ErrInvalidMountInfo,
			)
		}

		if value[i] != '\\' {
			decoded.WriteByte(value[i])
			continue
		}

		if i+3 >= len(value) {
			return "", fmt.Errorf(
				"%w: incomplete mount escape",
				ErrInvalidMountInfo,
			)
		}

		escaped, err := strconv.ParseUint(
			value[i+1:i+4],
			8,
			8,
		)

		if err != nil || escaped == 0 {
			return "", fmt.Errorf(
				"%w: invalid mount escape",
				ErrInvalidMountInfo,
			)
		}

		decoded.WriteByte(byte(escaped))

		i += 3
	}

	return decoded.String(), nil
}

// hasLinuxMountOption checks complete option names
// rather than matching arbitrary substrings.
func hasLinuxMountOption(
	options string,
	target string,
) bool {
	for _, option := range strings.Split(options, ",") {
		if option == target {
			return true
		}
	}

	return false
}

// filterLinuxMounts selects filesystems eligible
// for initial local storage monitoring.
func filterLinuxMounts(
	mounts []linuxMount,
) []linuxMount {
	result := make([]linuxMount, 0, len(mounts))

	for _, mount := range mounts {
		if isMonitoredLinuxFilesystem(mount) {
			result = append(result, mount)
		}
	}

	// Stable ordering simplifies diagnostics,
	// serialization, and future comparisons.
	sort.Slice(result, func(i, j int) bool {
		if result[i].MountPoint == result[j].MountPoint {
			return result[i].ID < result[j].ID
		}

		return result[i].MountPoint <
			result[j].MountPoint
	})

	return result
}

// isMonitoredLinuxFilesystem defines the initial
// filesystem inclusion policy.
func isMonitoredLinuxFilesystem(
	mount linuxMount,
) bool {
	switch mount.FilesystemType {
	case "ext2", "ext3", "ext4",
		"xfs", "btrfs", "zfs",
		"bcachefs", "f2fs",
		"jfs", "reiserfs", "nilfs2",
		"vfat", "msdos", "exfat",
		"ntfs", "ntfs3",
		"hfs", "hfsplus",
		"udf", "iso9660":
		return true

	case "overlay":
		// Include container root filesystems,
		// but avoid enumerating every OverlayFS
		// mount associated with container layers.
		return mount.MountPoint == "/"

	default:
		return false
	}
}
