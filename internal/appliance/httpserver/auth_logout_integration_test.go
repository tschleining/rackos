package httpserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/session"
)

func TestAuthLogoutPersistentHTTPS(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	db, err := database.Open(
		ctx,
		t.TempDir(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	administrators := admin.NewRepository(db)
	sessions := session.NewRepository(db)

	// Synthetic digest. Password verification
	// is already covered by the login tests.
	account, err := administrators.CreateInitial(
		ctx,
		"admin",
		admin.PasswordDigest{
			Scheme: admin.PasswordScheme,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	first, err := sessions.Issue(
		ctx,
		account,
	)
	if err != nil {
		t.Fatal(err)
	}

	second, err := sessions.Issue(
		ctx,
		account,
	)
	if err != nil {
		t.Fatal(err)
	}

	api := &authAPIHandler{
		administrators: administrators,
		sessions:       sessions,
	}

	mux := http.NewServeMux()

	mux.Handle(
		"POST "+authLogoutPath,
		api.logoutHandler(),
	)

	server := httptest.NewTLSServer(
		protectLocalManagement(mux),
	)
	defer server.Close()

	client := server.Client()
	client.Timeout = 10 * time.Second

	logout := func(
		token string,
		expected int,
	) *http.Response {
		t.Helper()

		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			server.URL+authLogoutPath,
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}

		request.Header.Set(
			"Origin",
			server.URL,
		)

		request.Header.Set(
			authCSRFHeaderName,
			authCSRFHeaderValue,
		)

		request.AddCookie(&http.Cookie{
			Name:  sessionCookieName,
			Value: token,
		})

		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()

		payload, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}

		if response.StatusCode != expected {
			t.Fatalf(
				"expected %d, got %d",
				expected,
				response.StatusCode,
			)
		}

		if response.Header.Get(
			"Cache-Control",
		) != "no-store" {
			t.Fatal("missing cache protection")
		}

		if expected == http.StatusNoContent {
			if len(payload) != 0 {
				t.Fatal("logout returned a response body")
			}
		} else if len(response.Cookies()) != 0 {
			t.Fatal("failed logout modified cookies")
		}

		return response
	}

	assertLogoutCount := func(
		expected int,
	) {
		t.Helper()

		var count int

		err := db.QueryRowContext(
			ctx,
			`SELECT COUNT(*)
			 FROM audit_events
			 WHERE event_type = 'auth.logout'`,
		).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}

		if count != expected {
			t.Fatalf(
				"expected %d logout events, got %d",
				expected,
				count,
			)
		}
	}

	// Force logout audit logging to fail.
	// Session deletion must roll back.
	_, err = db.ExecContext(ctx, `
		CREATE TRIGGER test_reject_auth_logout_audit
		BEFORE INSERT ON audit_events
		WHEN NEW.event_type = 'auth.logout'
		BEGIN
			SELECT RAISE(
				ABORT,
				'private-logout-audit-error'
			);
		END
	`)
	if err != nil {
		t.Fatal(err)
	}

	logout(
		first.Token(),
		http.StatusServiceUnavailable,
	)

	// Failed logout must preserve the session.
	if _, err := sessions.Validate(
		ctx,
		first.Token(),
	); err != nil {
		t.Fatal("failed logout revoked session:", err)
	}

	assertLogoutCount(0)

	_, err = db.ExecContext(ctx, `
		DROP TRIGGER test_reject_auth_logout_audit
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Retry logout after the infrastructure
	// failure has been resolved.
	response := logout(
		first.Token(),
		http.StatusNoContent,
	)

	cookies := response.Cookies()

	if len(cookies) != 1 {
		t.Fatal("expected one deletion cookie")
	}

	cookie := cookies[0]

	if cookie.Name != sessionCookieName ||
		cookie.Value != "" ||
		cookie.MaxAge >= 0 ||
		cookie.Expires.IsZero() ||
		!cookie.Secure ||
		!cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteStrictMode ||
		cookie.Path != "/" ||
		cookie.Domain != "" {
		t.Fatal("incorrect session cookie deletion")
	}

	// The original token must no longer be usable,
	// even if the client retains a copy.
	_, err = sessions.Validate(
		ctx,
		first.Token(),
	)

	if !errors.Is(
		err,
		session.ErrInvalidSession,
	) {
		t.Fatalf(
			"revoked session remained usable: %v",
			err,
		)
	}

	// Other sessions belonging to the same
	// administrator must remain authenticated.
	if _, err := sessions.Validate(
		ctx,
		second.Token(),
	); err != nil {
		t.Fatal("logout revoked an unrelated session:", err)
	}

	assertLogoutCount(1)

	// Verify the audit event identifies the exact
	// administrator and revoked session.
	var count int

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM audit_events
		 WHERE event_type = 'auth.logout'
		   AND actor_id = ?
		   AND target_id = ?`,
		account.ID,
		first.Session.ID,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatal("incorrect logout audit identity")
	}

	// Replaying an already revoked token must
	// fail without creating another audit event.
	logout(
		first.Token(),
		http.StatusUnauthorized,
	)

	assertLogoutCount(1)

	// Exactly one unrelated session should remain.
	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM administrator_sessions`,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf(
			"expected one remaining session, got %d",
			count,
		)
	}

	// The remaining session can independently
	// perform its own authenticated logout.
	logout(
		second.Token(),
		http.StatusNoContent,
	)

	assertLogoutCount(2)

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM administrator_sessions`,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatal("revoked sessions remained persisted")
	}
}
