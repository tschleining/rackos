package database

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenReadOnly(t *testing.T) {
	ctx, writer, dir :=
		setupStorageInspectionDatabase(t)

	_, err := writer.ExecContext(ctx, `
		INSERT INTO appliance_metadata (key, value)
		VALUES ('readonly-test', 'preserved')
	`)
	if err != nil {
		t.Fatal(err)
	}

	reader, err := OpenReadOnly(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	var value string

	err = reader.QueryRowContext(ctx, `
		SELECT value
		FROM appliance_metadata
		WHERE key = 'readonly-test'
	`).Scan(&value)
	if err != nil {
		t.Fatal(err)
	}

	if value != "preserved" {
		t.Fatal("read-only connection did not see committed data")
	}

	var queryOnly int

	err = reader.QueryRowContext(
		ctx,
		"PRAGMA query_only",
	).Scan(&queryOnly)
	if err != nil {
		t.Fatal(err)
	}

	if queryOnly != 1 {
		t.Fatal("query-only protection is not enabled")
	}

	usage, err := InspectStorage(ctx, reader)
	if err != nil {
		t.Fatal(err)
	}

	requireStorageUsageConsistency(t, usage)

	// Disable query_only to verify that the
	// underlying SQLite connection remains read-only.
	_, err = reader.ExecContext(
		ctx,
		"PRAGMA query_only = OFF",
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = reader.ExecContext(ctx, `
		DELETE FROM appliance_metadata
		WHERE key = 'readonly-test'
	`)
	if err == nil {
		t.Fatal("read-only connection permitted deletion")
	}

	var count int

	err = writer.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM appliance_metadata
		WHERE key = 'readonly-test'
	`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatal("read-only connection modified stored data")
	}
}

func TestOpenReadOnlyMissingDatabase(t *testing.T) {
	dir := filepath.Join(
		t.TempDir(),
		"missing",
	)

	db, err := OpenReadOnly(
		context.Background(),
		dir,
	)
	if db != nil {
		db.Close()
		t.Fatal("missing database returned a connection")
	}

	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("read-only opener created a directory")
	}
}

func TestOpenReadOnlyEmptyDirectory(t *testing.T) {
	for _, dir := range []string{"", " "} {
		db, err := OpenReadOnly(
			context.Background(),
			dir,
		)

		if db != nil {
			db.Close()
			t.Fatal("empty directory returned a connection")
		}

		if err == nil {
			t.Fatal("empty directory was accepted")
		}
	}
}

func TestOpenReadOnlyNonRegularFile(t *testing.T) {
	dir := t.TempDir()

	err := os.Mkdir(
		filepath.Join(dir, Filename),
		0700,
	)
	if err != nil {
		t.Fatal(err)
	}

	db, err := OpenReadOnly(
		context.Background(),
		dir,
	)

	if db != nil {
		db.Close()
		t.Fatal("directory returned a database connection")
	}

	if err == nil {
		t.Fatal("non-regular database file accepted")
	}
}

func TestOpenReadOnlySchemaVersion(t *testing.T) {
	for _, version := range []int{
		0,
		len(migrations) - 1,
		len(migrations) + 1,
	} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			ctx, writer, dir :=
				setupStorageInspectionDatabase(t)

			_, err := writer.ExecContext(
				ctx,
				fmt.Sprintf(
					"PRAGMA user_version = %d",
					version,
				),
			)
			if err != nil {
				t.Fatal(err)
			}

			reader, err := OpenReadOnly(ctx, dir)

			if reader != nil {
				reader.Close()
				t.Fatal("unsupported schema returned connection")
			}

			if err == nil || !strings.Contains(
				err.Error(),
				"unsupported database schema version",
			) {
				t.Fatalf("unexpected schema error: %v", err)
			}

			var actual int

			err = writer.QueryRowContext(
				ctx,
				"PRAGMA user_version",
			).Scan(&actual)
			if err != nil {
				t.Fatal(err)
			}

			if actual != version {
				t.Fatal("read-only opener changed schema version")
			}
		})
	}
}

func TestOpenReadOnlyCanceledContext(t *testing.T) {
	ctx, _, dir :=
		setupStorageInspectionDatabase(t)

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	db, err := OpenReadOnly(canceled, dir)

	if db != nil {
		db.Close()
		t.Fatal("canceled request returned connection")
	}

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
}
