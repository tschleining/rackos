package admin

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
)

var (
	ErrNotFound = errors.New(
		"administrator not found",
	)

	ErrAlreadyInitialized = errors.New(
		"administrator already initialized",
	)

	ErrInvalidUsername = errors.New(
		"invalid administrator username",
	)

	ErrInvalidID = errors.New(
		"invalid administrator ID",
	)

	ErrInvalidPasswordDigest = errors.New(
		"invalid password digest",
	)
)

var usernamePattern = regexp.MustCompile(
	`^[A-Za-z0-9][A-Za-z0-9_.-]{2,63}$`,
)

var idPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

// NormalizeUsername validates an ASCII username and
// returns its canonical lowercase representation.
func NormalizeUsername(
	username string,
) (string, error) {
	if !usernamePattern.MatchString(username) {
		return "", ErrInvalidUsername
	}

	return strings.ToLower(username), nil
}

func IsValidID(id string) bool {
	return idPattern.MatchString(id)
}

// newAdministratorID generates a cryptographically
// random RFC-compatible UUID version 4.
func newAdministratorID() (string, error) {
	var randomBytes [16]byte

	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", err
	}

	// Set UUID version 4.
	randomBytes[6] =
		(randomBytes[6] & 0x0f) | 0x40

	// Set the RFC UUID variant.
	randomBytes[8] =
		(randomBytes[8] & 0x3f) | 0x80

	value := hex.EncodeToString(
		randomBytes[:],
	)

	return value[:8] + "-" +
		value[8:12] + "-" +
		value[12:16] + "-" +
		value[16:20] + "-" +
		value[20:], nil
}

func validatePasswordDigest(
	digest PasswordDigest,
) error {
	if digest.Scheme != PasswordScheme {
		return ErrInvalidPasswordDigest
	}

	return nil
}
