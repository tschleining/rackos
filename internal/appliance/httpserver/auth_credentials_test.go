package httpserver

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/auth"
	"github.com/tschleining/rackos/internal/appliance/database"
)

type authCredentialTestReader struct {
	record   admin.AuthenticationRecord
	err      error
	calls    int
	username string
}

func (r *authCredentialTestReader) GetAuthenticationRecord(
	_ context.Context,
	username string,
) (admin.AuthenticationRecord, error) {
	r.calls++
	r.username = username

	return r.record, r.err
}

func testAuthCredentialRecord() admin.AuthenticationRecord {
	now := time.Date(
		2026, time.November, 27,
		12, 0, 0, 0, time.UTC,
	)

	record := admin.AuthenticationRecord{
		Administrator: admin.Administrator{
			ID:                "12345678-1234-4000-8000-123456789abc",
			Username:          "admin",
			Role:              admin.RoleAdministrator,
			CreatedAt:         now,
			UpdatedAt:         now,
			PasswordChangedAt: now,
		},

		Password: admin.PasswordDigest{
			Scheme: admin.PasswordScheme,
		},
	}

	record.Password.Salt[0] = 42
	record.Password.Hash[0] = 84

	return record
}

func TestVerifyAdministratorCredentialsSuccess(
	t *testing.T,
) {
	record := testAuthCredentialRecord()

	reader := &authCredentialTestReader{
		record: record,
	}

	password := []byte("synthetic-test-password")
	original := bytes.Clone(password)

	defer clear(password)
	defer clear(original)

	calls := 0

	verify := func(
		_ context.Context,
		input []byte,
		digest admin.PasswordDigest,
	) (bool, error) {
		calls++

		if !bytes.Equal(input, original) {
			t.Fatal("password was modified")
		}

		if digest != record.Password {
			t.Fatal("incorrect password digest")
		}

		return true, nil
	}

	account, err := verifyAdministratorCredentialsWith(
		context.Background(),
		reader,
		verify,
		"ADMIN",
		password,
	)
	if err != nil {
		t.Fatal(err)
	}

	if reader.calls != 1 || calls != 1 {
		t.Fatal("incorrect authentication operation count")
	}

	if reader.username != "admin" {
		t.Fatal("username was not normalized")
	}

	if account != record.Administrator {
		t.Fatal("original administrator snapshot was not preserved")
	}

	if !bytes.Equal(password, original) {
		t.Fatal("caller-owned password was modified")
	}
}

func TestVerifyAdministratorCredentialsRejected(
	t *testing.T,
) {
	disabled := testAuthCredentialRecord()

	disabledAt := disabled.Administrator.CreatedAt.
		Add(time.Hour)

	disabled.Administrator.DisabledAt = &disabledAt

	wrongRole := testAuthCredentialRecord()
	wrongRole.Administrator.Role = admin.Role("viewer")

	tests := []struct {
		name      string
		record    admin.AuthenticationRecord
		readerErr error
		matches   bool
		dummy     bool
	}{
		{
			name:   "incorrect password",
			record: testAuthCredentialRecord(),
		},
		{
			name:      "unknown administrator",
			readerErr: admin.ErrNotFound,
			dummy:     true,
		},
		{
			name:      "dummy digest unexpectedly matches",
			readerErr: admin.ErrNotFound,
			matches:   true,
			dummy:     true,
		},
		{
			name:    "disabled administrator",
			record:  disabled,
			matches: true,
			dummy:   true,
		},
		{
			name:    "incorrect administrator role",
			record:  wrongRole,
			matches: true,
			dummy:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &authCredentialTestReader{
				record: tt.record,
				err:    tt.readerErr,
			}

			calls := 0

			verify := func(
				_ context.Context,
				_ []byte,
				digest admin.PasswordDigest,
			) (bool, error) {
				calls++

				expected := tt.record.Password

				if tt.dummy {
					expected = dummyAuthenticationDigest()
				}

				if digest != expected {
					t.Fatal("incorrect verification digest")
				}

				return tt.matches, nil
			}

			account, err :=
				verifyAdministratorCredentialsWith(
					context.Background(),
					reader,
					verify,
					"admin",
					[]byte("synthetic-test-password"),
				)

			if !errors.Is(
				err,
				errInvalidAdministratorCredentials,
			) {
				t.Fatalf("unexpected error: %v", err)
			}

			if account != (admin.Administrator{}) {
				t.Fatal("failed authentication returned account")
			}

			if reader.calls != 1 || calls != 1 {
				t.Fatal("incorrect verification operation count")
			}
		})
	}
}

