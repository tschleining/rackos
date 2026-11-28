package session

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
)

func issueValidationTestSession(
	t *testing.T,
) (*sql.DB, *Repository, IssuedSession) {
	t.Helper()

	db, repository, account :=
		newSessionTestRepository(t)

	issued, err := repository.Issue(
		context.Background(),
		account,
	)
	if err != nil {
		t.Fatal(err)
	}

	return db, repository, issued
}

func setSessionTestClock(
	repository *Repository,
	now time.Time,
) {
	repository.now = func() time.Time {
		return now
	}
}

func assertStoredSessionActivity(
	t *testing.T,
	db *sql.DB,
	sessionID string,
	expected time.Time,
) {
	t.Helper()

	var timestamp int64

	err := db.QueryRow(`
		SELECT last_activity_at
		FROM administrator_sessions
		WHERE id = ?
	`, sessionID).Scan(&timestamp)
	if err != nil {
		t.Fatal(err)
	}

	if timestamp != expected.UnixMilli() {
		t.Fatalf(
			"expected activity timestamp %d, got %d",
			expected.UnixMilli(),
			timestamp,
		)
	}
}

// Both validation and activity renewal must reject
// an unauthorized session without returning metadata.
func assertSessionRejected(
	t *testing.T,
	repository *Repository,
	token string,
) {
	t.Helper()

	ctx := context.Background()

	operations := []struct {
		name string
		run  func(context.Context, string) (Session, error)
	}{
		{"validate", repository.Validate},
		{"record activity", repository.RecordActivity},
	}

	for _, operation := range operations {
		current, err := operation.run(ctx, token)

		if !errors.Is(err, ErrInvalidSession) {
			t.Fatalf(
				"%s: expected invalid session, got %v",
				operation.name,
				err,
			)
		}

		if current != (Session{}) {
			t.Fatalf(
				"%s: rejected session returned metadata",
				operation.name,
			)
		}
	}
}

