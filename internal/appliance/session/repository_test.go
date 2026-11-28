package session

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/database"
)

func newSessionTestRepository(
	t *testing.T,
) (*sql.DB, *Repository, admin.Administrator) {
	t.Helper()

	ctx := context.Background()

	db, err := database.Open(
		ctx,
		t.TempDir(),
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	// Synthetic digest for repository tests.
	// Password verification is tested separately.
	account, err := admin.NewRepository(db).
		CreateInitial(
			ctx,
			"admin",
			admin.PasswordDigest{
				Scheme: admin.PasswordScheme,
			},
		)
	if err != nil {
		t.Fatal(err)
	}

	repository := NewRepository(db)

	now := account.PasswordChangedAt.Add(
		time.Second,
	)

	repository.now = func() time.Time {
		return now
	}

	return db, repository, account
}

func assertSessionIssuanceCounts(
	t *testing.T,
	db *sql.DB,
	expectedSessions int,
	expectedLogins int,
) {
	t.Helper()

	var sessions int
	var logins int

	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM administrator_sessions
	`).Scan(&sessions)
	if err != nil {
		t.Fatal(err)
	}

	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM audit_events
		WHERE event_type = 'auth.login.succeeded'
	`).Scan(&logins)
	if err != nil {
		t.Fatal(err)
	}

	if sessions != expectedSessions {
		t.Fatalf(
			"expected %d sessions, got %d",
			expectedSessions,
			sessions,
		)
	}

	if logins != expectedLogins {
		t.Fatalf(
			"expected %d login events, got %d",
			expectedLogins,
			logins,
		)
	}
}

func assertNoIssuedSession(
	t *testing.T,
	issued IssuedSession,
) {
	t.Helper()

	if issued.Token() != "" ||
		issued.Session.ID != "" {
		t.Fatal("failed issuance returned a session")
	}
}

