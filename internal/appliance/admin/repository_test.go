package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/database"
)

func openTestRepository(t *testing.T) *Repository {
	t.Helper()

	db, err := database.Open(
		context.Background(),
		t.TempDir(),
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return NewRepository(db)
}

// testDigest supplies deterministic bytes for repository
// testing only. It is not a real password hash.
func testDigest() PasswordDigest {
	digest := PasswordDigest{
		Scheme: PasswordScheme,
	}

	for i := range digest.Salt {
		digest.Salt[i] = byte(i + 1)
	}

	for i := range digest.Hash {
		digest.Hash[i] = byte(i + 32)
	}

	return digest
}

func TestNormalizeUsername(t *testing.T) {
	tests := []struct {
		input string
		want  string
		valid bool
	}{
		{"admin", "admin", true},
		{"Admin", "admin", true},
		{"RackOS-Admin", "rackos-admin", true},
		{"admin.local", "admin.local", true},
		{"admin_01", "admin_01", true},
		{"", "", false},
		{"ab", "", false},
		{" admin", "", false},
		{"admin ", "", false},
		{"admin@example.com", "", false},
		{"_admin", "", false},
		{"admin\n", "", false},
		{"admin\x00", "", false},
		{"administratör", "", false},
		{strings.Repeat("a", 64), strings.Repeat("a", 64), true},
		{strings.Repeat("a", 65), "", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			actual, err := NormalizeUsername(
				tc.input,
			)

			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}

				if actual != tc.want {
					t.Fatalf(
						"got %q, want %q",
						actual,
						tc.want,
					)
				}
				return
			}

			if !errors.Is(
				err,
				ErrInvalidUsername,
			) {
				t.Fatalf(
					"expected invalid username error, got %v",
					err,
				)
			}
		})
	}
}

func TestAdministratorIdentity(t *testing.T) {
	seen := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id, err := newAdministratorID()
		if err != nil {
			t.Fatal(err)
		}

		if !IsValidID(id) {
			t.Fatal("generated invalid administrator ID")
		}

		if seen[id] {
			t.Fatal("duplicate administrator ID")
		}

		seen[id] = true
	}
}

