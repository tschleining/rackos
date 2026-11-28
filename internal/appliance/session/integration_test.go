package session

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/database"
)

// Open two independent database handles to the same
// SQLite file, rather than sharing one connection pool.
func openIndependentSessionRepositories(
	t *testing.T,
) (
	*sql.DB,
	*Repository,
	*Repository,
	admin.Administrator,
) {
	t.Helper()

	ctx := context.Background()
	dir := t.TempDir()

	firstDB, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		firstDB.Close()
	})

	secondDB, err := database.OpenExisting(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		secondDB.Close()
	})

	account, err := admin.NewRepository(firstDB).
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

	first := NewRepository(firstDB)
	second := NewRepository(secondDB)

	now := account.PasswordChangedAt.Add(time.Second)

	setSessionTestClock(first, now)
	setSessionTestClock(second, now)

	return firstDB, first, second, account
}

func TestSessionIntegrationIndependentConnectionsCapacity(
	t *testing.T,
) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		20*time.Second,
	)
	defer cancel()

	db, first, second, account :=
		openIndependentSessionRepositories(t)

	for i := 0; i < MaxActiveSessions-1; i++ {
		if _, err := first.Issue(
			ctx,
			account,
		); err != nil {
			t.Fatal(err)
		}
	}

	const attempts = 12

	type result struct {
		issued IssuedSession
		err    error
	}

	start := make(chan struct{})
	results := make(chan result, attempts)

	for i := 0; i < attempts; i++ {
		repository := first
		if i%2 != 0 {
			repository = second
		}

		go func(r *Repository) {
			<-start

			issued, err := r.Issue(ctx, account)

			results <- result{
				issued: issued,
				err:    err,
			}
		}(repository)
	}

	close(start)

	successful := 0
	limited := 0

	for i := 0; i < attempts; i++ {
		select {
		case result := <-results:
			switch {
			case result.err == nil:
				successful++

				if result.issued.Token() == "" {
					t.Error("successful issuance returned no token")
				}

			case errors.Is(
				result.err,
				ErrSessionLimit,
			):
				limited++

				if result.issued.Token() != "" ||
					result.issued.Session.ID != "" {
					t.Error("limited issuance returned a token")
				}

			default:
				t.Errorf(
					"unexpected issuance error: %v",
					result.err,
				)
			}

		case <-ctx.Done():
			t.Fatal("concurrent issuance timed out")
		}
	}

	if successful != 1 || limited != attempts-1 {
		t.Fatalf(
			"expected one success and %d rejections; got %d and %d",
			attempts-1,
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

func TestSessionIntegrationActivityWaitsForWriter(
	t *testing.T,
) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	db, first, second, account :=
		openIndependentSessionRepositories(t)

	issued, err := first.Issue(ctx, account)
	if err != nil {
		t.Fatal(err)
	}

	// Hold a write lock using the first connection.
	writer, err := beginWriteTransaction(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()

	var clock atomic.Int64

	clock.Store(
		issued.Session.CreatedAt.Add(
			IdleTimeout - time.Millisecond,
		).UnixMilli(),
	)

	sampled := make(chan struct{}, 1)

	second.now = func() time.Time {
		select {
		case sampled <- struct{}{}:
		default:
		}

		return time.UnixMilli(clock.Load()).UTC()
	}

	type result struct {
		current Session
		err     error
	}

	results := make(chan result, 1)

	go func() {
		current, err := second.RecordActivity(
			ctx,
			issued.Token(),
		)

		results <- result{
			current: current,
			err:     err,
		}
	}()

	// Wait until the second repository holds its
	// independent database connection.
	deadline := time.Now().Add(2 * time.Second)

	for second.db.Stats().InUse == 0 {
		if time.Now().After(deadline) {
			t.Fatal("activity did not acquire a connection")
		}

		time.Sleep(time.Millisecond)
	}

	// The clock must not be sampled while the
	// first connection still holds the writer lock.
	select {
	case <-sampled:
		t.Fatal("session clock sampled before writer lock")

	case result := <-results:
		t.Fatalf(
			"activity returned before lock release: %v",
			result.err,
		)

	case <-time.After(100 * time.Millisecond):
	}

	// Expire the session while the activity
	// operation is waiting for the writer lock.
	clock.Store(
		issued.Session.CreatedAt.Add(
			IdleTimeout,
		).UnixMilli(),
	)

	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}

	select {
	case result := <-results:
		if !errors.Is(
			result.err,
			ErrInvalidSession,
		) {
			t.Fatalf(
				"expired activity was not rejected: %v",
				result.err,
			)
		}

		if result.current != (Session{}) {
			t.Fatal("expired activity returned session metadata")
		}

	case <-ctx.Done():
		t.Fatal("activity operation timed out")
	}

	assertStoredSessionActivity(
		t,
		db,
		issued.Session.ID,
		issued.Session.LastActivityAt,
	)
}

func waitForIntegrationSessionCount(
	t *testing.T,
	db *sql.DB,
	expected int,
) {
	t.Helper()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		var count int

		err := db.QueryRowContext(
			ctx,
			`SELECT COUNT(*)
			 FROM administrator_sessions`,
		).Scan(&count)

		if err != nil {
			t.Fatal(err)
		}

		if count == expected {
			return
		}

		select {
		case <-ctx.Done():
			t.Fatalf(
				"expected %d sessions, last count was %d",
				expected,
				count,
			)

		case <-ticker.C:
		}
	}
}

func TestSessionIntegrationPeriodicMaintenance(
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

	activity := first.Session.CreatedAt.Add(
		20 * time.Minute,
	)

	setSessionTestClock(repository, activity)

	if _, err := repository.RecordActivity(
		ctx,
		second.Token(),
	); err != nil {
		t.Fatal(err)
	}

	var clock atomic.Int64

	clock.Store(
		first.Session.CreatedAt.Add(
			IdleTimeout,
		).UnixMilli(),
	)

	// All worker clock changes are synchronized.
	repository.now = func() time.Time {
		return time.UnixMilli(clock.Load()).UTC()
	}

	workerCtx, cancel := context.WithCancel(ctx)

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	done := make(chan struct{})

	go func() {
		defer close(done)

		repository.runMaintenance(
			workerCtx,
			logger,
			10*time.Millisecond,
		)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	// Initial cleanup must remove the first expired
	// session while preserving the active session.
	waitForIntegrationSessionCount(t, db, 1)

	if _, err := repository.Validate(
		ctx,
		second.Token(),
	); err != nil {
		t.Fatal("active session did not survive cleanup:", err)
	}

	// Move time forward without directly calling
	// Cleanup. A subsequent ticker cycle must
	// remove the second session.
	clock.Store(
		activity.Add(IdleTimeout).UnixMilli(),
	)

	waitForIntegrationSessionCount(t, db, 0)

	assertSessionRejected(
		t,
		repository,
		first.Token(),
	)

	assertSessionRejected(
		t,
		repository,
		second.Token(),
	)

	cancel()

	select {
	case <-done:

	case <-time.After(5 * time.Second):
		t.Fatal("maintenance worker did not terminate")
	}

	assertSessionIssuanceCounts(t, db, 0, 2)
	assertLogoutCount(t, db, 0)
}
