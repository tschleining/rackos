package auth

import (
	"bytes"
	"errors"
	"unicode"
	"unicode/utf8"

	"github.com/tschleining/rackos/internal/appliance/admin"

	"golang.org/x/text/unicode/norm"
)

const (
	MinPasswordLength = 15
	MaxPasswordLength = 128

	// Limit input before Unicode normalization.
	MaxPasswordInputBytes = 4096
)

var (
	ErrPasswordTooShort = errors.New(
		"password must contain at least 15 characters",
	)

	ErrPasswordTooLong = errors.New(
		"password exceeds the maximum supported length",
	)

	ErrInvalidPassword = errors.New(
		"password contains invalid characters",
	)

	ErrCommonPassword = errors.New(
		"password is too common or easily guessed",
	)
)

// Initial local blocklist.
//
// Expand this list before browser authentication
// becomes available. This is not a comprehensive
// compromised-password database.
var blockedPasswords = [...]string{
	"123456789012345",
	"1234567890123456",
	"12345678901234567890",
	"111111111111111",
	"000000000000000",
	"abcdefghijklmnop",
	"abcdefghijklmnopqrstuvwxyz",
	"qwertyuiopasdfghjkl",
	"passwordpassword",
	"password123456789",
	"administrator123",
	"administrator1234",
	"adminadminadmin",
	"adminadminadminadmin",
	"changemechangeme",
	"letmeinletmeinletmein",
	"correct horse battery staple",
	"rackosadministrator",
	"rackosadmin12345",
	"rackospassword123",
}

// ValidateNewPassword validates a prospective
// administrator password without hashing it.
func ValidateNewPassword(
	username string,
	password []byte,
) error {
	normalized, err := prepareNewPassword(
		username,
		password,
	)
	if err != nil {
		return err
	}

	clear(normalized)

	return nil
}

func prepareNewPassword(
	username string,
	password []byte,
) ([]byte, error) {
	canonicalUsername, err :=
		admin.NormalizeUsername(username)

	if err != nil {
		return nil, err
	}

	normalized, err := normalizePassword(password)
	if err != nil {
		return nil, err
	}

	if isBlockedPassword(
		canonicalUsername,
		normalized,
	) {
		clear(normalized)

		return nil, ErrCommonPassword
	}

	return normalized, nil
}

// normalizePassword applies the stable input policy
// associated with argon2id-v1.
//
// It returns a separate byte slice and does not
// modify the caller's password.
func normalizePassword(
	password []byte,
) ([]byte, error) {
	if len(password) > MaxPasswordInputBytes {
		return nil, ErrPasswordTooLong
	}

	if !utf8.Valid(password) {
		return nil, ErrInvalidPassword
	}

	normalized := norm.NFC.Append(
		nil,
		password...,
	)

	length := utf8.RuneCount(normalized)

	if length < MinPasswordLength {
		clear(normalized)

		return nil, ErrPasswordTooShort
	}

	if length > MaxPasswordLength {
		clear(normalized)

		return nil, ErrPasswordTooLong
	}

	for _, character := range string(normalized) {
		if unicode.IsControl(character) {
			clear(normalized)

			return nil, ErrInvalidPassword
		}
	}

	if len(bytes.TrimSpace(normalized)) == 0 {
		clear(normalized)

		return nil, ErrInvalidPassword
	}

	return normalized, nil
}

func isBlockedPassword(
	username string,
	password []byte,
) bool {
	// Trim spaces only for blocklist comparison.
	// The actual password retains its spaces.
	candidate := bytes.TrimSpace(password)

	if bytes.EqualFold(
		candidate,
		[]byte(username),
	) {
		return true
	}

	for _, blocked := range blockedPasswords {
		if bytes.EqualFold(
			candidate,
			[]byte(blocked),
		) {
			return true
		}
	}

	return false
}
