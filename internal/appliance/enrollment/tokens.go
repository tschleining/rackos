package enrollment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultTTL = 15 * time.Minute
	MaxTTL     = 24 * time.Hour

	tokenPrefix = "rackos_enroll_"
)

var (
	ErrInvalidTTL = errors.New(
		"enrollment token lifetime must be between 1 second and 24 hours",
	)

	ErrInvalidToken = errors.New(
		"invalid or unavailable enrollment token",
	)

	ErrNotRevocable = errors.New(
		"enrollment token is not revocable",
	)
)

type IssuedToken struct {
	ID        string
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

func Issue(
	ctx context.Context,
	db *sql.DB,
	ttl time.Duration,
) (IssuedToken, error) {
	if ttl < time.Second || ttl > MaxTTL {
		return IssuedToken{}, ErrInvalidTTL
	}

	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return IssuedToken{}, fmt.Errorf(
			"generate enrollment token: %w", err,
		)
	}

	var identifier [16]byte
	if _, err := rand.Read(identifier[:]); err != nil {
		return IssuedToken{}, fmt.Errorf(
			"generate enrollment token ID: %w", err,
		)
	}

	token := tokenPrefix +
		base64.RawURLEncoding.EncodeToString(secret[:])

	id := hex.EncodeToString(identifier[:])
	hash := sha256.Sum256([]byte(token))

	now := time.Now().UTC().Truncate(time.Millisecond)
	expires := now.Add(ttl).Truncate(time.Millisecond)

	_, err := db.ExecContext(
		ctx,
		`INSERT INTO enrollment_tokens (
			id, token_hash, created_at, expires_at
		) VALUES (?, ?, ?, ?)`,
		id,
		hash[:],
		now.UnixMilli(),
		expires.UnixMilli(),
	)

	if err != nil {
		return IssuedToken{}, fmt.Errorf(
			"store enrollment token: %w", err,
		)
	}

	return IssuedToken{
		ID:        id,
		Token:     token,
		CreatedAt: now,
		ExpiresAt: expires,
	}, nil
}

// Consume marks a token as used inside the caller's
// transaction. The caller must commit only after
// device registration and credential creation succeed.
func Consume(
	ctx context.Context,
	tx *sql.Tx,
	token string,
) error {
	hash, err := tokenHash(token)
	if err != nil {
		return err
	}

	now := time.Now().UnixMilli()

	result, err := tx.ExecContext(
		ctx,
		`UPDATE enrollment_tokens
		 SET used_at = ?
		 WHERE token_hash = ?
		   AND expires_at > ?
		   AND used_at IS NULL
		   AND revoked_at IS NULL`,
		now,
		hash[:],
		now,
	)

	if err != nil {
		return fmt.Errorf(
			"consume enrollment token: %w", err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if affected != 1 {
		return ErrInvalidToken
	}

	return nil
}

func Revoke(
	ctx context.Context,
	db *sql.DB,
	id string,
) error {
	now := time.Now().UnixMilli()

	result, err := db.ExecContext(
		ctx,
		`UPDATE enrollment_tokens
		 SET revoked_at = ?
		 WHERE id = ?
		   AND expires_at > ?
		   AND used_at IS NULL
		   AND revoked_at IS NULL`,
		now,
		id,
		now,
	)

	if err != nil {
		return fmt.Errorf(
			"revoke enrollment token: %w", err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if affected != 1 {
		return ErrNotRevocable
	}

	return nil
}

func tokenHash(token string) ([32]byte, error) {
	if !strings.HasPrefix(token, tokenPrefix) {
		return [32]byte{}, ErrInvalidToken
	}

	encoded := strings.TrimPrefix(token, tokenPrefix)

	if len(encoded) != 43 {
		return [32]byte{}, ErrInvalidToken
	}

	decoded, err := base64.RawURLEncoding.
		Strict().DecodeString(encoded)

	if err != nil || len(decoded) != 32 {
		return [32]byte{}, ErrInvalidToken
	}

	if base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return [32]byte{}, ErrInvalidToken
	}

	return sha256.Sum256([]byte(token)), nil
}
