package auth

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/admin"

	"golang.org/x/crypto/argon2"
)

const testPassword = "purple clouds above the forest"

func hashTestPassword(
	t *testing.T,
) admin.PasswordDigest {
	t.Helper()

	password := []byte(testPassword)
	defer clear(password)

	digest, err := HashPassword(
		context.Background(),
		"admin",
		password,
	)
	if err != nil {
		t.Fatal(err)
	}

	return digest
}

func TestArgon2idSchemeParameters(t *testing.T) {
	if admin.PasswordScheme != "argon2id-v1" {
		t.Fatal("unexpected password scheme")
	}

	if argon2.Version != 0x13 {
		t.Fatal("unexpected Argon2 version")
	}

	if Argon2MemoryKiB != 65536 ||
		Argon2Iterations != 3 ||
		Argon2Parallelism != 1 {
		t.Fatal("argon2id-v1 parameters changed")
	}

	if admin.PasswordSaltSize != 16 ||
		admin.PasswordHashSize != 32 {
		t.Fatal("argon2id-v1 digest sizes changed")
	}
}

func TestArgon2idKnownAnswer(t *testing.T) {
	var salt [admin.PasswordSaltSize]byte

	for i := range salt {
		salt[i] = byte(i)
	}

	password := []byte("rackos test passphrase")
	defer clear(password)

	actual, err := derivePasswordHash(
		context.Background(),
		password,
		salt,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(actual[:])

	expected, err := hex.DecodeString(
		"4fc1ee6f71275e77c70a740ff65704193a4faba69ae2472347b1a977625bdb90",
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(actual[:], expected) {
		t.Fatal("Argon2id known-answer test failed")
	}
}

func TestHashAndVerifyPassword(t *testing.T) {
	digest := hashTestPassword(t)

	if digest.Scheme != admin.PasswordScheme {
		t.Fatal("incorrect password scheme")
	}

	password := []byte(testPassword)
	defer clear(password)

	valid, err := VerifyPassword(
		context.Background(),
		password,
		digest,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !valid {
		t.Fatal("correct password was rejected")
	}
}

func TestRejectIncorrectPassword(t *testing.T) {
	digest := hashTestPassword(t)

	valid, err := VerifyPassword(
		context.Background(),
		[]byte("an entirely different password"),
		digest,
	)
	if err != nil {
		t.Fatal(err)
	}

	if valid {
		t.Fatal("incorrect password was accepted")
	}
}

func TestPasswordsUseRandomSalts(t *testing.T) {
	first := hashTestPassword(t)
	second := hashTestPassword(t)

	if first.Salt == second.Salt {
		t.Fatal("password salts were reused")
	}

	if first.Hash == second.Hash {
		t.Fatal("salted password hashes are identical")
	}
}

func TestPasswordVerificationIsCaseSensitive(
	t *testing.T,
) {
	digest := hashTestPassword(t)

	valid, err := VerifyPassword(
		context.Background(),
		[]byte("Purple clouds above the forest"),
		digest,
	)
	if err != nil {
		t.Fatal(err)
	}

	if valid {
		t.Fatal("password comparison ignored case")
	}
}

func TestPasswordVerificationPreservesSpaces(
	t *testing.T,
) {
	ctx := context.Background()

	digest, err := HashPassword(
		ctx,
		"admin",
		[]byte(" a sufficiently long password "),
	)
	if err != nil {
		t.Fatal(err)
	}

	valid, err := VerifyPassword(
		ctx,
		[]byte("a sufficiently long password"),
		digest,
	)
	if err != nil {
		t.Fatal(err)
	}

	if valid {
		t.Fatal("password whitespace was ignored")
	}
}

func TestVerifyCanonicallyEquivalentPassword(
	t *testing.T,
) {
	ctx := context.Background()

	digest, err := HashPassword(
		ctx,
		"admin",
		[]byte("café beneath the mountains"),
	)
	if err != nil {
		t.Fatal(err)
	}

	valid, err := VerifyPassword(
		ctx,
		[]byte("cafe\u0301 beneath the mountains"),
		digest,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !valid {
		t.Fatal("equivalent Unicode password rejected")
	}
}

func TestHashPasswordRejectsWeakPassword(
	t *testing.T,
) {
	_, err := HashPassword(
		context.Background(),
		"admin",
		[]byte("passwordpassword"),
	)

	if !errors.Is(err, ErrCommonPassword) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyRejectsUnsupportedScheme(
	t *testing.T,
) {
	digest := admin.PasswordDigest{
		Scheme: "unsupported",
	}

	valid, err := VerifyPassword(
		context.Background(),
		[]byte(testPassword),
		digest,
	)

	if !errors.Is(
		err,
		admin.ErrInvalidPasswordDigest,
	) {
		t.Fatalf("unexpected error: %v", err)
	}

	if valid {
		t.Fatal("unsupported password scheme accepted")
	}
}

func TestPasswordVerificationRejectsTamperedHash(
	t *testing.T,
) {
	digest := hashTestPassword(t)

	digest.Hash[0] ^= 0xff

	valid, err := VerifyPassword(
		context.Background(),
		[]byte(testPassword),
		digest,
	)
	if err != nil {
		t.Fatal(err)
	}

	if valid {
		t.Fatal("tampered password hash accepted")
	}
}

func TestPasswordVerificationRejectsTamperedSalt(
	t *testing.T,
) {
	digest := hashTestPassword(t)

	digest.Salt[0] ^= 0xff

	valid, err := VerifyPassword(
		context.Background(),
		[]byte(testPassword),
		digest,
	)
	if err != nil {
		t.Fatal(err)
	}

	if valid {
		t.Fatal("tampered password salt accepted")
	}
}

func TestPasswordHashDoesNotModifyInput(
	t *testing.T,
) {
	password := []byte(testPassword)
	expected := bytes.Clone(password)

	defer clear(password)
	defer clear(expected)

	_, err := HashPassword(
		context.Background(),
		"admin",
		password,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(password, expected) {
		t.Fatal("caller password was modified")
	}
}

func TestPasswordHashCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	cancel()

	_, err := HashPassword(
		ctx,
		"admin",
		[]byte(testPassword),
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPasswordHashConcurrencyLimit(
	t *testing.T,
) {
	// Occupy the hashing capacity without performing
	// an expensive derivation.
	for i := 0; i < MaxConcurrentPasswordHashes; i++ {
		passwordHashSlots <- struct{}{}
	}

	defer func() {
		for i := 0; i < MaxConcurrentPasswordHashes; i++ {
			<-passwordHashSlots
		}
	}()

	_, err := HashPassword(
		context.Background(),
		"admin",
		[]byte(testPassword),
	)

	if !errors.Is(err, ErrPasswordHashBusy) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func BenchmarkHashPassword(b *testing.B) {
	ctx := context.Background()

	password := []byte(testPassword)
	defer clear(password)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := HashPassword(
			ctx,
			"admin",
			password,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyPassword(b *testing.B) {
	ctx := context.Background()

	password := []byte(testPassword)
	defer clear(password)

	digest, err := HashPassword(
		ctx,
		"admin",
		password,
	)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		valid, err := VerifyPassword(
			ctx,
			password,
			digest,
		)
		if err != nil {
			b.Fatal(err)
		}

		if !valid {
			b.Fatal("password verification failed")
		}
	}
}
