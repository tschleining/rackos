package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/tschleining/rackos/internal/appliance/admin"

	"golang.org/x/crypto/argon2"
)

// These parameters define the stored argon2id-v1
// password scheme. Do not change them without
// introducing a new scheme identifier.
const (
	Argon2MemoryKiB   uint32 = 64 * 1024
	Argon2Iterations  uint32 = 3
	Argon2Parallelism uint8  = 1

	MaxConcurrentPasswordHashes = 1
)

var ErrPasswordHashBusy = errors.New(
	"password hashing capacity exhausted",
)

// Limit expensive password operations across the
// entire appliance process.
//
// Requests are rejected when capacity is exhausted
// rather than accumulating an unbounded queue.
var passwordHashSlots = make(
	chan struct{},
	MaxConcurrentPasswordHashes,
)

// HashPassword validates a new administrator password,
// generates a random salt, and derives its digest.
//
// The caller retains ownership of the supplied
// password and is responsible for clearing it.
func HashPassword(
	ctx context.Context,
	username string,
	password []byte,
) (admin.PasswordDigest, error) {
	if err := ctx.Err(); err != nil {
		return admin.PasswordDigest{}, err
	}

	normalized, err := prepareNewPassword(
		username,
		password,
	)
	if err != nil {
		return admin.PasswordDigest{}, err
	}
	defer clear(normalized)

	digest := admin.PasswordDigest{
		Scheme: admin.PasswordScheme,
	}

	if _, err := rand.Read(digest.Salt[:]); err != nil {
		return admin.PasswordDigest{}, fmt.Errorf(
			"generate password salt: %w",
			err,
		)
	}

	hash, err := derivePasswordHash(
		ctx,
		normalized,
		digest.Salt,
	)
	if err != nil {
		return admin.PasswordDigest{}, err
	}
	defer clear(hash[:])

	digest.Hash = hash

	return digest, nil
}

// VerifyPassword verifies a supplied password against
// stored password verification material.
//
// New-password blocklist rules are intentionally not
// reapplied during authentication.
func VerifyPassword(
	ctx context.Context,
	password []byte,
	digest admin.PasswordDigest,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	if digest.Scheme != admin.PasswordScheme {
		return false, admin.ErrInvalidPasswordDigest
	}

	normalized, err := normalizePassword(password)
	if err != nil {
		return false, err
	}
	defer clear(normalized)

	hash, err := derivePasswordHash(
		ctx,
		normalized,
		digest.Salt,
	)
	if err != nil {
		return false, err
	}
	defer clear(hash[:])

	matches := subtle.ConstantTimeCompare(
		hash[:],
		digest.Hash[:],
	) == 1

	return matches, nil
}

func derivePasswordHash(
	ctx context.Context,
	password []byte,
	salt [admin.PasswordSaltSize]byte,
) ([admin.PasswordHashSize]byte, error) {
	var result [admin.PasswordHashSize]byte

	if err := ctx.Err(); err != nil {
		return result, err
	}

	select {
	case passwordHashSlots <- struct{}{}:
		defer func() {
			<-passwordHashSlots
		}()

	default:
		return result, ErrPasswordHashBusy
	}

	if err := ctx.Err(); err != nil {
		return result, err
	}

	derived := argon2.IDKey(
		password,
		salt[:],
		Argon2Iterations,
		Argon2MemoryKiB,
		Argon2Parallelism,
		uint32(admin.PasswordHashSize),
	)
	defer clear(derived)

	// Argon2 cannot be interrupted once started.
	// Discard the result if the context expired
	// during computation.
	if err := ctx.Err(); err != nil {
		return result, err
	}

	copy(result[:], derived)

	return result, nil
}