func TestVerifyAdministratorCredentialsPasswordErrors(
	t *testing.T,
) {
	tests := []struct {
		name     string
		input    error
		expected error
	}{
		{
			"password too short",
			auth.ErrPasswordTooShort,
			errInvalidAdministratorCredentials,
		},
		{
			"password too long",
			auth.ErrPasswordTooLong,
			errInvalidAdministratorCredentials,
		},
		{
			"invalid password",
			auth.ErrInvalidPassword,
			errInvalidAdministratorCredentials,
		},
		{
			"hashing capacity exhausted",
			auth.ErrPasswordHashBusy,
			auth.ErrPasswordHashBusy,
		},
		{
			"invalid stored digest",
			admin.ErrInvalidPasswordDigest,
			admin.ErrInvalidPasswordDigest,
		},
		{
			"operation timed out",
			context.DeadlineExceeded,
			context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, unknown := range []bool{false, true} {
				reader := &authCredentialTestReader{
					record: testAuthCredentialRecord(),
				}

				if unknown {
					reader.record = admin.AuthenticationRecord{}
					reader.err = admin.ErrNotFound
				}

				verify := func(
					context.Context,
					[]byte,
					admin.PasswordDigest,
				) (bool, error) {
					return false, tt.input
				}

				account, err :=
					verifyAdministratorCredentialsWith(
						context.Background(),
						reader,
						verify,
						"admin",
						[]byte("synthetic-test-password"),
					)

				if !errors.Is(err, tt.expected) {
					t.Fatalf("unexpected error: %v", err)
				}

				if account != (admin.Administrator{}) {
					t.Fatal("failed verification returned account")
				}
			}
		})
	}
}

func TestVerifyAdministratorCredentialsRepositoryFailure(
	t *testing.T,
) {
	repositoryErr := errors.New(
		"synthetic database failure",
	)

	reader := &authCredentialTestReader{
		err: repositoryErr,
	}

	verify := func(
		context.Context,
		[]byte,
		admin.PasswordDigest,
	) (bool, error) {
		t.Fatal("repository failure reached password verification")
		return false, nil
	}

	account, err := verifyAdministratorCredentialsWith(
		context.Background(),
		reader,
		verify,
		"admin",
		[]byte("synthetic-test-password"),
	)

	if !errors.Is(err, repositoryErr) {
		t.Fatalf("unexpected error: %v", err)
	}

	if account != (admin.Administrator{}) {
		t.Fatal("repository failure returned account")
	}
}

func TestVerifyAdministratorCredentialsInvalidRecord(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*admin.AuthenticationRecord)
	}{
		{
			"invalid account ID",
			func(r *admin.AuthenticationRecord) {
				r.Administrator.ID = "invalid"
			},
		},
		{
			"incorrect username",
			func(r *admin.AuthenticationRecord) {
				r.Administrator.Username = "other"
			},
		},
		{
			"invalid credential revision",
			func(r *admin.AuthenticationRecord) {
				r.Administrator.PasswordChangedAt = time.Time{}
			},
		},
		{
			"invalid digest scheme",
			func(r *admin.AuthenticationRecord) {
				r.Password.Scheme = "unsupported"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := testAuthCredentialRecord()
			tt.mutate(&record)

			reader := &authCredentialTestReader{
				record: record,
			}

			verify := func(
				context.Context,
				[]byte,
				admin.PasswordDigest,
			) (bool, error) {
				t.Fatal("invalid record reached verification")
				return false, nil
			}

			account, err :=
				verifyAdministratorCredentialsWith(
					context.Background(),
					reader,
					verify,
					"admin",
					[]byte("synthetic-test-password"),
				)

			if !errors.Is(
				err,
				errInvalidAuthenticationRecord,
			) {
				t.Fatalf("unexpected error: %v", err)
			}

			if account != (admin.Administrator{}) {
				t.Fatal("invalid record returned account")
			}
		})
	}
}

