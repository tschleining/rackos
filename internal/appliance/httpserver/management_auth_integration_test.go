package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/session"
)

func TestManagementAuthenticationPersistentSessions(
	t *testing.T,
) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		20*time.Second,
	)
	defer cancel()

	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	// Synthetic password verification material.
	// This test exercises session authentication,
	// not administrator password verification.
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

	repository := session.NewRepository(db)

	handler := NewAuthenticatedManagementHandler(
		"0.1.0-test",
		db.PingContext,
		testManagementDeviceReader(),
		nil,
		nil,
		repository,
	)

	// Exercise actual HTTPS requests, including
	// local connection authority validation.
	server := httptest.NewTLSServer(handler)
	defer server.Close()

	client := server.Client()
	client.Timeout = 5 * time.Second

	request := func(
		path string,
		token string,
		expectedStatus int,
	) {
		t.Helper()

		req, err := http.NewRequestWithContext(
			ctx,
			http.MethodGet,
			server.URL+path,
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}

		if token != "" {
			req.AddCookie(&http.Cookie{
				Name:  sessionCookieName,
				Value: token,
			})
		}

		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()

		if response.StatusCode != expectedStatus {
			t.Fatalf(
				"%s: expected status %d, got %d",
				path,
				expectedStatus,
				response.StatusCode,
			)
		}

		if response.Header.Get("Cache-Control") !=
			"no-store" {
			t.Fatal("missing cache protection")
		}

		if response.Header.Get("Set-Cookie") != "" {
			t.Fatal("ordinary request modified session cookie")
		}
	}

	const protectedPath = "/api/v1/devices"

	// Public endpoints must remain accessible
	// without administrator authentication.
	request("/healthz", "", http.StatusOK)
	request("/readyz", "", http.StatusOK)
	request("/api/v1/version", "", http.StatusOK)

	// Protected endpoints reject missing sessions.
	request(
		protectedPath,
		"",
		http.StatusUnauthorized,
	)

	issued, err := repository.Issue(ctx, account)
	if err != nil {
		t.Fatal(err)
	}

	request(
		protectedPath,
		issued.Token(),
		http.StatusOK,
	)

	// Reject any attempt to update persisted
	// sessions during ordinary authentication.
	_, err = db.ExecContext(ctx, `
		CREATE TRIGGER test_reject_session_activity
		BEFORE UPDATE ON administrator_sessions
		BEGIN
			SELECT RAISE(
				ABORT,
				'unexpected session update'
			);
		END
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Dashboard polling must remain read-only.
	for i := 0; i < 5; i++ {
		request(
			protectedPath,
			issued.Token(),
			http.StatusOK,
		)
	}

	var lastActivity int64
	var expiresAt int64

	err = db.QueryRowContext(
		ctx,
		`SELECT last_activity_at, expires_at
		 FROM administrator_sessions
		 WHERE id = ?`,
		issued.Session.ID,
	).Scan(&lastActivity, &expiresAt)
	if err != nil {
		t.Fatal(err)
	}

	if lastActivity !=
		issued.Session.LastActivityAt.UnixMilli() {
		t.Fatal("authentication renewed session activity")
	}

	if expiresAt !=
		issued.Session.ExpiresAt.UnixMilli() {
		t.Fatal("authentication extended session expiration")
	}

	_, err = db.ExecContext(ctx, `
		DROP TRIGGER test_reject_session_activity
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Revoking a session must immediately prevent
	// subsequent HTTP authentication.
	revoked, err := repository.Revoke(
		ctx,
		issued.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !revoked {
		t.Fatal("session was not revoked")
	}

	request(
		protectedPath,
		issued.Token(),
		http.StatusUnauthorized,
	)

	// A newly issued session remains usable.
	second, err := repository.Issue(ctx, account)
	if err != nil {
		t.Fatal(err)
	}

	request(
		protectedPath,
		second.Token(),
		http.StatusOK,
	)

	// Simulate the persistent session invalidation
	// operation performed during daemon startup.
	count, err := repository.InvalidateOnStartup(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf(
			"expected one invalidated session, got %d",
			count,
		)
	}

	request(
		protectedPath,
		second.Token(),
		http.StatusUnauthorized,
	)

	// Authentication infrastructure failures must
	// fail closed without exposing database errors.
	third, err := repository.Issue(ctx, account)
	if err != nil {
		t.Fatal(err)
	}

	request(
		protectedPath,
		third.Token(),
		http.StatusOK,
	)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	request(
		protectedPath,
		third.Token(),
		http.StatusServiceUnavailable,
	)

	// Liveness remains independent of database
	// availability, while readiness must fail.
	request("/healthz", "", http.StatusOK)

	request(
		"/readyz",
		"",
		http.StatusServiceUnavailable,
	)
}