func TestSessionValidate(t *testing.T) {
	ctx := context.Background()

	db, repository, issued :=
		issueValidationTestSession(t)

	current, err := repository.Validate(
		ctx,
		issued.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if current != issued.Session {
		t.Fatal("validated session metadata differs")
	}

	assertStoredSessionActivity(
		t,
		db,
		issued.Session.ID,
		issued.Session.LastActivityAt,
	)

	assertSessionIssuanceCounts(t, db, 1, 1)
}

func TestSessionValidationPollingDoesNotRenew(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, issued :=
		issueValidationTestSession(t)

	created := issued.Session.CreatedAt

	pollTimes := []time.Duration{
		5 * time.Minute,
		10 * time.Minute,
		15 * time.Minute,
		20 * time.Minute,
		25 * time.Minute,
		IdleTimeout - time.Millisecond,
	}

	for _, elapsed := range pollTimes {
		setSessionTestClock(
			repository,
			created.Add(elapsed),
		)

		current, err := repository.Validate(
			ctx,
			issued.Token(),
		)
		if err != nil {
			t.Fatal(err)
		}

		if !current.LastActivityAt.Equal(created) {
			t.Fatal("validation renewed inactivity window")
		}
	}

	// The exact idle expiration boundary is invalid.
	setSessionTestClock(
		repository,
		created.Add(IdleTimeout),
	)

	assertSessionRejected(
		t,
		repository,
		issued.Token(),
	)

	assertStoredSessionActivity(
		t,
		db,
		issued.Session.ID,
		created,
	)

	// Validation does not delete expired records.
	// Cleanup will be implemented separately.
	assertSessionIssuanceCounts(t, db, 1, 1)
}

func TestSessionRecordActivity(t *testing.T) {
	ctx := context.Background()

	db, repository, issued :=
		issueValidationTestSession(t)

	activity := issued.Session.CreatedAt.Add(
		20 * time.Minute,
	)

	setSessionTestClock(repository, activity)

	current, err := repository.RecordActivity(
		ctx,
		issued.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !current.LastActivityAt.Equal(activity) {
		t.Fatal("activity timestamp was not renewed")
	}

	if current.ID != issued.Session.ID ||
		current.AdministratorID !=
			issued.Session.AdministratorID ||
		!current.CreatedAt.Equal(
			issued.Session.CreatedAt,
		) ||
		!current.PasswordChangedAt.Equal(
			issued.Session.PasswordChangedAt,
		) {
		t.Fatal("activity changed immutable session metadata")
	}

	if !current.ExpiresAt.Equal(
		issued.Session.ExpiresAt,
	) {
		t.Fatal("activity changed absolute expiration")
	}

	assertStoredSessionActivity(
		t,
		db,
		issued.Session.ID,
		activity,
	)

	// A second activity operation in the same
	// millisecond must remain valid.
	repeated, err := repository.RecordActivity(
		ctx,
		issued.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if repeated != current {
		t.Fatal("same-time activity changed metadata")
	}

	// The original idle deadline has passed, but
	// qualifying activity renewed the idle window.
	setSessionTestClock(
		repository,
		issued.Session.CreatedAt.Add(IdleTimeout),
	)

	_, err = repository.Validate(
		ctx,
		issued.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	// One millisecond before the renewed deadline.
	setSessionTestClock(
		repository,
		activity.Add(
			IdleTimeout-time.Millisecond,
		),
	)

	_, err = repository.Validate(
		ctx,
		issued.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	// At the renewed deadline, both operations fail.
	setSessionTestClock(
		repository,
		activity.Add(IdleTimeout),
	)

	assertSessionRejected(
		t,
		repository,
		issued.Token(),
	)

	assertStoredSessionActivity(
		t,
		db,
		issued.Session.ID,
		activity,
	)

	// Activity must not generate additional
	// successful login audit events.
	assertSessionIssuanceCounts(t, db, 1, 1)
}

func TestSessionValidationAbsoluteExpiration(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, issued :=
		issueValidationTestSession(t)

	created := issued.Session.CreatedAt

	lastActivity := created

	// Maintain qualifying activity every 29 minutes
	// throughout the original session lifetime.
	for elapsed := 29 * time.Minute; elapsed < AbsoluteLifetime; elapsed += 29 * time.Minute {

		lastActivity = created.Add(elapsed)

		setSessionTestClock(
			repository,
			lastActivity,
		)

		current, err := repository.RecordActivity(
			ctx,
			issued.Token(),
		)
		if err != nil {
			t.Fatal(err)
		}

		if !current.ExpiresAt.Equal(
			issued.Session.ExpiresAt,
		) {
			t.Fatal("absolute expiration was extended")
		}
	}

	// Ensure inactivity is not the reason for the
	// session rejection at the absolute deadline.
	if issued.Session.ExpiresAt.Sub(
		lastActivity,
	) >= IdleTimeout {
		t.Fatal("test session was already idle")
	}

	setSessionTestClock(
		repository,
		issued.Session.ExpiresAt.Add(
			-time.Millisecond,
		),
	)

	_, err := repository.Validate(
		ctx,
		issued.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	setSessionTestClock(
		repository,
		issued.Session.ExpiresAt,
	)

	assertSessionRejected(
		t,
		repository,
		issued.Token(),
	)

	setSessionTestClock(
		repository,
		issued.Session.ExpiresAt.Add(time.Hour),
	)

	assertSessionRejected(
		t,
		repository,
		issued.Token(),
	)

	assertStoredSessionActivity(
		t,
		db,
		issued.Session.ID,
		lastActivity,
	)

	assertSessionIssuanceCounts(t, db, 1, 1)
}

func TestSessionValidationRejectsInvalidTokens(
	t *testing.T,
) {
	db, repository, issued :=
		issueValidationTestSession(t)

	unknownToken, _, err := generateToken()
	if err != nil {
		t.Fatal(err)
	}

	tokens := []string{
		"",
		"invalid",
		issued.Session.ID,
		issued.Token() + "=",
		issued.Token() + " ",
		unknownToken,
	}

	for _, token := range tokens {
		assertSessionRejected(
			t,
			repository,
			token,
		)
	}

	assertStoredSessionActivity(
		t,
		db,
		issued.Session.ID,
		issued.Session.LastActivityAt,
	)

	assertSessionIssuanceCounts(t, db, 1, 1)
}

func TestSessionValidationRejectsChangedCredentials(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, issued :=
		issueValidationTestSession(t)

	digest := admin.PasswordDigest{
		Scheme: admin.PasswordScheme,
	}
	digest.Hash[0] = 1

	err := admin.NewRepository(db).ReplacePassword(
		ctx,
		issued.Session.AdministratorID,
		digest,
	)
	if err != nil {
		t.Fatal(err)
	}

	assertSessionRejected(
		t,
		repository,
		issued.Token(),
	)

	assertStoredSessionActivity(
		t,
		db,
		issued.Session.ID,
		issued.Session.LastActivityAt,
	)

	// Step 6F will physically remove these sessions.
	// For now, the revision mismatch invalidates them.
	assertSessionIssuanceCounts(t, db, 1, 1)
}

func TestSessionValidationRejectsDisabledAdministrator(
	t *testing.T,
) {
	db, repository, issued :=
		issueValidationTestSession(t)

	_, err := db.Exec(`
		UPDATE administrators
		SET disabled_at = updated_at
		WHERE id = ?
	`, issued.Session.AdministratorID)
	if err != nil {
		t.Fatal(err)
	}

	assertSessionRejected(
		t,
		repository,
		issued.Token(),
	)

	assertStoredSessionActivity(
		t,
		db,
		issued.Session.ID,
		issued.Session.LastActivityAt,
	)
}

func TestSessionValidationRejectsDeletedAdministrator(
	t *testing.T,
) {
	db, repository, issued :=
		issueValidationTestSession(t)

	_, err := db.Exec(`
		DELETE FROM administrators
		WHERE id = ?
	`, issued.Session.AdministratorID)
	if err != nil {
		t.Fatal(err)
	}

	assertSessionRejected(
		t,
		repository,
		issued.Token(),
	)

	// Sessions are deleted by the foreign key,
	// but their historical audit events survive.
	assertSessionIssuanceCounts(t, db, 0, 1)
}

func TestSessionValidationRejectsClockRollback(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, issued :=
		issueValidationTestSession(t)

	created := issued.Session.CreatedAt

	// Reject a clock earlier than session creation.
	setSessionTestClock(
		repository,
		created.Add(-time.Millisecond),
	)

	assertSessionRejected(
		t,
		repository,
		issued.Token(),
	)

	activity := created.Add(10 * time.Minute)

	setSessionTestClock(repository, activity)

	_, err := repository.RecordActivity(
		ctx,
		issued.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Reject a clock earlier than the most recent
	// qualifying activity, even if it is later
	// than the original creation timestamp.
	setSessionTestClock(
		repository,
		activity.Add(-time.Millisecond),
	)

	assertSessionRejected(
		t,
		repository,
		issued.Token(),
	)

	assertStoredSessionActivity(
		t,
		db,
		issued.Session.ID,
		activity,
	)
}

func TestSessionValidationRejectsInvalidClock(
	t *testing.T,
) {
	ctx := context.Background()

	_, repository, issued :=
		issueValidationTestSession(t)

	setSessionTestClock(
		repository,
		time.UnixMilli(0),
	)

	operations := []func(
		context.Context,
		string,
	) (Session, error){
		repository.Validate,
		repository.RecordActivity,
	}

	for _, operation := range operations {
		current, err := operation(
			ctx,
			issued.Token(),
		)

		if !errors.Is(err, ErrInvalidClock) {
			t.Fatalf("unexpected clock error: %v", err)
		}

		if current != (Session{}) {
			t.Fatal("invalid clock returned session metadata")
		}
	}
}

func TestSessionValidationCanceledContext(
	t *testing.T,
) {
	db, repository, issued :=
		issueValidationTestSession(t)

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	operations := []func(
		context.Context,
		string,
	) (Session, error){
		repository.Validate,
		repository.RecordActivity,
	}

	for _, operation := range operations {
		current, err := operation(
			ctx,
			issued.Token(),
		)

		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected context error: %v", err)
		}

		if current != (Session{}) {
			t.Fatal("canceled operation returned session metadata")
		}
	}

	assertStoredSessionActivity(
		t,
		db,
		issued.Session.ID,
		issued.Session.LastActivityAt,
	)
}

func TestSessionValidationIsReadOnly(t *testing.T) {
	ctx := context.Background()

	db, repository, issued :=
		issueValidationTestSession(t)

	// The appliance database uses a single pooled
	// connection. Temporarily disallow database writes
	// on that connection.
	_, err := db.Exec("PRAGMA query_only = ON")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if _, err := db.Exec(
			"PRAGMA query_only = OFF",
		); err != nil {
			t.Error(err)
		}
	}()

	setSessionTestClock(
		repository,
		issued.Session.CreatedAt.Add(time.Minute),
	)

	// Ordinary validation must succeed without
	// performing any database writes.
	_, err = repository.Validate(
		ctx,
		issued.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Renewal, unlike validation, requires a write.
	current, err := repository.RecordActivity(
		ctx,
		issued.Token(),
	)

	if err == nil {
		t.Fatal("activity unexpectedly succeeded in read-only mode")
	}

	if errors.Is(err, ErrInvalidSession) {
		t.Fatal("storage failure was treated as invalid session")
	}

	if current != (Session{}) {
		t.Fatal("failed renewal returned session metadata")
	}

	assertStoredSessionActivity(
		t,
		db,
		issued.Session.ID,
		issued.Session.LastActivityAt,
	)
}

func TestSessionRecordActivityStorageFailure(
	t *testing.T,
) {
	ctx := context.Background()

	db, repository, issued :=
		issueValidationTestSession(t)

	activity := issued.Session.CreatedAt.Add(
		time.Minute,
	)

	setSessionTestClock(repository, activity)

	_, err := db.Exec(`
		CREATE TRIGGER test_reject_session_activity
		BEFORE UPDATE OF last_activity_at
		ON administrator_sessions
		BEGIN
			SELECT RAISE(
				ABORT,
				'simulated session activity failure'
			);
		END
	`)
	if err != nil {
		t.Fatal(err)
	}

	current, err := repository.RecordActivity(
		ctx,
		issued.Token(),
	)

	if err == nil {
		t.Fatal("activity ignored storage failure")
	}

	if errors.Is(err, ErrInvalidSession) {
		t.Fatal("storage failure was treated as invalid session")
	}

	if current != (Session{}) {
		t.Fatal("failed activity returned session metadata")
	}

	assertStoredSessionActivity(
		t,
		db,
		issued.Session.ID,
		issued.Session.LastActivityAt,
	)

	// Removing the simulated failure should allow
	// subsequent renewal of the still-valid session.
	_, err = db.Exec(`
		DROP TRIGGER test_reject_session_activity
	`)
	if err != nil {
		t.Fatal(err)
	}

	current, err = repository.RecordActivity(
		ctx,
		issued.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !current.LastActivityAt.Equal(activity) {
		t.Fatal("session renewal did not recover")
	}

	assertStoredSessionActivity(
		t,
		db,
		issued.Session.ID,
		activity,
	)
}
