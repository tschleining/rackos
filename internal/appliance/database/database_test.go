package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpen(t *testing.T) {
	ctx := context.Background()
	dataDir := filepath.Join(t.TempDir(), "data")

	db, err := Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	path := filepath.Join(dataDir, Filename)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database file not created: %v", err)
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
		t.Fatalf(
			"expected schema version %d, got %d",
			len(migrations),
			version,
		)
	}
}

func TestPersistence(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	db, err := Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.ExecContext(
		ctx,
		"INSERT INTO appliance_metadata (key, value) VALUES (?, ?)",
		"test_key",
		"test_value",
	)

	if err != nil {
		db.Close()
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var value string

	err = db.QueryRowContext(
		ctx,
		"SELECT value FROM appliance_metadata WHERE key = ?",
		"test_key",
	).Scan(&value)

	if err != nil {
		t.Fatal(err)
	}

	if value != "test_value" {
		t.Errorf("unexpected value: %q", value)
	}
}

func TestUnsupportedSchemaVersion(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	db, err := Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.ExecContext(
		ctx,
		"PRAGMA user_version = 999",
	)

	if err != nil {
		db.Close()
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(ctx, dataDir)
	if err == nil {
		db.Close()
		t.Fatal("expected unsupported schema version error")
	}
}

func TestForeignKeysEnabled(t *testing.T) {
	ctx := context.Background()

	db, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var enabled int

	err = db.QueryRowContext(
		ctx,
		"PRAGMA foreign_keys",
	).Scan(&enabled)

	if err != nil {
		t.Fatal(err)
	}

	if enabled != 1 {
		t.Fatal("foreign keys are not enabled")
	}
}
