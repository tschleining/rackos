package admin

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCurrentAdministrator(t *testing.T) {
	ctx := context.Background()
	repo := openTestRepository(t)

	_, err := repo.Current(ctx)

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unexpected error: %v", err)
	}

	created, err := repo.CreateInitial(
		ctx,
		"admin",
		testDigest(),
	)
	if err != nil {
		t.Fatal(err)
	}

	current, err := repo.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if current.ID != created.ID {
		t.Fatal("incorrect current administrator")
	}
}

func TestReplaceAdministratorPassword(
	t *testing.T,
) {
	ctx := context.Background()
	repo := openTestRepository(t)

	initialTime := time.UnixMilli(
		1800000000000,
	).UTC()

	repo.now = func() time.Time {
		return initialTime
	}

	original := testDigest()

	account, err := repo.CreateInitial(
		ctx,
		"admin",
		original,
	)
	if err != nil {
		t.Fatal(err)
	}

	replacement := original
	replacement.Salt[0] ^= 0xff
	replacement.Hash[0] ^= 0xff

	replacementTime := initialTime.Add(
		time.Minute,
	)

	repo.now = func() time.Time {
		return replacementTime
	}

	if err := repo.ReplacePassword(
		ctx,
		account.ID,
		replacement,
	); err != nil {
		t.Fatal(err)
	}

	record, err := repo.GetAuthenticationRecord(
		ctx,
		"admin",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record.Password != replacement {
		t.Fatal("replacement digest was not stored")
	}

	stored := record.Administrator

	if stored.ID != account.ID ||
		stored.Username != account.Username ||
		stored.Role != account.Role {
		t.Fatal("administrator identity changed")
	}

	if !stored.CreatedAt.Equal(
		account.CreatedAt,
	) {
		t.Fatal("creation timestamp changed")
	}

	if !stored.PasswordChangedAt.Equal(
		replacementTime,
	) {
		t.Fatal("incorrect password-change timestamp")
	}

	if !stored.UpdatedAt.Equal(
		replacementTime,
	) {
		t.Fatal("incorrect update timestamp")
	}
}

func TestPasswordReplacementAdvancesRevision(
	t *testing.T,
) {
	ctx := context.Background()
	repo := openTestRepository(t)

	initialTime := time.UnixMilli(
		1800000000000,
	).UTC()

	repo.now = func() time.Time {
		return initialTime
	}

	account, err := repo.CreateInitial(
		ctx,
		"admin",
		testDigest(),
	)
	if err != nil {
		t.Fatal(err)
	}

	// First replacement in the same millisecond.
	digest := testDigest()
	digest.Hash[0] ^= 0xff

	if err := repo.ReplacePassword(
		ctx,
		account.ID,
		digest,
	); err != nil {
		t.Fatal(err)
	}

	first, err := repo.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !first.PasswordChangedAt.After(
		account.PasswordChangedAt,
	) {
		t.Fatal("password revision did not advance")
	}

	// Simulate the system clock moving backwards.
	repo.now = func() time.Time {
		return initialTime.Add(-time.Hour)
	}

	digest.Hash[1] ^= 0xff

	if err := repo.ReplacePassword(
		ctx,
		account.ID,
		digest,
	); err != nil {
		t.Fatal(err)
	}

	second, err := repo.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !second.PasswordChangedAt.After(
		first.PasswordChangedAt,
	) {
		t.Fatal("password revision moved backwards")
	}

	if second.UpdatedAt.Before(
		second.PasswordChangedAt,
	) {
		t.Fatal("inconsistent update timestamp")
	}
}

func TestPasswordReplacementPreservesAccountState(
	t *testing.T,
) {
	ctx := context.Background()
	repo := openTestRepository(t)

	account, err := repo.CreateInitial(
		ctx,
		"admin",
		testDigest(),
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = repo.db.ExecContext(
		ctx,
		`UPDATE administrators
		 SET disabled_at = created_at,
		     last_login_at = created_at
		 WHERE id = ?`,
		account.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	before, err := repo.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}

	digest := testDigest()
	digest.Hash[0] ^= 0xff

	if err := repo.ReplacePassword(
		ctx,
		account.ID,
		digest,
	); err != nil {
		t.Fatal(err)
	}

	after, err := repo.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}

	if after.IsActive() {
		t.Fatal("password replacement reactivated account")
	}

	if after.DisabledAt == nil ||
		!after.DisabledAt.Equal(
			*before.DisabledAt,
		) {
		t.Fatal("disabled timestamp changed")
	}

	if after.LastLoginAt == nil ||
		!after.LastLoginAt.Equal(
			*before.LastLoginAt,
		) {
		t.Fatal("last login timestamp changed")
	}
}

func TestReplacePasswordRejectsInvalidInput(
	t *testing.T,
) {
	ctx := context.Background()
	repo := openTestRepository(t)

	original := testDigest()

	account, err := repo.CreateInitial(
		ctx,
		"admin",
		original,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = repo.ReplacePassword(
		ctx,
		"invalid",
		original,
	)

	if !errors.Is(err, ErrInvalidID) {
		t.Fatalf("unexpected error: %v", err)
	}

	err = repo.ReplacePassword(
		ctx,
		"11111111-1111-4111-8111-111111111111",
		original,
	)

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unexpected error: %v", err)
	}

	invalid := original
	invalid.Scheme = "unsupported"

	err = repo.ReplacePassword(
		ctx,
		account.ID,
		invalid,
	)

	if !errors.Is(
		err,
		ErrInvalidPasswordDigest,
	) {
		t.Fatalf("unexpected error: %v", err)
	}

	record, err := repo.GetAuthenticationRecord(
		ctx,
		"admin",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record.Password != original {
		t.Fatal("invalid update changed password")
	}
}
