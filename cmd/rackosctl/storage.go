package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tschleining/rackos/internal/appliance/config"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/performance"
)

const storageCommandTimeout = 10 * time.Second

func runStorage(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if len(args) == 0 ||
		args[0] == "help" ||
		args[0] == "--help" ||
		args[0] == "-h" {
		printStorageUsage(stdout)
		return nil
	}

	if args[0] != "stats" {
		return fmt.Errorf(
			"unknown storage command: %q",
			args[0],
		)
	}

	defaultDir := config.Default().DataDir

	if value, ok := os.LookupEnv(
		"RACKOS_DATA_DIR",
	); ok {
		defaultDir = value
	}

	flags := flag.NewFlagSet(
		"storage stats",
		flag.ContinueOnError,
	)

	flags.SetOutput(stderr)

	dataDir := flags.String(
		"data-dir",
		defaultDir,
		"appliance data directory",
	)

	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	if flags.NArg() != 0 {
		return errors.New(
			"unexpected positional arguments",
		)
	}

	if strings.TrimSpace(*dataDir) == "" {
		return errors.New(
			"data directory cannot be empty",
		)
	}

	ctx, cancel := context.WithTimeout(
		ctx,
		storageCommandTimeout,
	)
	defer cancel()

	db, err := database.OpenReadOnly(
		ctx,
		*dataDir,
	)
	if err != nil {
		return err
	}
	defer db.Close()

	repository := performance.NewRepository(db)

	statistics, err := repository.Statistics(ctx)
	if err != nil {
		return fmt.Errorf(
			"inspect performance statistics: %w",
			err,
		)
	}

	usage, err := database.InspectStorage(
		ctx,
		db,
	)
	if err != nil {
		return fmt.Errorf(
			"inspect database storage: %w",
			err,
		)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	return writeStorageStatistics(
		stdout,
		statistics,
		usage,
	)
}

// writeStorageStatistics renders logical historical
// storage and physical database observations.
//
// All measurements must be retrieved successfully
// before the report is written.
func writeStorageStatistics(
	w io.Writer,
	statistics performance.StorageStatistics,
	usage database.StorageUsage,
) error {
	_, err := fmt.Fprintf(w, `RackOS Storage Statistics

Performance Storage
-------------------
Historical samples:    %d
Reporting devices:     %d
Payload size:          %s

Oldest collection:     %s
Latest collection:     %s
Oldest receipt:        %s
Latest receipt:        %s

SQLite Files
------------
Database size:         %s
WAL size:              %s
SHM size:              %s

SQLite Page Usage
-----------------
Page size:             %s
Total pages:           %d
Reusable pages:        %d
Allocated capacity:    %s
Reusable capacity:     %s
Non-freelist capacity: %s

Notes:
  Payload size excludes database overhead and indexes.
  File sizes represent file lengths, not allocated disk blocks.
  Reusable pages may be used by future database writes.
  Deletion does not necessarily shrink database files.
  Measurements may differ during concurrent writes.
`,
		statistics.TotalSamples,
		statistics.ReportingDevices,
		formatStorageBytes(statistics.PayloadBytes),

		formatStorageTimestamp(statistics.OldestCollectedAt),
		formatStorageTimestamp(statistics.LatestCollectedAt),
		formatStorageTimestamp(statistics.OldestReceivedAt),
		formatStorageTimestamp(statistics.LatestReceivedAt),

		formatStorageBytes(usage.DatabaseBytes),
		formatStorageBytes(usage.WALBytes),
		formatStorageBytes(usage.SHMBytes),

		formatStorageBytes(usage.PageSize),
		usage.PageCount,
		usage.FreePageCount,
		formatStorageBytes(usage.AllocatedBytes),
		formatStorageBytes(usage.ReusableBytes),
		formatStorageBytes(usage.NonFreelistBytes),
	)

	return err
}

// formatStorageBytes presents binary storage units
// while preserving the original exact byte count.
func formatStorageBytes(value int64) string {
	const unit = 1024

	if value < unit {
		return fmt.Sprintf("%d B", value)
	}

	units := []string{
		"KiB",
		"MiB",
		"GiB",
		"TiB",
		"PiB",
		"EiB",
	}

	size := float64(value)

	for _, name := range units {
		size /= unit

		if size < unit || name == "EiB" {
			return fmt.Sprintf(
				"%.2f %s (%d bytes)",
				size,
				name,
				value,
			)
		}
	}

	return fmt.Sprintf("%d B", value)
}

// formatStorageTimestamp distinguishes missing
// history from an actual stored timestamp.
func formatStorageTimestamp(
	value *time.Time,
) string {
	if value == nil {
		return "none"
	}

	return value.UTC().Format(
		time.RFC3339Nano,
	)
}

func printStorageUsage(w io.Writer) {
	fmt.Fprint(w, `RackOS storage administration

Usage:
  rackosctl storage stats [--data-dir DIR]
  rackosctl storage help

Commands:
  stats    Display historical performance and SQLite storage usage.

The storage command opens an existing appliance
database in read-only mode.

No retention cleanup, migration, checkpoint, or
vacuum is initiated by this command.

The default data directory is ./data, or the value of
RACKOS_DATA_DIR when configured.
`)
}
