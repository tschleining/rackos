package credentials

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

const tokenPrefix = "rackos_agent_"

var (
	ErrUnauthenticated = errors.New(
		"invalid agent credentials",
	)

	ErrNotRevocable = errors.New(
		"agent credential is not revocable",
	)
)

// Issue creates a device-specific credential inside
// the caller's database transaction.
//
// The caller must commit before returning the token
// to the agent.
func Issue(
	ctx context.Context,
	tx *sql.Tx,
	deviceID string,
) (string, error) {
	var secret [32]byte

	if _, err := rand.Read(secret[:]); err != nil {
		return "", fmt.Errorf(
			"generate agent credential: %w", err,
		)
	}

	token := tokenPrefix +
		base64.RawURLEncoding.EncodeToString(secret[:])

	hash := sha256.Sum256([]byte(token))

	_, err := tx.ExecContext(
		ctx,
		`INSERT INTO agent_credentials (
			device_id, token_hash, created_at
		) VALUES (?, ?, ?)`,
		deviceID,
		hash[:],
		time.Now().UnixMilli(),
	)

	if err != nil {
		return "", fmt.Errorf(
			"store agent credential: %w", err,
		)
	}

	return token, nil
}

// queryer allows authentication to operate against
// either a database connection pool or a transaction.
type queryer interface {
	QueryRowContext(
		context.Context,
		string,
		...any,
	) *sql.Row
}

func Authenticate(
	ctx context.Context,
	db queryer,
	deviceID string,
	token string,
) error {
	hash, err := tokenHash(token)
	if err != nil {
		return err
	}

	var storedHash []byte

	err = db.QueryRowContext(
		ctx,
		`SELECT token_hash
		 FROM agent_credentials
		 WHERE device_id = ?
		   AND revoked_at IS NULL`,
		deviceID,
	).Scan(&storedHash)

	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthenticated
	}

	if err != nil {
		return fmt.Errorf(
			"load agent credential: %w", err,
		)
	}

	if subtle.ConstantTimeCompare(
		storedHash, hash[:],
	) != 1 {
		return ErrUnauthenticated
	}

	return nil
}

func Revoke(
	ctx context.Context,
	db *sql.DB,
	deviceID string,
) error {
	result, err := db.ExecContext(
		ctx,
		`UPDATE agent_credentials
		 SET revoked_at = ?
		 WHERE device_id = ?
		   AND revoked_at IS NULL`,
		time.Now().UnixMilli(),
		deviceID,
	)

	if err != nil {
		return fmt.Errorf(
			"revoke agent credential: %w", err,
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
		return [32]byte{}, ErrUnauthenticated
	}

	encoded := strings.TrimPrefix(
		token, tokenPrefix,
	)

	if len(encoded) != 43 {
		return [32]byte{}, ErrUnauthenticated
	}

	decoded, err := base64.RawURLEncoding.
		Strict().DecodeString(encoded)

	if err != nil || len(decoded) != 32 {
		return [32]byte{}, ErrUnauthenticated
	}

	if base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return [32]byte{}, ErrUnauthenticated
	}

	return sha256.Sum256([]byte(token)), nil
}
