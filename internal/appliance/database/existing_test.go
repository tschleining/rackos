package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenExistingDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	existing, err := OpenExisting(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer existing.Close()

	assertInventorySchemaVersion(
		t,
		existing,
		len(migrations),
	)

	_, err = existing.ExecContext(
		ctx,
		`INSERT INTO appliance_metadata (key, value)
		 VALUES (?, ?)`,
		"existing_test",
		"write succeeded",
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenExistingRejectsMissingDatabase(
	t *testing.T,
) {
	dir := filepath.Join(
		t.TempDir(),
		"missing",
	)

	db, err := OpenExisting(
		context.Background(),
		dir,
	)
	if err == nil {
		db.Close()
		t.Fatal("missing database was accepted")
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("missing data directory was created")
	}
}

func TestOpenExistingDoesNotMigrate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	createAdministratorVersionSixDatabase(
		t,
		dir,
	)

	db, err := OpenExisting(ctx, dir)
	if err == nil {
		db.Close()
		t.Fatal("outdated schema was accepted")
	}

	// Open the database directly to inspect the
	// version without applying migrations.
	raw, err := openRawExistingTestDatabase(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()

	assertInventorySchemaVersion(t, raw, 6)

	inventoryAssertCount(t, raw, 0, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE name = 'administrators'
	`)
}

func TestOpenExistingRejectsNewerSchema(
	t *testing.T,
) {
	ctx := context.Background()
	dir := t.TempDir()

	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(
		ctx,
		"PRAGMA user_version = 999",
	); err != nil {
		db.Close()
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	existing, err := OpenExisting(ctx, dir)
	if err == nil {
		existing.Close()
		t.Fatal("unsupported newer schema accepted")
	}
}

func TestOpenExistingRejectsDirectory(t *testing.T) {
	dir := t.TempDir()

	if err := os.Mkdir(
		filepath.Join(dir, Filename),
		0700,
	); err != nil {
		t.Fatal(err)
	}

	db, err := OpenExisting(
		context.Background(),
		dir,
	)
	if err == nil {
		db.Close()
		t.Fatal("directory accepted as database")
	}
}

func TestOpenExistingCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	db, err := OpenExisting(ctx, t.TempDir())
	if err == nil {
		db.Close()
		t.Fatal("canceled context accepted")
	}
}
