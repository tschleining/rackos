package enrollment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/database"
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

	return db
}

func consumeToken(
	ctx context.Context,
	db *sql.DB,
	token string,
) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := Consume(ctx, tx, token); err != nil {
		return err
	}

	return tx.Commit()
}

func TestIssue(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	issued, err := Issue(ctx, db, DefaultTTL)
	if err != nil {
		t.Fatal(err)
	}

	if issued.ID == "" || issued.Token == "" {
		t.Fatal("missing token information")
	}

	if issued.ExpiresAt.Sub(issued.CreatedAt) != DefaultTTL {
		t.Fatal("incorrect token expiration")
	}

	var stored []byte

	err = db.QueryRowContext(
		ctx,
		"SELECT token_hash FROM enrollment_tokens WHERE id = ?",
		issued.ID,
	).Scan(&stored)

	if err != nil {
		t.Fatal(err)
	}

	expected := sha256.Sum256([]byte(issued.Token))

	if !bytes.Equal(stored, expected[:]) {
		t.Fatal("incorrect stored token hash")
	}
}

func TestUniqueTokens(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	first, err := Issue(ctx, db, DefaultTTL)
	if err != nil {
		t.Fatal(err)
	}

	second, err := Issue(ctx, db, DefaultTTL)
	if err != nil {
		t.Fatal(err)
	}

	if first.ID == second.ID ||
		first.Token == second.Token {
		t.Fatal("duplicate enrollment token")
	}
}

func TestInvalidTTL(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	for _, ttl := range []time.Duration{
		0,
		-time.Minute,
		time.Millisecond,
		MaxTTL + time.Second,
	} {
		_, err := Issue(ctx, db, ttl)

		if !errors.Is(err, ErrInvalidTTL) {
			t.Fatalf("expected invalid TTL, got %v", err)
		}
	}
}

func TestSingleUseToken(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	issued, err := Issue(ctx, db, DefaultTTL)
	if err != nil {
		t.Fatal(err)
	}

	if err := consumeToken(
		ctx, db, issued.Token,
	); err != nil {
		t.Fatal(err)
	}

	err = consumeToken(ctx, db, issued.Token)

	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected consumed token rejection, got %v", err)
	}
}

func TestExpiredToken(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	issued, err := Issue(ctx, db, DefaultTTL)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UnixMilli()

	_, err = db.ExecContext(
		ctx,
		`UPDATE enrollment_tokens
		 SET created_at = ?, expires_at = ?
		 WHERE id = ?`,
		now-60000,
		now-1000,
		issued.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = consumeToken(ctx, db, issued.Token)

	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected expired token rejection, got %v", err)
	}
}

func TestRevokedToken(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	issued, err := Issue(ctx, db, DefaultTTL)
	if err != nil {
		t.Fatal(err)
	}

	if err := Revoke(ctx, db, issued.ID); err != nil {
		t.Fatal(err)
	}

	err = consumeToken(ctx, db, issued.Token)

	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected revoked token rejection, got %v", err)
	}

	err = Revoke(ctx, db, issued.ID)

	if !errors.Is(err, ErrNotRevocable) {
		t.Fatalf("expected revocation rejection, got %v", err)
	}
}

func TestInvalidToken(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	err := consumeToken(ctx, db, "invalid-token")

	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expected invalid token error, got %v", err)
	}
}

func TestConsumptionRollback(t *testing.T) {
	ctx := context.Background()
	db := testDatabase(t)

	issued, err := Issue(ctx, db, DefaultTTL)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	if err := Consume(ctx, tx, issued.Token); err != nil {
		t.Fatal(err)
	}

	// Simulate a failed device registration.
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// The token must still be usable.
	if err := consumeToken(
		ctx, db, issued.Token,
	); err != nil {
		t.Fatalf("token lost after rollback: %v", err)
	}
}
