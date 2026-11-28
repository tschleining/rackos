package session

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
)

func assertLifecycleCount(
	t *testing.T,
	db *sql.DB,
	query string,
	expected int,
	args ...any,
) {
	t.Helper()

	var count int

	if err := db.QueryRow(
		query,
		args...,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != expected {
		t.Fatalf(
			"expected count %d, got %d",
			expected,
			count,
		)
	}
}

func assertLogoutCount(
	t *testing.T,
	db *sql.DB,
	expected int,
) {
	t.Helper()

	assertLifecycleCount(
		t,
		db,
		`SELECT COUNT(*)
		 FROM audit_events
		 WHERE event_type = 'auth.logout'`,
		expected,
	)
}

func TestSessionLifecycleRevoke(t *testing.T) {
	ctx := context.Background()

	db, repository, issued :=
		issueValidationTestSession(t)

	revoked, err := repository.Revoke(
		ctx,
		issued.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !revoked {
		t.Fatal("valid session was not revoked")
	}

	assertSessionRejected(
		t,
		repository,
		issued.Token(),
	)

	assertSessionIssuanceCounts(t, db, 0, 1)
	assertLogoutCount(t, db, 1)

	assertLifecycleCount(
		t,
		db,
		`SELECT COUNT(*)
		 FROM audit_events
		 WHERE event_type = 'auth.logout'
		   AND actor_kind = 'administrator'
		   AND actor_id = ?
		   AND target_kind = 'session'
		   AND target_id = ?
		   AND outcome = 'success'`,
		1,
		issued.Session.AdministratorID,
		issued.Session.ID,
	)

	unknown, _, err := generateToken()
	if err != nil {
		t.Fatal(err)
	}

	// Repeated or invalid logout operations must
	// not generate additional successful events.
	for _, token := range []string{
		issued.Token(),
		unknown,
		"",
		"invalid",
	} {
		revoked, err := repository.Revoke(ctx, token)
		if err != nil {
			t.Fatal(err)
		}

		if revoked {
			t.Fatal("invalid session was revoked")
		}
	}

	assertLogoutCount(t, db, 1)
}

func TestSessionLifecycleRevokeRollback(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, issued :=
		issueValidationTestSession(t)

	_, err := db.Exec(`
		CREATE TRIGGER test_reject_logout_audit
		BEFORE INSERT ON audit_events
		WHEN NEW.event_type = 'auth.logout'
		BEGIN
			SELECT RAISE(
				ABORT,
				'simulated logout audit failure'
			);
		END
	`)
	if err != nil {
		t.Fatal(err)
	}

	revoked, err := repository.Revoke(
		ctx,
		issued.Token(),
	)

	if err == nil || revoked {
		t.Fatal("logout ignored audit storage failure")
	}

	// Failed logout must not partially delete
	// the session.
	_, err = repository.Validate(
		ctx,
		issued.Token(),
	)
	if err != nil {
		t.Fatal("session did not survive rollback:", err)
	}

	assertSessionIssuanceCounts(t, db, 1, 1)
	assertLogoutCount(t, db, 0)

	_, err = db.Exec(`
		DROP TRIGGER test_reject_logout_audit
	`)
	if err != nil {
		t.Fatal(err)
	}

	revoked, err = repository.Revoke(
		ctx,
		issued.Token(),
	)
	if err != nil || !revoked {
		t.Fatalf("logout did not recover: %v", err)
	}

	assertSessionIssuanceCounts(t, db, 0, 1)
	assertLogoutCount(t, db, 1)
}

func TestSessionLifecycleCleanupIdle(t *testing.T) {
	ctx := context.Background()

	db, repository, account :=
		newSessionTestRepository(t)

	first, err := repository.Issue(ctx, account)
	if err != nil {
		t.Fatal(err)
	}

	second, err := repository.Issue(ctx, account)
	if err != nil {
		t.Fatal(err)
	}

	setSessionTestClock(
		repository,
		first.Session.CreatedAt.Add(20*time.Minute),
	)

	if _, err := repository.RecordActivity(
		ctx,
		second.Token(),
	); err != nil {
		t.Fatal(err)
	}

	setSessionTestClock(
		repository,
		first.Session.CreatedAt.Add(IdleTimeout),
	)

	count, err := repository.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf("expected one expired session, got %d", count)
	}

	assertSessionRejected(t, repository, first.Token())

	if _, err := repository.Validate(
		ctx,
		second.Token(),
	); err != nil {
		t.Fatal("active session was removed:", err)
	}

	assertSessionIssuanceCounts(t, db, 1, 2)
	assertLogoutCount(t, db, 0)

	count, err = repository.Cleanup(ctx)
	if err != nil || count != 0 {
		t.Fatalf("cleanup was not idempotent: %d, %v", count, err)
	}
}

func TestSessionLifecycleCleanupAbsolute(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, issued :=
		issueValidationTestSession(t)

	created := issued.Session.CreatedAt

	// Keep the session active until its absolute
	// expiration timestamp.
	for elapsed := 29 * time.Minute; elapsed < AbsoluteLifetime; elapsed += 29 * time.Minute {

		setSessionTestClock(
			repository,
			created.Add(elapsed),
		)

		if _, err := repository.RecordActivity(
			ctx,
			issued.Token(),
		); err != nil {
			t.Fatal(err)
		}
	}

	setSessionTestClock(
		repository,
		issued.Session.ExpiresAt.Add(-time.Millisecond),
	)

	count, err := repository.Cleanup(ctx)
	if err != nil || count != 0 {
		t.Fatalf("session removed prematurely: %d, %v", count, err)
	}

	setSessionTestClock(
		repository,
		issued.Session.ExpiresAt,
	)

	count, err = repository.Cleanup(ctx)
	if err != nil || count != 1 {
		t.Fatalf("absolute cleanup failed: %d, %v", count, err)
	}

	assertSessionRejected(t, repository, issued.Token())
	assertSessionIssuanceCounts(t, db, 0, 1)
}

func TestSessionLifecycleCleanupObsolete(
	t *testing.T,
) {
	tests := []string{
		"password changed",
		"administrator disabled",
	}

	for _, reason := range tests {
		t.Run(reason, func(t *testing.T) {
			ctx := context.Background()

			db, repository, issued :=
				issueValidationTestSession(t)

			switch reason {
			case "password changed":
				digest := admin.PasswordDigest{
					Scheme: admin.PasswordScheme,
				}
				digest.Hash[0] = 1

				err := admin.NewRepository(db).
					ReplacePassword(
						ctx,
						issued.Session.AdministratorID,
						digest,
					)
				if err != nil {
					t.Fatal(err)
				}

			case "administrator disabled":
				_, err := db.Exec(`
					UPDATE administrators
					SET disabled_at = updated_at
					WHERE id = ?
				`, issued.Session.AdministratorID)
				if err != nil {
					t.Fatal(err)
				}
			}

			count, err := repository.Cleanup(ctx)
			if err != nil || count != 1 {
				t.Fatalf(
					"obsolete cleanup failed: %d, %v",
					count,
					err,
				)
			}

			assertSessionRejected(
				t,
				repository,
				issued.Token(),
			)

			assertSessionIssuanceCounts(t, db, 0, 1)
			assertLogoutCount(t, db, 0)
		})
	}
}

func TestSessionLifecycleCleanupRollback(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, issued :=
		issueValidationTestSession(t)

	setSessionTestClock(
		repository,
		issued.Session.CreatedAt.Add(IdleTimeout),
	)

	_, err := db.Exec(`
		CREATE TRIGGER test_reject_session_cleanup
		BEFORE DELETE ON administrator_sessions
		BEGIN
			SELECT RAISE(
				ABORT,
				'simulated cleanup failure'
			);
		END
	`)
	if err != nil {
		t.Fatal(err)
	}

	count, err := repository.Cleanup(ctx)

	if err == nil || count != 0 {
		t.Fatal("cleanup ignored storage failure")
	}

	assertSessionIssuanceCounts(t, db, 1, 1)

	// The record still exists but cannot authenticate.
	assertSessionRejected(
		t,
		repository,
		issued.Token(),
	)

	_, err = db.Exec(`
		DROP TRIGGER test_reject_session_cleanup
	`)
	if err != nil {
		t.Fatal(err)
	}

	count, err = repository.Cleanup(ctx)
	if err != nil || count != 1 {
		t.Fatalf("cleanup did not recover: %d, %v", count, err)
	}

	assertSessionIssuanceCounts(t, db, 0, 1)
}

func TestSessionLifecycleCapacity(t *testing.T) {
	ctx := context.Background()

	db, repository, account :=
		newSessionTestRepository(t)

	sessions := make(
		[]IssuedSession,
		0,
		MaxActiveSessions,
	)

	for i := 0; i < MaxActiveSessions; i++ {
		issued, err := repository.Issue(ctx, account)
		if err != nil {
			t.Fatal(err)
		}

		sessions = append(sessions, issued)
	}

	rejected, err := repository.Issue(ctx, account)

	if !errors.Is(err, ErrSessionLimit) {
		t.Fatalf("expected session limit, got %v", err)
	}

	assertNoIssuedSession(t, rejected)

	assertSessionIssuanceCounts(
		t,
		db,
		MaxActiveSessions,
		MaxActiveSessions,
	)

	// Existing authenticated sessions are retained.
	for _, issued := range sessions {
		if _, err := repository.Validate(
			ctx,
			issued.Token(),
		); err != nil {
			t.Fatal("existing session was evicted:", err)
		}
	}

	revoked, err := repository.Revoke(
		ctx,
		sessions[0].Token(),
	)
	if err != nil || !revoked {
		t.Fatalf("logout failed: %v", err)
	}

	// Explicit revocation releases a session slot.
	if _, err := repository.Issue(
		ctx,
		account,
	); err != nil {
		t.Fatal(err)
	}

	assertSessionIssuanceCounts(
		t,
		db,
		MaxActiveSessions,
		MaxActiveSessions+1,
	)

	assertLogoutCount(t, db, 1)

	// Issuance itself cleans expired sessions,
	// without waiting for periodic maintenance.
	setSessionTestClock(
		repository,
		sessions[0].Session.CreatedAt.Add(IdleTimeout),
	)

	if _, err := repository.Issue(
		ctx,
		account,
	); err != nil {
		t.Fatal("expired sessions blocked issuance:", err)
	}

	assertSessionIssuanceCounts(
		t,
		db,
		1,
		MaxActiveSessions+2,
	)
}

func TestSessionLifecycleConcurrentCapacity(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, account :=
		newSessionTestRepository(t)

	// Leave exactly one available session slot.
	for i := 0; i < MaxActiveSessions-1; i++ {
		if _, err := repository.Issue(
			ctx,
			account,
		); err != nil {
			t.Fatal(err)
		}
	}

	const attempts = 8

	type result struct {
		issued IssuedSession
		err    error
	}

	start := make(chan struct{})
	results := make(chan result, attempts)

	for i := 0; i < attempts; i++ {
		go func() {
			<-start

			issued, err := repository.Issue(
				ctx,
				account,
			)

			results <- result{
				issued: issued,
				err:    err,
			}
		}()
	}

	close(start)

	successful := 0
	limited := 0

	for i := 0; i < attempts; i++ {
		result := <-results

		switch {
		case result.err == nil:
			successful++

			if result.issued.Token() == "" {
				t.Error("successful issuance returned no token")
			}

		case errors.Is(result.err, ErrSessionLimit):
			limited++

			if result.issued.Token() != "" ||
				result.issued.Session.ID != "" {
				t.Error("limited issuance returned a session")
			}

		default:
			t.Errorf(
				"unexpected issuance error: %v",
				result.err,
			)
		}
	}

	if successful != 1 || limited != attempts-1 {
		t.Fatalf(
			"expected one success and seven rejections, got %d and %d",
			successful,
			limited,
		)
	}

	assertSessionIssuanceCounts(
		t,
		db,
		MaxActiveSessions,
		MaxActiveSessions,
	)
}

func TestSessionLifecycleRevokeAll(t *testing.T) {
	ctx := context.Background()

	db, repository, account :=
		newSessionTestRepository(t)

	first, err := repository.Issue(ctx, account)
	if err != nil {
		t.Fatal(err)
	}

	second, err := repository.Issue(ctx, account)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	count, err := RevokeAllForAdministrator(
		ctx,
		tx,
		account.ID,
	)
	if err != nil || count != 2 {
		t.Fatalf("revocation failed: %d, %v", count, err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// The caller can roll back revocation as part
	// of a larger administrative transaction.
	assertSessionIssuanceCounts(t, db, 2, 2)

	if _, err := repository.Validate(
		ctx,
		first.Token(),
	); err != nil {
		t.Fatal(err)
	}

	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	count, err = RevokeAllForAdministrator(
		ctx,
		tx,
		account.ID,
	)
	if err != nil || count != 2 {
		t.Fatalf("revocation failed: %d, %v", count, err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertSessionRejected(t, repository, first.Token())
	assertSessionRejected(t, repository, second.Token())

	assertSessionIssuanceCounts(t, db, 0, 2)
	assertLogoutCount(t, db, 0)
}

func TestSessionLifecycleRevokeAllRequiresTransaction(
	t *testing.T,
) {
	_, _, account := newSessionTestRepository(t)

	count, err := RevokeAllForAdministrator(
		context.Background(),
		nil,
		account.ID,
	)

	if count != 0 ||
		!errors.Is(err, ErrTransactionRequired) {
		t.Fatalf("unexpected result: %d, %v", count, err)
	}
}

func TestSessionLifecycleClockRollback(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, issued :=
		issueValidationTestSession(t)

	created := issued.Session.CreatedAt

	// Observe expiration without physically
	// deleting the session.
	setSessionTestClock(
		repository,
		created.Add(IdleTimeout),
	)

	assertSessionRejected(
		t,
		repository,
		issued.Token(),
	)

	// Roll back to a time when the session would
	// otherwise appear valid again.
	setSessionTestClock(
		repository,
		created.Add(time.Minute),
	)

	assertSessionRejected(
		t,
		repository,
		issued.Token(),
	)

	count, err := repository.Cleanup(ctx)

	if count != 0 ||
		!errors.Is(err, ErrClockRollback) {
		t.Fatalf("unexpected cleanup result: %d, %v", count, err)
	}

	// New sessions must also be rejected while
	// the clock is below the observed high-water mark.
	account, err := admin.NewRepository(db).
		Get(ctx, issued.Session.AdministratorID)
	if err != nil {
		t.Fatal(err)
	}

	newSession, err := repository.Issue(ctx, account)

	if !errors.Is(err, ErrClockRollback) {
		t.Fatalf("unexpected issuance error: %v", err)
	}

	assertNoIssuedSession(t, newSession)

	// Normal operation resumes once the clock
	// reaches the previous high-water mark.
	setSessionTestClock(
		repository,
		created.Add(IdleTimeout),
	)

	count, err = repository.Cleanup(ctx)
	if err != nil || count != 1 {
		t.Fatalf("cleanup did not recover: %d, %v", count, err)
	}

	assertSessionIssuanceCounts(t, db, 0, 1)
}

func TestSessionLifecycleStartupInvalidation(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, account :=
		newSessionTestRepository(t)

	first, err := repository.Issue(ctx, account)
	if err != nil {
		t.Fatal(err)
	}

	second, err := repository.Issue(ctx, account)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a fresh daemon repository operating
	// against the persisted session database.
	restarted := NewRepository(db)

	setSessionTestClock(
		restarted,
		first.Session.CreatedAt,
	)

	count, err := restarted.InvalidateOnStartup(ctx)
	if err != nil || count != 2 {
		t.Fatalf(
			"startup invalidation failed: %d, %v",
			count,
			err,
		)
	}

	assertSessionRejected(t, restarted, first.Token())
	assertSessionRejected(t, restarted, second.Token())

	// Startup invalidation must preserve audit
	// history and administrator credentials.
	assertSessionIssuanceCounts(t, db, 0, 2)

	current, err := admin.NewRepository(db).
		Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !current.PasswordChangedAt.Equal(
		account.PasswordChangedAt,
	) {
		t.Fatal("startup changed administrator credentials")
	}

	if _, err := restarted.Issue(
		ctx,
		current,
	); err != nil {
		t.Fatal("reauthentication failed:", err)
	}

	assertSessionIssuanceCounts(t, db, 1, 3)
}

func TestSessionLifecycleMaintenanceCancellation(
	t *testing.T,
) {
	_, repository, _ := newSessionTestRepository(t)

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	done := make(chan struct{})

	go func() {
		defer close(done)

		repository.RunMaintenance(ctx, logger)
	}()

	select {
	case <-done:

	case <-time.After(time.Second):
		t.Fatal("maintenance worker did not terminate")
	}
}