func TestAdministratorInitialization(t *testing.T) {
	ctx := context.Background()
	repo := openTestRepository(t)

	exists, err := repo.Exists(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if exists {
		t.Fatal("fresh appliance already has an administrator")
	}

	expectedTime := time.UnixMilli(
		1800000000000,
	).UTC()

	repo.now = func() time.Time {
		return expectedTime
	}

	account, err := repo.CreateInitial(
		ctx,
		"Admin",
		testDigest(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !IsValidID(account.ID) {
		t.Fatal("invalid administrator identity")
	}

	if account.Username != "admin" {
		t.Fatal("username was not normalized")
	}

	if account.Role != RoleAdministrator {
		t.Fatal("unexpected administrator role")
	}

	if !account.IsActive() {
		t.Fatal("new administrator should be active")
	}

	if account.LastLoginAt != nil ||
		account.DisabledAt != nil {
		t.Fatal("unexpected optional timestamps")
	}

	if !account.CreatedAt.Equal(expectedTime) ||
		!account.UpdatedAt.Equal(expectedTime) ||
		!account.PasswordChangedAt.Equal(expectedTime) {
		t.Fatal("incorrect account timestamps")
	}

	exists, err = repo.Exists(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !exists {
		t.Fatal("administrator initialization not recorded")
	}

	stored, err := repo.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(stored, account) {
		t.Fatal("stored administrator differs from created account")
	}

	byUsername, err := repo.GetByUsername(
		ctx,
		"ADMIN",
	)
	if err != nil {
		t.Fatal(err)
	}

	if byUsername.ID != account.ID {
		t.Fatal("username lookup returned wrong account")
	}
}

func TestRejectSecondAdministrator(t *testing.T) {
	ctx := context.Background()
	repo := openTestRepository(t)

	first, err := repo.CreateInitial(
		ctx,
		"admin",
		testDigest(),
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, username := range []string{
		"ADMIN",
		"another-admin",
	} {
		_, err := repo.CreateInitial(
			ctx,
			username,
			testDigest(),
		)

		if !errors.Is(
			err,
			ErrAlreadyInitialized,
		) {
			t.Fatalf(
				"expected already initialized error, got %v",
				err,
			)
		}
	}

	stored, err := repo.Get(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}

	if stored.Username != first.Username {
		t.Fatal("existing administrator was modified")
	}
}

func TestConcurrentAdministratorInitialization(t *testing.T) {
	ctx := context.Background()
	repo := openTestRepository(t)

	start := make(chan struct{})
	results := make(chan error, 2)

	for _, username := range []string{
		"admin-one",
		"admin-two",
	} {
		go func(username string) {
			<-start

			_, err := repo.CreateInitial(
				ctx,
				username,
				testDigest(),
			)

			results <- err
		}(username)
	}

	close(start)

	var successes int
	var conflicts int

	for i := 0; i < 2; i++ {
		err := <-results

		switch {
		case err == nil:
			successes++

		case errors.Is(
			err,
			ErrAlreadyInitialized,
		):
			conflicts++

		default:
			t.Fatalf(
				"unexpected initialization error: %v",
				err,
			)
		}
	}

	if successes != 1 || conflicts != 1 {
		t.Fatalf(
			"successes=%d, conflicts=%d",
			successes,
			conflicts,
		)
	}
}

func TestRejectInvalidAdministratorInput(t *testing.T) {
	ctx := context.Background()
	repo := openTestRepository(t)

	_, err := repo.CreateInitial(
		ctx,
		"invalid username",
		testDigest(),
	)

	if !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("unexpected error: %v", err)
	}

	digest := testDigest()
	digest.Scheme = "unsupported"

	_, err = repo.CreateInitial(
		ctx,
		"admin",
		digest,
	)

	if !errors.Is(err, ErrInvalidPasswordDigest) {
		t.Fatalf("unexpected error: %v", err)
	}

	exists, err := repo.Exists(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if exists {
		t.Fatal("invalid input created an administrator")
	}
}

func TestAdministratorNotFound(t *testing.T) {
	ctx := context.Background()
	repo := openTestRepository(t)

	_, err := repo.GetByUsername(ctx, "admin")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unexpected username lookup error: %v", err)
	}

	_, err = repo.Get(
		ctx,
		"11111111-1111-4111-8111-111111111111",
	)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unexpected identity lookup error: %v", err)
	}

	_, err = repo.Get(ctx, "invalid")
	if !errors.Is(err, ErrInvalidID) {
		t.Fatalf("unexpected invalid ID error: %v", err)
	}

	_, err = repo.GetAuthenticationRecord(
		ctx,
		"admin",
	)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("unexpected authentication lookup error: %v", err)
	}
}

func TestAuthenticationRecord(t *testing.T) {
	ctx := context.Background()
	repo := openTestRepository(t)

	digest := testDigest()

	account, err := repo.CreateInitial(
		ctx,
		"admin",
		digest,
	)
	if err != nil {
		t.Fatal(err)
	}

	record, err := repo.GetAuthenticationRecord(
		ctx,
		"ADMIN",
	)
	if err != nil {
		t.Fatal(err)
	}

	if record.Administrator.ID != account.ID {
		t.Fatal("incorrect administrator identity")
	}

	if record.Password != digest {
		t.Fatal("password digest changed during persistence")
	}
}

func TestPasswordDigestRedaction(t *testing.T) {
	record := AuthenticationRecord{
		Administrator: Administrator{
			Username: "admin",
			Role:     RoleAdministrator,
		},
		Password: testDigest(),
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage

	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}

	if len(fields) != 1 ||
		fields["administrator"] == nil {
		t.Fatal("unexpected authentication record JSON fields")
	}

	digestJSON, err := json.Marshal(record.Password)
	if err != nil {
		t.Fatal(err)
	}

	if string(digestJSON) != "{}" {
		t.Fatal("password digest was exposed in JSON")
	}

	expected := "[redacted password digest]"

	for _, format := range []string{
		"%v",
		"%+v",
		"%#v",
		"%s",
	} {
		if fmt.Sprintf(
			format,
			record.Password,
		) != expected {
			t.Fatal("password digest was not redacted")
		}
	}
}

func TestAdministratorTransactionRollback(t *testing.T) {
	ctx := context.Background()

	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repo := NewRepository(db)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	txRepo := repo.WithTx(tx)

	_, err = txRepo.CreateInitial(
		ctx,
		"admin",
		testDigest(),
	)
	if err != nil {
		t.Fatal(err)
	}

	exists, err := txRepo.Exists(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !exists {
		t.Fatal("transaction cannot see its own administrator")
	}

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	exists, err = repo.Exists(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if exists {
		t.Fatal("rolled-back administrator remains persisted")
	}
}

func TestAdministratorPersistence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repo := NewRepository(db)
	digest := testDigest()

	account, err := repo.CreateInitial(
		ctx,
		"admin",
		digest,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	repo = NewRepository(reopened)

	record, err := repo.GetAuthenticationRecord(
		ctx,
		"admin",
	)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(
		record.Administrator,
		account,
	) {
		t.Fatal("administrator metadata changed after reopening")
	}

	if record.Password != digest {
		t.Fatal("password digest changed after reopening")
	}
}

func TestDisabledAdministratorRemainsInitialized(t *testing.T) {
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

	// Direct database update for testing the account
	// representation. No disabling API exists yet.
	_, err = repo.db.ExecContext(
		ctx,
		`UPDATE administrators
		 SET disabled_at = created_at
		 WHERE id = ?`,
		account.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	stored, err := repo.Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}

	if stored.IsActive() {
		t.Fatal("disabled administrator is active")
	}

	if stored.DisabledAt == nil {
		t.Fatal("missing disabled timestamp")
	}

	exists, err := repo.Exists(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if !exists {
		t.Fatal("disabled account must not reopen initial setup")
	}

	_, err = repo.CreateInitial(
		ctx,
		"another-admin",
		testDigest(),
	)

	if !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("unexpected initialization error: %v", err)
	}
}
