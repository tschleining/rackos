package database

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupStorageInspectionDatabase(
	t *testing.T,
) (context.Context, *sql.DB, string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	t.Cleanup(cancel)

	// Include spaces to verify database path handling.
	dir := filepath.Join(
		t.TempDir(),
		"storage inspection",
	)

	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return ctx, db, dir
}

func requireStorageUsageConsistency(
	t *testing.T,
	usage StorageUsage,
) {
	t.Helper()

	if usage.PageSize <= 0 {
		t.Fatal("invalid page size")
	}

	if usage.PageCount < 0 {
		t.Fatal("invalid page count")
	}

	if usage.FreePageCount < 0 ||
		usage.FreePageCount > usage.PageCount {
		t.Fatal("invalid free page count")
	}

	if usage.AllocatedBytes !=
		usage.PageCount*usage.PageSize {
		t.Fatal("incorrect allocated page capacity")
	}

	if usage.ReusableBytes !=
		usage.FreePageCount*usage.PageSize {
		t.Fatal("incorrect reusable page capacity")
	}

	if usage.NonFreelistBytes !=
		usage.AllocatedBytes-usage.ReusableBytes {
		t.Fatal("incorrect non-freelist capacity")
	}

	if usage.DatabaseBytes < 0 ||
		usage.WALBytes < 0 ||
		usage.SHMBytes < 0 {
		t.Fatal("invalid filesystem size")
	}
}

