package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/audit"
	"github.com/tschleining/rackos/internal/appliance/auth"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/session"
)

func TestAdminSessionIntegrationConcurrentRecovery(
	t *testing.T,
) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		20*time.Second,
	)
	defer cancel()

	fixture := newAdminSessionTestFixture(t)

	// Simulate the independent connection used
	// by the offline administrator recovery CLI.
	recoveryDB, err := database.OpenExisting(
		ctx,
		fixture.dir,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer recoveryDB.Close()

	// Finish password hashing before starting the
	// race, so we exercise database transactions
	// rather than Argon2id execution time.
	password := []byte(testReplacementAdminPassword)
	defer clear(password)

	digest, err := auth.HashPassword(
		ctx,
		"admin",
		password,
	)
	if err != nil {
		t.Fatal(err)
	}

	clear(password)
	defer clear(digest.Salt[:])
	defer clear(digest.Hash[:])

	const attempts = 8

	type issueResult struct {
		issued session.IssuedSession
		err    error
	}

	start := make(chan struct{})

	results := make(chan issueResult, attempts)

	recoveryResult := make(chan error, 1)

	for i := 0; i < attempts; i++ {
		go func() {
			<-start

			issued, err :=
				fixture.sessionRepository.Issue(
					ctx,
					fixture.original.Administrator,
				)

			results <- issueResult{
				issued: issued,
				err:    err,
			}
		}()
	}

	go func() {
		<-start

		_, err := commitAdministratorChange(
			ctx,
			recoveryDB,
			"reset-password",
			"admin",
			fixture.original.Administrator,
			digest,
		)

		recoveryResult <- err
	}()

	close(start)

	issuedDuringRace := make(
		[]session.IssuedSession,
		0,
		attempts,
	)

	for i := 0; i < attempts; i++ {
		select {
		case result := <-results:
			switch {
			case result.err == nil:
				issuedDuringRace = append(
					issuedDuringRace,
					result.issued,
				)

			case errors.Is(
				result.err,
				session.ErrCredentialsChanged,
			):
				if result.issued.Token() != "" ||
					result.issued.Session.ID != "" {
					t.Error("rejected issuance returned a session")
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

	select {
	case err := <-recoveryResult:
		if err != nil {
			t.Fatal("concurrent recovery failed:", err)
		}

	case <-ctx.Done():
		t.Fatal("password recovery timed out")
	}

	// Regardless of which transaction acquired
	// the writer lock first, no old session may
	// remain once recovery commits.
	assertAdminSessionCount(t, fixture.db, 0)

	allOldSessions := append(
		fixture.sessions,
		issuedDuringRace...,
	)

	for _, issued := range allOldSessions {
		current, err :=
			fixture.sessionRepository.Validate(
				ctx,
				issued.Token(),
			)

		if !errors.Is(
			err,
			session.ErrInvalidSession,
		) {
			t.Fatalf(
				"old session survived concurrent recovery: %v",
				err,
			)
		}

		if current != (session.Session{}) {
			t.Fatal("revoked session returned metadata")
		}
	}

	// The original credential snapshot must no
	// longer permit session issuance.
	rejected, err :=
		fixture.sessionRepository.Issue(
			ctx,
			fixture.original.Administrator,
		)

	if !errors.Is(
		err,
		session.ErrCredentialsChanged,
	) {
		t.Fatalf("stale credentials accepted: %v", err)
	}

	if rejected.Token() != "" {
		t.Fatal("stale credentials returned a token")
	}

	current, err :=
		fixture.adminRepository.GetAuthenticationRecord(
			ctx,
			"admin",
		)
	if err != nil {
		t.Fatal(err)
	}

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

	assertAdminSessionAuditCount(
		t,
		fixture.db,
		audit.EventAdminPasswordChanged,
		1,
	)

	assertAdminSessionAuditCount(
		t,
		fixture.db,
		audit.EventLoginSucceeded,
		2+len(issuedDuringRace),
	)

	assertAdminSessionAuditCount(
		t,
		fixture.db,
		audit.EventLogout,
		0,
	)

	assertAdminAuditEventCount(
		t,
		fixture.db,
		4+len(issuedDuringRace),
	)

	// Reauthentication with the new credential
	// revision must still work.
	fresh, err :=
		fixture.sessionRepository.Issue(
			ctx,
			current.Administrator,
		)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.sessionRepository.Validate(
		ctx,
		fresh.Token(),
	); err != nil {
		t.Fatal("replacement session is invalid:", err)
	}

	assertAdminSessionCount(t, fixture.db, 1)

	assertAdminAuditEventCount(
		t,
		fixture.db,
		5+len(issuedDuringRace),
	)
}
