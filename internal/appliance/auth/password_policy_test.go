package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/admin"
)

func TestPasswordPolicy(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{
			"valid passphrase",
			"purple clouds above the forest",
			nil,
		},
		{
			"minimum length",
			"abcdefghijklmno",
			nil,
		},
		{
			"maximum length",
			strings.Repeat("ab", 64),
			nil,
		},
		{
			"too short",
			strings.Repeat("a", 14),
			ErrPasswordTooShort,
		},
		{
			"too long",
			strings.Repeat("a", 129),
			ErrPasswordTooLong,
		},
		{
			"empty password",
			"",
			ErrPasswordTooShort,
		},
		{
			"spaces only",
			strings.Repeat(" ", 20),
			ErrInvalidPassword,
		},
		{
			"leading and trailing spaces",
			" a valid long passphrase ",
			nil,
		},
		{
			"unicode password",
			"café beneath the mountains",
			nil,
		},
		{
			"unicode character length",
			strings.Repeat("界", 15),
			nil,
		},
		{
			"emoji password",
			strings.Repeat("🌲", 15),
			nil,
		},
		{
			"embedded newline",
			"long password\nwith newline",
			ErrInvalidPassword,
		},
		{
			"embedded null",
			"long password\x00with null",
			ErrInvalidPassword,
		},
		{
			"embedded tab",
			"long password\twith tab",
			ErrInvalidPassword,
		},
		{
			"invalid UTF-8",
			"long password with \xff",
			ErrInvalidPassword,
		},
		{
			"common numeric password",
			"123456789012345",
			ErrCommonPassword,
		},
		{
			"common password case variation",
			"PASSWORDPASSWORD",
			ErrCommonPassword,
		},
		{
			"common password with spaces",
			" passwordpassword ",
			ErrCommonPassword,
		},
		{
			"common RackOS password",
			"rackosadministrator",
			ErrCommonPassword,
		},
		{
			"oversized raw input",
			strings.Repeat(
				"a",
				MaxPasswordInputBytes+1,
			),
			ErrPasswordTooLong,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNewPassword(
				"admin",
				[]byte(tc.password),
			)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf(
					"got %v, want %v",
					err,
					tc.wantErr,
				)
			}
		})
	}
}

func TestPasswordRejectsUsername(t *testing.T) {
	err := ValidateNewPassword(
		"administrator-account",
		[]byte("ADMINISTRATOR-ACCOUNT"),
	)

	if !errors.Is(err, ErrCommonPassword) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPasswordRejectsInvalidUsername(t *testing.T) {
	err := ValidateNewPassword(
		"invalid username",
		[]byte("a sufficiently long passphrase"),
	)

	if !errors.Is(err, admin.ErrInvalidUsername) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPasswordUnicodeNormalization(t *testing.T) {
	composed := []byte(
		"café beneath the mountains",
	)

	decomposed := []byte(
		"cafe\u0301 beneath the mountains",
	)

	first, err := normalizePassword(composed)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(first)

	second, err := normalizePassword(decomposed)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(second)

	if string(first) != string(second) {
		t.Fatal("equivalent Unicode passwords differ")
	}
}

func TestPasswordNormalizationPreservesSpaces(
	t *testing.T,
) {
	password := []byte(
		" a sufficiently long password ",
	)

	normalized, err := normalizePassword(password)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(normalized)

	if string(normalized) != string(password) {
		t.Fatal("password whitespace was modified")
	}
}

func TestPasswordLengthAfterNormalization(
	t *testing.T,
) {
	password := []byte(
		strings.Repeat("e\u0301", 14),
	)

	err := ValidateNewPassword(
		"admin",
		password,
	)

	if !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("unexpected error: %v", err)
	}
}
