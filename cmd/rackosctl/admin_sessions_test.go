package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/audit"
	"github.com/tschleining/rackos/internal/appliance/auth"
	"github.com/tschleining/rackos/internal/appliance/session"
)

type adminSessionTestFixture struct {
	dir string
	db  *sql.DB

	adminRepository   *admin.Repository
	sessionRepository *session.Repository

	original admin.AuthenticationRecord
	sessions []session.IssuedSession
}

func newAdminSessionTestFixture(
	t *testing.T,
) adminSessionTestFixture {
	t.Helper()

	ctx := context.Background()

	dir, db := setupDeviceCommandDB(t)

	// Initialize the administrator through the
	// actual audited CLI command.
	_, _, err := executeAdminCLI(
		adminTestPrompt(
			testInitialAdminPassword,
			testInitialAdminPassword,
		),
		"init",
		"--username", "admin",
		"--confirm-offline",
		"--data-dir", dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	adminRepository := admin.NewRepository(db)

	original, err :=
		adminRepository.GetAuthenticationRecord(
			ctx,
			"admin",
		)
	if err != nil {
		t.Fatal(err)
	}

	valid, err := auth.VerifyPassword(
		ctx,
		[]byte(testInitialAdminPassword),
		original.Password,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !valid {
		t.Fatal("initial password verification failed")
	}

	sessionRepository := session.NewRepository(db)

	sessions := make([]session.IssuedSession, 0, 2)

	// Simulate two independently authenticated
	// browser sessions.
	for i := 0; i < 2; i++ {
		issued, err := sessionRepository.Issue(
			ctx,
			original.Administrator,
		)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := sessionRepository.Validate(
			ctx,
			issued.Token(),
		); err != nil {
			t.Fatal(err)
		}

		sessions = append(sessions, issued)
	}

	// Refresh the account metadata after issuance
	// because last_login_at and updated_at changed.
	original, err =
		adminRepository.GetAuthenticationRecord(
			ctx,
			"admin",
		)
	if err != nil {
		t.Fatal(err)
	}

	assertAdminSessionCount(t, db, 2)

	// One administrator creation event and two
	// successful login events.
	assertAdminAuditEventCount(t, db, 3)

	return adminSessionTestFixture{
		dir:               dir,
		db:                db,
		adminRepository:   adminRepository,
		sessionRepository: sessionRepository,
		original:          original,
		sessions:          sessions,
	}
}

func assertAdminSessionCount(
	t *testing.T,
	db *sql.DB,
	expected int,
) {
	t.Helper()

	var count int

	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM administrator_sessions
	`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != expected {
		t.Fatalf(
			"expected %d administrator sessions, got %d",
			expected,
			count,
		)
	}
}

func assertAdminSessionAuditCount(
	t *testing.T,
	db *sql.DB,
	event audit.EventType,
	expected int,
) {
	t.Helper()

	var count int

	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM audit_events
		WHERE event_type = ?
	`, string(event)).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != expected {
		t.Fatalf(
			"expected %d %s events, got %d",
			expected,
			event,
			count,
		)
	}
}

func assertAdminSessionPassword(
	t *testing.T,
	password string,
	digest admin.PasswordDigest,
	expected bool,
) {
	t.Helper()

	valid, err := auth.VerifyPassword(
		context.Background(),
		[]byte(password),
		digest,
	)
	if err != nil {
		t.Fatal(err)
	}

	if valid != expected {
		t.Fatal("unexpected password verification result")
	}
}

func TestAdminSessionRecoveryRevokesAll(
	t *testing.T,
) {
	ctx := context.Background()

	fixture := newAdminSessionTestFixture(t)

	stdout, _, err := executeAdminCLI(
		adminTestPrompt(
			testReplacementAdminPassword,
			testReplacementAdminPassword,
		),
		"reset-password",
		"--username", "admin",
		"--confirm-offline",
		"--data-dir", fixture.dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(
		stdout,
		"Administrator password replaced: admin",
	) {
		t.Fatal("missing successful recovery confirmation")
	}

	current, err :=
		fixture.adminRepository.GetAuthenticationRecord(
			ctx,
			"admin",
		)
	if err != nil {
		t.Fatal(err)
	}

	if current.Administrator.ID !=
		fixture.original.Administrator.ID {
		t.Fatal("password recovery changed account identity")
	}

	if !current.Administrator.PasswordChangedAt.After(
		fixture.original.Administrator.PasswordChangedAt,
	) {
		t.Fatal("password revision did not advance")
	}

	if current.Password.Salt ==
		fixture.original.Password.Salt {
		t.Fatal("password replacement reused salt")
	}

	if current.Password.Hash ==
		fixture.original.Password.Hash {
		t.Fatal("password hash did not change")
	}

	// Successful recovery must physically remove
	// all previously issued sessions.
	assertAdminSessionCount(t, fixture.db, 0)

	for _, issued := range fixture.sessions {
		validated, err :=
			fixture.sessionRepository.Validate(
				ctx,
				issued.Token(),
			)

		if !errors.Is(err, session.ErrInvalidSession) {
			t.Fatalf(
				"old session was not rejected: %v",
				err,
			)
		}

		if validated != (session.Session{}) {
			t.Fatal("revoked session returned metadata")
		}

		renewed, err :=
			fixture.sessionRepository.RecordActivity(
				ctx,
				issued.Token(),
			)

		if !errors.Is(err, session.ErrInvalidSession) {
			t.Fatalf(
				"revoked session could be renewed: %v",
				err,
			)
		}

		if renewed != (session.Session{}) {
			t.Fatal("revoked session returned metadata")
		}
	}

	// Historical audit records must survive
	// session invalidation.
	assertAdminAuditEventCount(t, fixture.db, 4)

	assertAdminSessionAuditCount(
		t,
		fixture.db,
		audit.EventAdminCreated,
		1,
	)

	assertAdminSessionAuditCount(
		t,
		fixture.db,
		audit.EventLoginSucceeded,
		2,
	)

	assertAdminAuditEvent(
		t,
		fixture.db,
		audit.EventAdminPasswordChanged,
		current.Administrator.ID,
	)

	assertAdminSessionAuditCount(
		t,
		fixture.db,
		audit.EventLogout,
		0,
	)

	// An authentication attempt that verified the
	// previous password before recovery must not
	// be able to issue a new session afterward.
	stale, err := fixture.sessionRepository.Issue(
		ctx,
		fixture.original.Administrator,
	)

	if !errors.Is(err, session.ErrCredentialsChanged) {
		t.Fatalf(
			"stale credentials were not rejected: %v",
			err,
		)
	}

	if stale.Token() != "" ||
		stale.Session.ID != "" {
		t.Fatal("stale credentials returned a session")
	}

	assertAdminSessionCount(t, fixture.db, 0)

	assertAdminAuditEventCount(t, fixture.db, 4)

	// Only the replacement password should work.
	assertAdminSessionPassword(
		t,
		testInitialAdminPassword,
		current.Password,
		false,
	)

	assertAdminSessionPassword(
		t,
		testReplacementAdminPassword,
		current.Password,
		true,
	)

	// Reauthentication using the newly verified
	// account snapshot must work normally.
	replacement, err :=
		fixture.sessionRepository.Issue(
			ctx,
			current.Administrator,
		)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.sessionRepository.Validate(
		ctx,
		replacement.Token(),
	); err != nil {
		t.Fatal("new session is invalid:", err)
	}

	if !replacement.Session.PasswordChangedAt.Equal(
		current.Administrator.PasswordChangedAt,
	) {
		t.Fatal("new session has incorrect password revision")
	}

	for _, old := range fixture.sessions {
		if replacement.Token() == old.Token() ||
			replacement.Session.ID == old.Session.ID {
			t.Fatal("old session credentials were reused")
		}
	}

	assertAdminSessionCount(t, fixture.db, 1)

	assertAdminAuditEventCount(t, fixture.db, 5)
}

func TestAdminSessionRecoveryRollback(
	t *testing.T,
) {
	tests := []struct {
		name          string
		trigger       string
		expectedError string
	}{
		{
			name: "session deletion failure",
			trigger: `
				CREATE TRIGGER test_reject_session_revocation
				BEFORE DELETE ON administrator_sessions
				BEGIN
					SELECT RAISE(
						ABORT,
						'simulated session revocation failure'
					);
				END
			`,
			expectedError: "revoke administrator sessions",
		},
		{
			name: "audit insertion failure",
			trigger: `
				CREATE TRIGGER test_reject_password_audit
				BEFORE INSERT ON audit_events
				WHEN NEW.event_type = 'admin.password.changed'
				BEGIN
					SELECT RAISE(
						ABORT,
						'simulated password audit failure'
					);
				END
			`,
			expectedError: "record administrative audit event",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			fixture := newAdminSessionTestFixture(t)

			if _, err := fixture.db.Exec(
				tc.trigger,
			); err != nil {
				t.Fatal(err)
			}

			stdout, _, err := executeAdminCLI(
				adminTestPrompt(
					testReplacementAdminPassword,
					testReplacementAdminPassword,
				),
				"reset-password",
				"--username", "admin",
				"--confirm-offline",
				"--data-dir", fixture.dir,
			)

			if err == nil {
				t.Fatal("recovery ignored storage failure")
			}

			if !strings.Contains(
				err.Error(),
				tc.expectedError,
			) {
				t.Fatalf("unexpected error: %v", err)
			}

			if strings.Contains(
				stdout,
				"Administrator password replaced:",
			) {
				t.Fatal("failed recovery reported success")
			}

			current, err :=
				fixture.adminRepository.GetAuthenticationRecord(
					ctx,
					"admin",
				)
			if err != nil {
				t.Fatal(err)
			}

			// Password verification material must
			// remain completely unchanged.
			if current.Password !=
				fixture.original.Password {
				t.Fatal("password replacement survived rollback")
			}

			if !current.Administrator.PasswordChangedAt.Equal(
				fixture.original.Administrator.PasswordChangedAt,
			) {
				t.Fatal("credential revision survived rollback")
			}

			if !current.Administrator.UpdatedAt.Equal(
				fixture.original.Administrator.UpdatedAt,
			) {
				t.Fatal("account update survived rollback")
			}

			// Both original sessions must survive
			// the failed recovery transaction.
			assertAdminSessionCount(
				t,
				fixture.db,
				len(fixture.sessions),
			)

			for _, issued := range fixture.sessions {
				validated, err :=
					fixture.sessionRepository.Validate(
						ctx,
						issued.Token(),
					)
				if err != nil {
					t.Fatal(
						"session was invalidated by failed recovery:",
						err,
					)
				}

				if validated != issued.Session {
					t.Fatal("session metadata changed after rollback")
				}
			}

			// The original password must still work.
			assertAdminSessionPassword(
				t,
				testInitialAdminPassword,
				current.Password,
				true,
			)

			assertAdminSessionPassword(
				t,
				testReplacementAdminPassword,
				current.Password,
				false,
			)

			// No successful password change or logout
			// event may survive the failed transaction.
			assertAdminAuditEventCount(
				t,
				fixture.db,
				3,
			)

			assertAdminSessionAuditCount(
				t,
				fixture.db,
				audit.EventAdminPasswordChanged,
				0,
			)

			assertAdminSessionAuditCount(
				t,
				fixture.db,
				audit.EventLoginSucceeded,
				2,
			)

			assertAdminSessionAuditCount(
				t,
				fixture.db,
				audit.EventLogout,
				0,
			)
		})
	}
}