func TestInspectStorage(t *testing.T) {
	ctx, db, dir :=
		setupStorageInspectionDatabase(t)

	usage, err := InspectStorage(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	requireStorageUsageConsistency(
		t,
		usage,
	)

	if usage.DatabaseBytes <= 0 {
		t.Fatal("missing database file size")
	}

	if usage.PageCount <= 0 {
		t.Fatal("missing database pages")
	}

	path := filepath.Join(dir, Filename)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if usage.DatabaseBytes != info.Size() {
		t.Fatal("incorrect database file size")
	}
}

func TestInspectStoragePageStatistics(t *testing.T) {
	ctx, db, _ :=
		setupStorageInspectionDatabase(t)

	usage, err := InspectStorage(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		query    string
		expected int64
	}{
		{
			"PRAGMA page_size",
			usage.PageSize,
		},
		{
			"PRAGMA page_count",
			usage.PageCount,
		},
		{
			"PRAGMA freelist_count",
			usage.FreePageCount,
		},
	}

	for _, tt := range tests {
		var actual int64

		err := db.QueryRowContext(
			ctx,
			tt.query,
		).Scan(&actual)
		if err != nil {
			t.Fatal(err)
		}

		if actual != tt.expected {
			t.Fatalf(
				"%s: expected %d, got %d",
				tt.query,
				tt.expected,
				actual,
			)
		}
	}
}

func TestInspectStorageWALFiles(t *testing.T) {
	ctx, db, dir :=
		setupStorageInspectionDatabase(t)

	// Disable automatic checkpoints for this
	// isolated test connection only.
	_, err := db.ExecContext(
		ctx,
		"PRAGMA wal_autocheckpoint = 0",
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO appliance_metadata (key, value)
		VALUES ('storage-test', 'test-value')
	`)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, Filename)

	walInfo, err := os.Stat(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}

	shmInfo, err := os.Stat(path + "-shm")
	if err != nil {
		t.Fatal(err)
	}

	usage, err := InspectStorage(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	requireStorageUsageConsistency(t, usage)

	if usage.WALBytes <= 0 {
		t.Fatal("missing WAL storage")
	}

	if usage.WALBytes != walInfo.Size() {
		t.Fatal("incorrect WAL file size")
	}

	if usage.SHMBytes != shmInfo.Size() {
		t.Fatal("incorrect SHM file size")
	}

	// Inspection must not change the WAL size.
	after, err := os.Stat(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}

	if after.Size() != walInfo.Size() {
		t.Fatal("inspection changed WAL file size")
	}

	var checkpointInterval int

	err = db.QueryRowContext(
		ctx,
		"PRAGMA wal_autocheckpoint",
	).Scan(&checkpointInterval)
	if err != nil {
		t.Fatal(err)
	}

	if checkpointInterval != 0 {
		t.Fatal("inspection changed checkpoint policy")
	}
}

func TestInspectStorageWithoutWALFiles(t *testing.T) {
	ctx := context.Background()

	path := filepath.Join(
		t.TempDir(),
		"test.db",
	)

	// Use a separate rollback-journal database
	// to verify optional WAL and SHM handling.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `
		PRAGMA journal_mode = DELETE;

		CREATE TABLE test_data (
			id INTEGER PRIMARY KEY
		);

		INSERT INTO test_data VALUES (1);
	`)
	if err != nil {
		t.Fatal(err)
	}

	usage, err := InspectStorage(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	requireStorageUsageConsistency(t, usage)

	if usage.DatabaseBytes <= 0 {
		t.Fatal("missing database file size")
	}

	if usage.WALBytes != 0 {
		t.Fatal("absent WAL file reported storage")
	}

	if usage.SHMBytes != 0 {
		t.Fatal("absent SHM file reported storage")
	}
}

func TestInspectStorageReusablePages(t *testing.T) {
	ctx, db, _ :=
		setupStorageInspectionDatabase(t)

	var autoVacuum int

	err := db.QueryRowContext(
		ctx,
		"PRAGMA auto_vacuum",
	).Scan(&autoVacuum)
	if err != nil {
		t.Fatal(err)
	}

	if autoVacuum != 0 {
		t.Fatal("unexpected automatic vacuum mode")
	}

	_, err = db.ExecContext(ctx, `
		CREATE TABLE storage_test_payloads (
			payload BLOB NOT NULL
		)
	`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO storage_test_payloads (payload)
		VALUES (zeroblob(1048576))
	`)
	if err != nil {
		t.Fatal(err)
	}

	before, err := InspectStorage(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.ExecContext(
		ctx,
		"DROP TABLE storage_test_payloads",
	)
	if err != nil {
		t.Fatal(err)
	}

	after, err := InspectStorage(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	requireStorageUsageConsistency(t, before)
	requireStorageUsageConsistency(t, after)

	if after.FreePageCount <= before.FreePageCount {
		t.Fatal("deleted data did not create reusable pages")
	}

	if after.ReusableBytes <= before.ReusableBytes {
		t.Fatal("reusable capacity did not increase")
	}

	if after.PageCount != before.PageCount {
		t.Fatal("logical database page count changed")
	}

	if after.AllocatedBytes != before.AllocatedBytes {
		t.Fatal("logical database capacity changed")
	}

	if after.NonFreelistBytes >= before.NonFreelistBytes {
		t.Fatal("non-freelist capacity did not decrease")
	}
}

func TestInspectStorageReadOnlyBehavior(t *testing.T) {
	ctx, db, _ :=
		setupStorageInspectionDatabase(t)

	_, err := db.ExecContext(ctx, `
		INSERT INTO appliance_metadata (key, value)
		VALUES ('storage-inspection', 'preserve-me')
	`)
	if err != nil {
		t.Fatal(err)
	}

	before, err := InspectStorage(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	after, err := InspectStorage(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	if before != after {
		t.Fatal("inspection changed storage usage")
	}

	var value string

	err = db.QueryRowContext(ctx, `
		SELECT value
		FROM appliance_metadata
		WHERE key = 'storage-inspection'
	`).Scan(&value)
	if err != nil {
		t.Fatal(err)
	}

	if value != "preserve-me" {
		t.Fatal("inspection modified stored data")
	}

	var version int

	err = db.QueryRowContext(
		ctx,
		"PRAGMA user_version",
	).Scan(&version)
	if err != nil {
		t.Fatal(err)
	}

	if version != len(migrations) {
		t.Fatal("inspection changed schema version")
	}
}

func TestInspectStorageDatabaseRestart(t *testing.T) {
	ctx, db, dir :=
		setupStorageInspectionDatabase(t)

	before, err := InspectStorage(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	after, err := InspectStorage(ctx, reopened)
	if err != nil {
		t.Fatal(err)
	}

	requireStorageUsageConsistency(t, after)

	if after.PageSize != before.PageSize ||
		after.PageCount != before.PageCount ||
		after.FreePageCount != before.FreePageCount ||
		after.AllocatedBytes != before.AllocatedBytes {
		t.Fatal("logical page statistics changed after restart")
	}

	// Physical WAL and SHM sizes may legitimately
	// change when SQLite connections are closed.
}

func TestInspectStorageCanceledContext(t *testing.T) {
	ctx, db, _ :=
		setupStorageInspectionDatabase(t)

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	usage, err := InspectStorage(canceled, db)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context, got %v", err)
	}

	if usage != (StorageUsage{}) {
		t.Fatal("canceled inspection returned results")
	}
}

func TestInspectStorageDatabaseFailure(t *testing.T) {
	ctx, db, _ :=
		setupStorageInspectionDatabase(t)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	usage, err := InspectStorage(ctx, db)

	if err == nil {
		t.Fatal("closed database returned storage usage")
	}

	if usage != (StorageUsage{}) {
		t.Fatal("failed inspection returned partial results")
	}
}

func TestInspectStorageNilDatabase(t *testing.T) {
	usage, err := InspectStorage(
		context.Background(),
		nil,
	)

	if !errors.Is(err, ErrInvalidStorageUsage) {
		t.Fatalf("unexpected error: %v", err)
	}

	if usage != (StorageUsage{}) {
		t.Fatal("missing database returned storage usage")
	}
}

func TestInspectStorageInMemoryDatabase(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open(
		"sqlite",
		":memory:",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	usage, err := InspectStorage(ctx, db)

	if !errors.Is(err, ErrInvalidStorageUsage) {
		t.Fatalf("expected file-backed database error, got %v", err)
	}

	if usage != (StorageUsage{}) {
		t.Fatal("in-memory database returned filesystem usage")
	}
}

func TestCalculateStoragePageUsage(t *testing.T) {
	tests := []struct {
		name      string
		pageSize  int64
		pageCount int64
		freePages int64
		allocated int64
		reusable  int64
	}{
		{
			"empty database",
			4096, 0, 0,
			0, 0,
		},
		{
			"no free pages",
			4096, 100, 0,
			409600, 0,
		},
		{
			"reusable pages",
			4096, 100, 25,
			409600, 102400,
		},
		{
			"minimum page size",
			512, 10, 2,
			5120, 1024,
		},
		{
			"maximum page size",
			65536, 10, 2,
			655360, 131072,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := StorageUsage{
				PageSize:      tt.pageSize,
				PageCount:     tt.pageCount,
				FreePageCount: tt.freePages,
			}

			if err := calculateStoragePageUsage(
				&usage,
			); err != nil {
				t.Fatal(err)
			}

			if usage.AllocatedBytes != tt.allocated {
				t.Fatal("incorrect allocated capacity")
			}

			if usage.ReusableBytes != tt.reusable {
				t.Fatal("incorrect reusable capacity")
			}

			requireStorageUsageConsistency(
				t,
				usage,
			)
		})
	}
}

func TestCalculateStoragePageUsageInvalid(t *testing.T) {
	tests := []struct {
		name      string
		pageSize  int64
		pageCount int64
		freePages int64
	}{
		{"zero page size", 0, 10, 0},
		{"negative page size", -1, 10, 0},
		{"small page size", 256, 10, 0},
		{"non-power-of-two size", 1000, 10, 0},
		{"large page size", 131072, 10, 0},
		{"negative page count", 4096, -1, 0},
		{"negative free pages", 4096, 10, -1},
		{"excessive free pages", 4096, 10, 11},
		{"integer overflow", 4096, math.MaxInt64, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage := StorageUsage{
				PageSize:      tt.pageSize,
				PageCount:     tt.pageCount,
				FreePageCount: tt.freePages,
			}

			err := calculateStoragePageUsage(
				&usage,
			)

			if !errors.Is(err, ErrInvalidStorageUsage) {
				t.Fatalf(
					"expected invalid storage usage, got %v",
					err,
				)
			}

			if usage.AllocatedBytes != 0 ||
				usage.ReusableBytes != 0 ||
				usage.NonFreelistBytes != 0 {
				t.Fatal("invalid page statistics returned capacity")
			}
		})
	}
}

func TestStorageFileSize(t *testing.T) {
	path := filepath.Join(
		t.TempDir(),
		"storage-test.db",
	)

	data := []byte("test database contents")

	if err := os.WriteFile(
		path,
		data,
		0600,
	); err != nil {
		t.Fatal(err)
	}

	for _, optional := range []bool{false, true} {
		size, err := storageFileSize(
			path,
			optional,
		)
		if err != nil {
			t.Fatal(err)
		}

		if size != int64(len(data)) {
			t.Fatal("incorrect file size")
		}
	}
}

func TestStorageFileSizeMissing(t *testing.T) {
	path := filepath.Join(
		t.TempDir(),
		"missing.db",
	)

	size, err := storageFileSize(path, true)
	if err != nil {
		t.Fatal(err)
	}

	if size != 0 {
		t.Fatal("missing optional file has nonzero size")
	}

	size, err = storageFileSize(path, false)

	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected missing file error, got %v", err)
	}

	if size != 0 {
		t.Fatal("missing required file has nonzero size")
	}
}

func TestStorageFileSizeNonRegular(t *testing.T) {
	dir := t.TempDir()

	for _, optional := range []bool{false, true} {
		size, err := storageFileSize(
			dir,
			optional,
		)

		if !errors.Is(err, ErrInvalidStorageUsage) {
			t.Fatalf("expected invalid file error, got %v", err)
		}

		if size != 0 {
			t.Fatal("invalid file returned nonzero size")
		}
	}
}
