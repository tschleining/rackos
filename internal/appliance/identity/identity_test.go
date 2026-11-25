package identity

import (
	"context"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/database"
)

func TestLoadOrCreate(t *testing.T) {
	ctx := context.Background()

	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	id, err := LoadOrCreate(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	if !idPattern.MatchString(id) {
		t.Fatalf("invalid installation ID: %q", id)
	}

	loadedID, err := LoadOrCreate(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	if loadedID != id {
		t.Fatalf("identity changed: %s != %s", loadedID, id)
	}
}

func TestIdentityPersistence(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()

	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}

	id, err := LoadOrCreate(ctx, db)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	loadedID, err := LoadOrCreate(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	if loadedID != id {
		t.Fatalf(
			"identity changed after restart: %s != %s",
			loadedID,
			id,
		)
	}
}

func TestUniqueInstallations(t *testing.T) {
	ctx := context.Background()

	db1, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db1.Close()

	db2, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()

	id1, err := LoadOrCreate(ctx, db1)
	if err != nil {
		t.Fatal(err)
	}

	id2, err := LoadOrCreate(ctx, db2)
	if err != nil {
		t.Fatal(err)
	}

	if id1 == id2 {
		t.Fatal("independent installations share an identity")
	}
}

func TestInvalidStoredIdentity(t *testing.T) {
	ctx := context.Background()

	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.ExecContext(
		ctx,
		"INSERT INTO appliance_metadata (key, value) VALUES (?, ?)",
		metadataKey,
		"invalid-identity",
	)

	if err != nil {
		t.Fatal(err)
	}

	if _, err := LoadOrCreate(ctx, db); err == nil {
		t.Fatal("expected invalid identity error")
	}

	var stored string

	err = db.QueryRowContext(
		ctx,
		"SELECT value FROM appliance_metadata WHERE key = ?",
		metadataKey,
	).Scan(&stored)

	if err != nil {
		t.Fatal(err)
	}

	if stored != "invalid-identity" {
		t.Fatal("invalid identity was unexpectedly overwritten")
	}
}

func TestDatabaseUnavailable(t *testing.T) {
	ctx := context.Background()

	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadOrCreate(ctx, db); err == nil {
		t.Fatal("expected database error")
	}
}
