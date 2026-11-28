package httpserver

import (
	"context"
	"errors"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/auth"
)

var (
	errInvalidAdministratorCredentials = errors.New(
		"invalid administrator credentials",
	)

	errInvalidAuthenticationRecord = errors.New(
		"invalid administrator authentication record",
	)
)

// passwordVerifier represents the existing password
// verification operation.
//
// The function dependency allows deterministic tests
// without performing Argon2id for every test case.
type passwordVerifier func(
	context.Context,
	[]byte,
	admin.PasswordDigest,
) (bool, error)

// dummyAuthenticationDigest supplies valid Argon2id
// verification parameters for nonexistent or
// unavailable administrator accounts.
//
// Its verification result must never be accepted
// as successful authentication.
func dummyAuthenticationDigest() admin.PasswordDigest {
	digest := admin.PasswordDigest{
		Scheme: admin.PasswordScheme,
	}

	copy(
		digest.Salt[:],
		[]byte("rackos-auth-v1"),
	)

	return digest
}

// verifyAdministratorCredentials verifies a password
// and returns the original administrator snapshot
// associated with the verified credential revision.
//
// The caller retains ownership of the password
// buffer and must clear it when no longer required.
func verifyAdministratorCredentials(
	ctx context.Context,
	reader AdministratorAuthenticationReader,
	username string,
	password []byte,
) (admin.Administrator, error) {
	return verifyAdministratorCredentialsWith(
		ctx,
		reader,
		auth.VerifyPassword,
		username,
		password,
	)
}

func verifyAdministratorCredentialsWith(
	ctx context.Context,
	reader AdministratorAuthenticationReader,
	verify passwordVerifier,
	username string,
	password []byte,
) (admin.Administrator, error) {
	if err := ctx.Err(); err != nil {
		return admin.Administrator{}, err
	}

	if reader == nil || verify == nil {
		return admin.Administrator{},
			errInvalidAuthenticationRecord
	}

	canonicalUsername, err :=
		admin.NormalizeUsername(username)

	if err != nil {
		return admin.Administrator{},
			errInvalidAdministratorCredentials
	}

	record, err := reader.GetAuthenticationRecord(
		ctx,
		canonicalUsername,
	)

	defer clear(record.Password.Salt[:])
	defer clear(record.Password.Hash[:])

	if err := ctx.Err(); err != nil {
		return admin.Administrator{}, err
	}

	var eligible bool

	digest := dummyAuthenticationDigest()

	defer clear(digest.Salt[:])
	defer clear(digest.Hash[:])

	switch {
	case errors.Is(err, admin.ErrNotFound):
		// Unknown accounts still perform password
		// verification using the dummy digest.

	case err != nil:
		return admin.Administrator{}, err

	default:
		account := record.Administrator

		// Reject malformed or inconsistent
		// successful repository responses.
		if !admin.IsValidID(account.ID) ||
			account.Username != canonicalUsername ||
			account.PasswordChangedAt.UnixMilli() <= 0 ||
			record.Password.Scheme != admin.PasswordScheme {
			return admin.Administrator{},
				errInvalidAuthenticationRecord
		}

		if account.IsActive() &&
			account.Role == admin.RoleAdministrator {
			eligible = true
			digest = record.Password
		}
	}

	// Perform the same password verification
	// operation for eligible and ineligible accounts.
	matches, err := verify(
		ctx,
		password,
		digest,
	)

	if contextErr := ctx.Err(); contextErr != nil {
		return admin.Administrator{}, contextErr
	}

	switch {
	case errors.Is(err, auth.ErrPasswordTooShort),
		errors.Is(err, auth.ErrPasswordTooLong),
		errors.Is(err, auth.ErrInvalidPassword):

		return admin.Administrator{},
			errInvalidAdministratorCredentials

	case err != nil:
		return admin.Administrator{}, err
	}

	// A dummy digest must never authenticate an
	// unknown, disabled or otherwise ineligible account.
	if !eligible || !matches {
		return admin.Administrator{},
			errInvalidAdministratorCredentials
	}

	// Preserve the original verified credential
	// revision. Never refetch the administrator
	// between verification and session issuance.
	return record.Administrator, nil
}
