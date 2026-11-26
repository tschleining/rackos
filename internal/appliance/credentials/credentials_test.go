package credentials

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
)

const (
	deviceID1 = "e2a104d8-9c72-4b31-8fa2-63001b24c9e8"
	deviceID2 = "e2a104d8-9c72-4b31-8fa2-63001b24c9e9"
)

func testDatabase(t *testing.T) *sql.DB {
	t.Helper()

	db, err := database.Open(
		context.Background(),
		t.TempDir(),
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	repo := devices.NewRepository(db)

	for _, id := range []string{
		deviceID1,
		deviceID2,
	} {
		_, err := repo.Create(
			context.Background(),
			id,
			devices.DeviceInfo{
				Hostname:     "test-server",
				OS:           "linux",
				Architecture: "amd64",
				AgentVersion: "0.1.0-dev",
			},
		)

		if err != nil {
			t.Fatal(err)
		}
	}

	return db
}

func issueCredential(
	ctx context.Context,
	db *sql.DB,
	deviceID string,
) (string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	token, err := Issue(ctx, tx, deviceID)
	if err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return token, nil
}

func TestIssueAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	token, err := issueCredential(
		ctx, db, deviceID1,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(token, tokenPrefix) {
		t.Fatal("invalid credential prefix")
	}

	if err := Authenticate(
		ctx, db, deviceID1, token,
	); err != nil {
		t.Fatal(err)
	}

	var stored []byte

	err = db.QueryRowContext(
		ctx,
		`SELECT token_hash
		 FROM agent_credentials
		 WHERE device_id = ?`,
		deviceID1,
	).Scan(&stored)

	if err != nil {
		t.Fatal(err)
	}

	expected := sha256.Sum256([]byte(token))

	if !bytes.Equal(stored, expected[:]) {
		t.Fatal("stored credential hash is incorrect")
	}
}

func TestWrongDevice(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	token1, err := issueCredential(
		ctx, db, deviceID1,
	)
	if err != nil {
		t.Fatal(err)
	}

	token2, err := issueCredential(
		ctx, db, deviceID2,
	)
	if err != nil {
		t.Fatal(err)
	}

	if token1 == token2 {
		t.Fatal("devices received identical credentials")
	}

	err = Authenticate(
		ctx, db, deviceID2, token1,
	)

	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected authentication rejection, got %v", err)
	}

	if err := Authenticate(
		ctx, db, deviceID2, token2,
	); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidCredentials(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	token, err := issueCredential(
		ctx, db, deviceID1,
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, invalid := range []string{
		"",
		"invalid-token",
		"rackos_enroll_" + strings.Repeat("A", 43),
		tokenPrefix + strings.Repeat("A", 43),
		token + "extra",
	} {
		err := Authenticate(
			ctx, db, deviceID1, invalid,
		)

		if !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf(
				"expected authentication rejection, got %v",
				err,
			)
		}
	}
}

func TestRevoke(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	token, err := issueCredential(
		ctx, db, deviceID1,
	)
	if err != nil {
		t.Fatal(err)
	}

	otherToken, err := issueCredential(
		ctx, db, deviceID2,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := Revoke(ctx, db, deviceID1); err != nil {
		t.Fatal(err)
	}

	err = Authenticate(
		ctx, db, deviceID1, token,
	)

	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("revoked credential accepted: %v", err)
	}

	if err := Authenticate(
		ctx, db, deviceID2, otherToken,
	); err != nil {
		t.Fatal("revocation affected another device")
	}

	err = Revoke(ctx, db, deviceID1)

	if !errors.Is(err, ErrNotRevocable) {
		t.Fatalf("expected revocation rejection, got %v", err)
	}
}

func TestDuplicateCredential(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	token, err := issueCredential(
		ctx, db, deviceID1,
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := issueCredential(
		ctx, db, deviceID1,
	); err == nil {
		t.Fatal("duplicate credential was accepted")
	}

	if err := Authenticate(
		ctx, db, deviceID1, token,
	); err != nil {
		t.Fatal("original credential was overwritten")
	}
}

func TestUnknownDevice(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	token, err := issueCredential(
		ctx, db, deviceID1,
	)
	if err != nil {
		t.Fatal(err)
	}

	unknownID := "unknown-device"

	if _, err := issueCredential(
		ctx, db, unknownID,
	); err == nil {
		t.Fatal("credential issued for nonexistent device")
	}

	err = Authenticate(
		ctx, db, unknownID, token,
	)

	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected authentication rejection, got %v", err)
	}
}

func TestCredentialRollback(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	token, err := Issue(ctx, tx, deviceID1)
	if err != nil {
		t.Fatal(err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	err = Authenticate(
		ctx, db, deviceID1, token,
	)

	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("rolled-back credential accepted: %v", err)
	}

	if _, err := issueCredential(
		ctx, db, deviceID1,
	); err != nil {
		t.Fatal("could not issue credential after rollback")
	}
}

func TestDeviceDeletion(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	token, err := issueCredential(
		ctx, db, deviceID1,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.ExecContext(
		ctx,
		"DELETE FROM devices WHERE id = ?",
		deviceID1,
	)
	if err != nil {
		t.Fatal(err)
	}

	var count int

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM agent_credentials
		 WHERE device_id = ?`,
		deviceID1,
	).Scan(&count)

	if err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatal("orphaned agent credential")
	}

	err = Authenticate(
		ctx, db, deviceID1, token,
	)

	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("deleted device authenticated: %v", err)
	}
}