func TestSessionIssue(t *testing.T) {
	ctx := context.Background()

	db, repository, account :=
		newSessionTestRepository(t)

	issued, err := repository.Issue(
		ctx,
		account,
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(issued.Token()) != EncodedTokenSize {
		t.Fatal("invalid issued token")
	}

	if len(issued.Session.ID) != SessionIDSize*2 {
		t.Fatal("invalid issued session identity")
	}

	if issued.Session.AdministratorID != account.ID {
		t.Fatal("incorrect administrator association")
	}

	if !issued.Session.PasswordChangedAt.Equal(
		account.PasswordChangedAt,
	) {
		t.Fatal("incorrect credential revision")
	}

	if !issued.Session.LastActivityAt.Equal(
		issued.Session.CreatedAt,
	) {
		t.Fatal("incorrect initial activity timestamp")
	}

	if issued.Session.ExpiresAt.Sub(
		issued.Session.CreatedAt,
	) != AbsoluteLifetime {
		t.Fatal("incorrect absolute session lifetime")
	}

	digest, err := hashToken(issued.Token())
	if err != nil {
		t.Fatal(err)
	}

	var storedDigest []byte
	var storedAdministratorID string
	var storedRevision int64
	var createdAt int64
	var lastActivityAt int64
	var expiresAt int64

	err = db.QueryRow(`
		SELECT
			token_hash,
			administrator_id,
			password_changed_at,
			created_at,
			last_activity_at,
			expires_at
		FROM administrator_sessions
		WHERE id = ?
	`,
		issued.Session.ID,
	).Scan(
		&storedDigest,
		&storedAdministratorID,
		&storedRevision,
		&createdAt,
		&lastActivityAt,
		&expiresAt,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(storedDigest, digest[:]) {
		t.Fatal("incorrect stored token digest")
	}

	if bytes.Equal(
		storedDigest,
		[]byte(issued.Token()),
	) {
		t.Fatal("plaintext authentication token stored")
	}

	if storedAdministratorID != account.ID ||
		storedRevision != account.PasswordChangedAt.UnixMilli() ||
		createdAt != issued.Session.CreatedAt.UnixMilli() ||
		lastActivityAt != issued.Session.LastActivityAt.UnixMilli() ||
		expiresAt != issued.Session.ExpiresAt.UnixMilli() {
		t.Fatal("stored session metadata is incorrect")
	}

	updated, err := admin.NewRepository(db).
		Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}

	if updated.LastLoginAt == nil ||
		!updated.LastLoginAt.Equal(
			issued.Session.CreatedAt,
		) {
		t.Fatal("administrator login timestamp not updated")
	}

	if !updated.PasswordChangedAt.Equal(
		account.PasswordChangedAt,
	) {
		t.Fatal("session issuance changed password revision")
	}

	var auditCount int

	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM audit_events
		WHERE event_type = 'auth.login.succeeded'
			AND actor_kind = 'administrator'
			AND actor_id = ?
			AND target_kind = 'session'
			AND target_id = ?
			AND outcome = 'success'
	`,
		account.ID,
		issued.Session.ID,
	).Scan(&auditCount)
	if err != nil {
		t.Fatal(err)
	}

	if auditCount != 1 {
		t.Fatal("missing session login audit event")
	}

	assertSessionIssuanceCounts(t, db, 1, 1)
}

func TestSessionIssueMultipleSessions(t *testing.T) {
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

	if first.Session.ID == second.Session.ID {
		t.Fatal("session identity was reused")
	}

	if first.Token() == second.Token() {
		t.Fatal("authentication token was reused")
	}

	assertSessionIssuanceCounts(t, db, 2, 2)
}

func TestSessionIssueRejectsStaleCredentials(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, account :=
		newSessionTestRepository(t)

	adminRepository := admin.NewRepository(db)

	digest := admin.PasswordDigest{
		Scheme: admin.PasswordScheme,
	}
	digest.Hash[0] = 1

	if err := adminRepository.ReplacePassword(
		ctx,
		account.ID,
		digest,
	); err != nil {
		t.Fatal(err)
	}

	// The original account snapshot now contains
	// an obsolete password revision.
	issued, err := repository.Issue(
		ctx,
		account,
	)

	if !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("unexpected error: %v", err)
	}

	assertNoIssuedSession(t, issued)

	assertSessionIssuanceCounts(t, db, 0, 0)

	// A newly verified account snapshot can receive
	// a session using the current password revision.
	current, err := adminRepository.Get(
		ctx,
		account.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	now := current.PasswordChangedAt.Add(time.Second)

	repository.now = func() time.Time {
		return now
	}

	issued, err = repository.Issue(
		ctx,
		current,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !issued.Session.PasswordChangedAt.Equal(
		current.PasswordChangedAt,
	) {
		t.Fatal("session has obsolete credential revision")
	}

	assertSessionIssuanceCounts(t, db, 1, 1)
}

func TestSessionIssueRejectsDisabledAdministrator(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, account :=
		newSessionTestRepository(t)

	// Disable the administrator after obtaining
	// the original authentication snapshot.
	_, err := db.Exec(`
		UPDATE administrators
		SET disabled_at = updated_at
		WHERE id = ?
	`, account.ID)
	if err != nil {
		t.Fatal(err)
	}

	issued, err := repository.Issue(
		ctx,
		account,
	)

	if !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("unexpected error: %v", err)
	}

	assertNoIssuedSession(t, issued)

	// A snapshot already marked disabled must
	// also be rejected.
	disabled, err := admin.NewRepository(db).
		Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}

	issued, err = repository.Issue(ctx, disabled)

	if !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("unexpected error: %v", err)
	}

	assertNoIssuedSession(t, issued)

	assertSessionIssuanceCounts(t, db, 0, 0)
}

func TestSessionIssueRejectsDeletedAdministrator(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, account :=
		newSessionTestRepository(t)

	_, err := db.Exec(`
		DELETE FROM administrators
		WHERE id = ?
	`, account.ID)
	if err != nil {
		t.Fatal(err)
	}

	issued, err := repository.Issue(
		ctx,
		account,
	)

	if !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("unexpected error: %v", err)
	}

	assertNoIssuedSession(t, issued)

	assertSessionIssuanceCounts(t, db, 0, 0)
}

func TestSessionIssueRejectsInvalidAdministrator(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, account :=
		newSessionTestRepository(t)

	invalid := []admin.Administrator{
		{},
		{
			ID:                "invalid",
			Role:              admin.RoleAdministrator,
			PasswordChangedAt: account.PasswordChangedAt,
		},
		{
			ID:                account.ID,
			Role:              "unknown",
			PasswordChangedAt: account.PasswordChangedAt,
		},
		{
			ID:   account.ID,
			Role: admin.RoleAdministrator,
		},
	}

	for _, snapshot := range invalid {
		issued, err := repository.Issue(
			ctx,
			snapshot,
		)

		if !errors.Is(err, ErrInvalidAdministrator) {
			t.Fatalf("unexpected error: %v", err)
		}

		assertNoIssuedSession(t, issued)
	}

	assertSessionIssuanceCounts(t, db, 0, 0)
}

func TestSessionIssueRejectsInvalidClock(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, account :=
		newSessionTestRepository(t)

	clocks := []time.Time{
		time.UnixMilli(0),
		account.PasswordChangedAt.Add(
			-time.Millisecond,
		),
	}

	for _, now := range clocks {
		repository.now = func() time.Time {
			return now
		}

		issued, err := repository.Issue(
			ctx,
			account,
		)

		if !errors.Is(err, ErrInvalidClock) {
			t.Fatalf("unexpected error: %v", err)
		}

		assertNoIssuedSession(t, issued)
	}

	assertSessionIssuanceCounts(t, db, 0, 0)
}

func TestSessionIssueCanceledContext(
	t *testing.T,
) {
	db, repository, account :=
		newSessionTestRepository(t)

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	issued, err := repository.Issue(
		ctx,
		account,
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error: %v", err)
	}

	assertNoIssuedSession(t, issued)

	assertSessionIssuanceCounts(t, db, 0, 0)
}

func TestSessionIssueAuditFailureRollback(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, account :=
		newSessionTestRepository(t)

	_, err := db.Exec(`
		CREATE TRIGGER test_reject_login_audit
		BEFORE INSERT ON audit_events
		WHEN NEW.event_type = 'auth.login.succeeded'
		BEGIN
			SELECT RAISE(
				ABORT,
				'simulated login audit failure'
			);
		END
	`)
	if err != nil {
		t.Fatal(err)
	}

	issued, err := repository.Issue(
		ctx,
		account,
	)

	if err == nil {
		t.Fatal("session issuance ignored audit failure")
	}

	if !strings.Contains(
		err.Error(),
		"record administrative audit event",
	) {
		t.Fatalf("unexpected error: %v", err)
	}

	assertNoIssuedSession(t, issued)

	assertSessionIssuanceCounts(t, db, 0, 0)

	current, err := admin.NewRepository(db).
		Get(ctx, account.ID)
	if err != nil {
		t.Fatal(err)
	}

	if current.LastLoginAt != nil {
		t.Fatal("login timestamp survived rollback")
	}

	if !current.UpdatedAt.Equal(
		account.UpdatedAt,
	) {
		t.Fatal("account update timestamp survived rollback")
	}

	// Remove the simulated failure and confirm
	// that issuance can recover normally.
	_, err = db.Exec(`
		DROP TRIGGER test_reject_login_audit
	`)
	if err != nil {
		t.Fatal(err)
	}

	issued, err = repository.Issue(
		ctx,
		account,
	)
	if err != nil {
		t.Fatal(err)
	}

	if issued.Token() == "" {
		t.Fatal("successful issuance returned no token")
	}

	assertSessionIssuanceCounts(t, db, 1, 1)
}

func TestSessionIssueConcurrentPasswordReplacement(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, account :=
		newSessionTestRepository(t)

	const attempts = 8

	type issueResult struct {
		issued IssuedSession
		err    error
	}

	start := make(chan struct{})

	results := make(chan issueResult, attempts)

	resetResult := make(chan error, 1)

	for i := 0; i < attempts; i++ {
		go func() {
			<-start

			issued, err := repository.Issue(
				ctx,
				account,
			)

			results <- issueResult{
				issued: issued,
				err:    err,
			}
		}()
	}

	go func() {
		<-start

		digest := admin.PasswordDigest{
			Scheme: admin.PasswordScheme,
		}
		digest.Hash[0] = 1

		resetResult <- admin.NewRepository(db).
			ReplacePassword(
				ctx,
				account.ID,
				digest,
			)
	}()

	close(start)

	successful := 0

	for i := 0; i < attempts; i++ {
		result := <-results

		switch {
		case result.err == nil:
			successful++

			if result.issued.Token() == "" {
				t.Error("successful issuance returned no token")
			}

			if !result.issued.Session.
				PasswordChangedAt.Equal(
				account.PasswordChangedAt,
			) {
				t.Error("issuance changed credential revision")
			}

		case errors.Is(
			result.err,
			ErrCredentialsChanged,
		):
			assertNoIssuedSession(t, result.issued)

		default:
			t.Errorf(
				"unexpected issuance error: %v",
				result.err,
			)
		}
	}

	if err := <-resetResult; err != nil {
		t.Fatal(err)
	}

	assertSessionIssuanceCounts(
		t,
		db,
		successful,
		successful,
	)

	// Once replacement has committed, no new
	// session using the old revision may be issued.
	issued, err := repository.Issue(
		ctx,
		account,
	)

	if !errors.Is(err, ErrCredentialsChanged) {
		t.Fatalf("unexpected error: %v", err)
	}

	assertNoIssuedSession(t, issued)

	var matchingRevisionSessions int

	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM administrator_sessions AS s
		JOIN administrators AS a
			ON a.id = s.administrator_id
		WHERE s.password_changed_at =
			a.password_changed_at
	`).Scan(&matchingRevisionSessions)
	if err != nil {
		t.Fatal(err)
	}

	if matchingRevisionSessions != 0 {
		t.Fatal("old session unexpectedly matches new revision")
	}
}