func TestVerifyAdministratorCredentialsCanceledContext(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)

	cancel()

	reader := &authCredentialTestReader{
		record: testAuthCredentialRecord(),
	}

	verify := func(
		context.Context,
		[]byte,
		admin.PasswordDigest,
	) (bool, error) {
		t.Fatal("canceled request reached verification")
		return false, nil
	}

	_, err := verifyAdministratorCredentialsWith(
		ctx,
		reader,
		verify,
		"admin",
		[]byte("synthetic-test-password"),
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}

	if reader.calls != 0 {
		t.Fatal("canceled request accessed repository")
	}
}

func TestVerifyAdministratorCredentialsCanceledDuringVerification(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	reader := &authCredentialTestReader{
		record: testAuthCredentialRecord(),
	}

	verify := func(
		context.Context,
		[]byte,
		admin.PasswordDigest,
	) (bool, error) {
		cancel()
		return true, nil
	}

	account, err := verifyAdministratorCredentialsWith(
		ctx,
		reader,
		verify,
		"admin",
		[]byte("synthetic-test-password"),
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}

	if account != (admin.Administrator{}) {
		t.Fatal("canceled verification authenticated account")
	}
}

func TestVerifyAdministratorCredentialsMissingDependencies(
	t *testing.T,
) {
	reader := &authCredentialTestReader{}

	verify := func(
		context.Context,
		[]byte,
		admin.PasswordDigest,
	) (bool, error) {
		t.Fatal("missing dependency reached verification")
		return false, nil
	}

	tests := []struct {
		reader AdministratorAuthenticationReader
		verify passwordVerifier
	}{
		{nil, verify},
		{reader, nil},
		{nil, nil},
	}

	for _, tt := range tests {
		_, err := verifyAdministratorCredentialsWith(
			context.Background(),
			tt.reader,
			tt.verify,
			"admin",
			[]byte("synthetic-test-password"),
		)

		if !errors.Is(
			err,
			errInvalidAuthenticationRecord,
		) {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}
func TestVerifyAdministratorCredentialsIntegration(
	t *testing.T,
) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repository := admin.NewRepository(db)

	password := []byte(
		"Synthetic-RackOS-Test-Password-42",
	)
	defer clear(password)

	// Before administrator initialization,
	// authentication must fail normally.
	_, err = verifyAdministratorCredentials(
		ctx,
		repository,
		"admin",
		password,
	)

	if !errors.Is(
		err,
		errInvalidAdministratorCredentials,
	) {
		t.Fatalf("unexpected uninitialized account error: %v", err)
	}

	digest, err := auth.HashPassword(
		ctx,
		"admin",
		password,
	)
	if err != nil {
		t.Fatal(err)
	}

	defer clear(digest.Salt[:])
	defer clear(digest.Hash[:])

	expected, err := repository.CreateInitial(
		ctx,
		"admin",
		digest,
	)
	if err != nil {
		t.Fatal(err)
	}

	account, err := verifyAdministratorCredentials(
		ctx,
		repository,
		"ADMIN",
		password,
	)
	if err != nil {
		t.Fatal(err)
	}

	if account.ID != expected.ID ||
		!account.PasswordChangedAt.Equal(
			expected.PasswordChangedAt,
		) {
		t.Fatal("verified account snapshot was incorrect")
	}

	tests := []struct {
		name     string
		username string
		password []byte
	}{
		{
			"incorrect password",
			"admin",
			[]byte("Incorrect-Synthetic-Password-42"),
		},
		{
			"unknown username",
			"unknown",
			password,
		},
		{
			"invalid password length",
			"admin",
			[]byte("short"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account, err :=
				verifyAdministratorCredentials(
					ctx,
					repository,
					tt.username,
					tt.password,
				)

			if !errors.Is(
				err,
				errInvalidAdministratorCredentials,
			) {
				t.Fatalf("unexpected error: %v", err)
			}

			if account != (admin.Administrator{}) {
				t.Fatal("failed authentication returned account")
			}
		})
	}

	// Password verification must not create
	// sessions or record successful login events.
	var sessions int
	var logins int

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM administrator_sessions`,
	).Scan(&sessions)
	if err != nil {
		t.Fatal(err)
	}

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM audit_events
		 WHERE event_type = 'auth.login.succeeded'`,
	).Scan(&logins)
	if err != nil {
		t.Fatal(err)
	}

	if sessions != 0 || logins != 0 {
		t.Fatal("credential verification modified session state")
	}

	current, err := repository.Get(ctx, expected.ID)
	if err != nil {
		t.Fatal(err)
	}

	if current.LastLoginAt != nil {
		t.Fatal("credential verification modified last login")
	}
}
