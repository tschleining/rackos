package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

var tokenEncoding = base64.RawURLEncoding.Strict()

// ValidateTokenEncoding verifies the canonical
// representation of an authentication token.
//
// This checks token format only. It does not
// authenticate a session or query the database.
func ValidateTokenEncoding(token string) error {
	digest, err := hashToken(token)
	clear(digest[:])

	return err
}

// generateToken creates a cryptographically random
// 256-bit authentication token and its SHA-256 digest.
//
// The digest is calculated from the original random
// bytes, not their Base64URL representation.
func generateToken() (
	string,
	[TokenHashSize]byte,
	error,
) {
	var raw [TokenSize]byte
	defer clear(raw[:])

	if _, err := rand.Read(raw[:]); err != nil {
		return "", [TokenHashSize]byte{}, fmt.Errorf(
			"generate session token: %w",
			err,
		)
	}

	digest := sha256.Sum256(raw[:])

	encoded := tokenEncoding.EncodeToString(raw[:])

	return encoded, digest, nil
}

// hashToken validates a canonical authentication token
// and derives its database lookup digest.
//
// Noncanonical encodings, padding, whitespace and
// incorrect token lengths are rejected.
func hashToken(
	token string,
) ([TokenHashSize]byte, error) {
	if len(token) != EncodedTokenSize {
		return [TokenHashSize]byte{}, ErrInvalidToken
	}

	var raw [TokenSize]byte
	defer clear(raw[:])

	n, err := tokenEncoding.Decode(
		raw[:],
		[]byte(token),
	)

	if err != nil || n != TokenSize {
		return [TokenHashSize]byte{}, ErrInvalidToken
	}

	if tokenEncoding.EncodeToString(raw[:]) != token {
		return [TokenHashSize]byte{}, ErrInvalidToken
	}

	return sha256.Sum256(raw[:]), nil
}

// generateSessionID creates a nonsecret identifier
// independently of the authentication token.
func generateSessionID() (string, error) {
	var raw [SessionIDSize]byte

	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf(
			"generate session identity: %w",
			err,
		)
	}

	return hex.EncodeToString(raw[:]), nil
}
