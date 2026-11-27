package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
)

// StorageUsage describes SQLite database file sizes
// and logical database page utilization.
//
// File sizes describe file lengths, not filesystem
// block allocation.
//
// Logical page statistics include the database state
// visible through SQLite, including committed WAL data.
//
// File sizes and logical page statistics are separate
// observations and may differ during concurrent writes.
type StorageUsage struct {
	DatabaseBytes int64
	WALBytes      int64
	SHMBytes      int64

	PageSize      int64
	PageCount     int64
	FreePageCount int64

	AllocatedBytes   int64
	ReusableBytes    int64
	NonFreelistBytes int64
}

var ErrInvalidStorageUsage = errors.New(
	"invalid SQLite storage usage",
)

// storageUsageSQL retrieves the main database path
// and logical page statistics.
//
// The database path originates from the active SQLite
// connection rather than a separate configuration value.
const storageUsageSQL = `
	SELECT
		file,
		(SELECT page_size FROM pragma_page_size),
		(SELECT page_count FROM pragma_page_count),
		(SELECT freelist_count FROM pragma_freelist_count)
	FROM pragma_database_list
	WHERE name = 'main'
`

// InspectStorage retrieves logical SQLite page usage
// and filesystem sizes for an open appliance database.
//
// Missing WAL and SHM files are reported as zero bytes.
//
// This operation does not initiate a checkpoint,
// vacuum, or other database maintenance operation.
//
// The caller remains responsible for the database
// connection lifecycle.
func InspectStorage(
	ctx context.Context,
	db *sql.DB,
) (StorageUsage, error) {
	if err := ctx.Err(); err != nil {
		return StorageUsage{}, err
	}

	if db == nil {
		return StorageUsage{}, fmt.Errorf(
			"%w: missing database connection",
			ErrInvalidStorageUsage,
		)
	}

	var usage StorageUsage
	var databasePath string

	err := db.QueryRowContext(
		ctx,
		storageUsageSQL,
	).Scan(
		&databasePath,
		&usage.PageSize,
		&usage.PageCount,
		&usage.FreePageCount,
	)
	if err != nil {
		return StorageUsage{}, fmt.Errorf(
			"query SQLite storage usage: %w",
			err,
		)
	}

	if databasePath == "" {
		return StorageUsage{}, fmt.Errorf(
			"%w: database is not file-backed",
			ErrInvalidStorageUsage,
		)
	}

	if err := calculateStoragePageUsage(
		&usage,
	); err != nil {
		return StorageUsage{}, err
	}

	files := []struct {
		name     string
		path     string
		optional bool
		target   *int64
	}{
		{
			name:   "database",
			path:   databasePath,
			target: &usage.DatabaseBytes,
		},
		{
			name:     "WAL",
			path:     databasePath + "-wal",
			optional: true,
			target:   &usage.WALBytes,
		},
		{
			name:     "SHM",
			path:     databasePath + "-shm",
			optional: true,
			target:   &usage.SHMBytes,
		},
	}

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return StorageUsage{}, err
		}

		size, err := storageFileSize(
			file.path,
			file.optional,
		)
		if err != nil {
			return StorageUsage{}, fmt.Errorf(
				"inspect SQLite %s file: %w",
				file.name,
				err,
			)
		}

		*file.target = size
	}

	if err := ctx.Err(); err != nil {
		return StorageUsage{}, err
	}

	return usage, nil
}

// calculateStoragePageUsage validates SQLite page
// statistics and calculates their byte capacities.
func calculateStoragePageUsage(
	usage *StorageUsage,
) error {
	pageSize := usage.PageSize

	// SQLite page sizes must be powers of two
	// between 512 and 65536 bytes.
	if pageSize < 512 ||
		pageSize > 65536 ||
		pageSize&(pageSize-1) != 0 {
		return fmt.Errorf(
			"%w: invalid page size",
			ErrInvalidStorageUsage,
		)
	}

	if usage.PageCount < 0 {
		return fmt.Errorf(
			"%w: invalid page count",
			ErrInvalidStorageUsage,
		)
	}

	if usage.FreePageCount < 0 ||
		usage.FreePageCount > usage.PageCount {
		return fmt.Errorf(
			"%w: invalid free page count",
			ErrInvalidStorageUsage,
		)
	}

	if usage.PageCount > math.MaxInt64/pageSize {
		return fmt.Errorf(
			"%w: page capacity exceeds integer range",
			ErrInvalidStorageUsage,
		)
	}

	usage.AllocatedBytes =
		usage.PageCount * pageSize

	usage.ReusableBytes =
		usage.FreePageCount * pageSize

	usage.NonFreelistBytes =
		usage.AllocatedBytes - usage.ReusableBytes

	return nil
}

// storageFileSize retrieves a regular file's length.
//
// Optional files may be absent during normal SQLite
// journal lifecycle operations.
//
// Other filesystem errors are returned to the caller.
func storageFileSize(
	path string,
	optional bool,
) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		if optional && errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}

		return 0, err
	}

	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf(
			"%w: expected a regular file",
			ErrInvalidStorageUsage,
		)
	}

	return info.Size(), nil
}
