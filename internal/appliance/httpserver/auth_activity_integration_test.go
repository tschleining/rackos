package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/session"
)

func TestAuthActivityPersistentHTTPS(t *testing.T) {
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
		"POST "+authActivityPath,
		api.activityHandler(),
	)

	mux.Handle(
		"GET "+authSessionPath,
		api.currentSessionHandler(),
	)

	server := httptest.NewTLSServer(
		protectLocalManagement(mux),
	)
	defer server.Close()

	client := server.Client()
	client.Timeout = 10 * time.Second

	request := func(
		method string,
		path string,
		token string,
		csrf bool,
		expected int,
	) []byte {
		t.Helper()

		req, err := http.NewRequestWithContext(
			ctx,
			method,
			server.URL+path,
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}

		if method == http.MethodPost {
			req.Header.Set("Origin", server.URL)
		}

		if csrf {
			req.Header.Set(
				authCSRFHeaderName,
				authCSRFHeaderValue,
			)
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

		if len(response.Cookies()) != 0 {
			t.Fatal("activity or inspection modified cookie")
		}

		return payload
	}

	// Unauthenticated requests cannot renew
	// administrator sessions.
	request(
		http.MethodPost,
		authActivityPath,
		"",
		true,
		http.StatusUnauthorized,
	)

	// Missing CSRF protection must prevent
	// session renewal.
	request(
		http.MethodPost,
		authActivityPath,
		first.Token(),
		false,
		http.StatusForbidden,
	)

	// Force session updates to fail.
	_, err = db.ExecContext(ctx, `
		CREATE TRIGGER test_reject_auth_activity_update
		BEFORE UPDATE ON administrator_sessions
		BEGIN
			SELECT RAISE(
				ABORT,
				'private-activity-update-error'
			);
		END
	`)
	if err != nil {
		t.Fatal(err)
	}

	request(
		http.MethodPost,
		authActivityPath,
		first.Token(),
		true,
		http.StatusServiceUnavailable,
	)

	// Failed renewal must leave the persisted
	// session metadata unchanged.
	current, err := sessions.Validate(
		ctx,
		first.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if current != first.Session {
		t.Fatal("failed renewal modified session")
	}

	// Read-only inspection must continue to
	// succeed even when session writes fail.
	request(
		http.MethodGet,
		authSessionPath,
		first.Token(),
		false,
		http.StatusOK,
	)

	_, err = db.ExecContext(ctx, `
		DROP TRIGGER test_reject_auth_activity_update
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Persistent timestamps have millisecond
	// precision. Ensure renewal occurs after
	// the original activity timestamp.
	for time.Now().UnixMilli() <=
		first.Session.LastActivityAt.UnixMilli() {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}

	payload := request(
		http.MethodPost,
		authActivityPath,
		first.Token(),
		true,
		http.StatusOK,
	)

	var result authSessionResponse

	if err := json.Unmarshal(
		payload,
		&result,
	); err != nil {
		t.Fatal(err)
	}

	current, err = sessions.Validate(
		ctx,
		first.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !current.LastActivityAt.After(
		first.Session.LastActivityAt,
	) {
		t.Fatal("session activity was not renewed")
	}

	if !current.ExpiresAt.Equal(
		first.Session.ExpiresAt,
	) {
		t.Fatal("absolute session lifetime changed")
	}

	if !current.CreatedAt.Equal(
		first.Session.CreatedAt,
	) {
		t.Fatal("session creation timestamp changed")
	}

	if current.ID != first.Session.ID ||
		current.AdministratorID != account.ID ||
		!current.PasswordChangedAt.Equal(
			first.Session.PasswordChangedAt,
		) {
		t.Fatal("session identity changed")
	}

	if result != toAuthSessionResponse(current) {
		t.Fatal("response did not match persisted session")
	}

	if !result.IdleExpiresAt.Equal(
		current.LastActivityAt.Add(session.IdleTimeout),
	) {
		t.Fatal("incorrect renewed inactivity deadline")
	}

	// Renewal must not affect other sessions
	// belonging to the administrator.
	unrelated, err := sessions.Validate(
		ctx,
		second.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if unrelated != second.Session {
		t.Fatal("renewal modified unrelated session")
	}

	// Repeated read-only inspection must not
	// continue extending the renewed session.
	for i := 0; i < 10; i++ {
		payload := request(
			http.MethodGet,
			authSessionPath,
			first.Token(),
			false,
			http.StatusOK,
		)

		var inspected authSessionResponse

		if err := json.Unmarshal(
			payload,
			&inspected,
		); err != nil {
			t.Fatal(err)
		}

		if inspected != result {
			t.Fatal("session inspection renewed activity")
		}
	}

	// Activity renewal must not create new
	// sessions or successful login events.
	var count int

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM administrator_sessions`,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Fatal("renewal created additional sessions")
	}

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM audit_events
		 WHERE event_type = 'auth.login.succeeded'`,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Fatal("renewal created a login audit event")
	}

	// Revocation must prevent subsequent
	// session activity renewal.
	revoked, err := sessions.Revoke(
		ctx,
		first.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !revoked {
		t.Fatal("session was not revoked")
	}

	request(
		http.MethodPost,
		authActivityPath,
		first.Token(),
		true,
		http.StatusUnauthorized,
	)

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM administrator_sessions
		 WHERE id = ?`,
		first.Session.ID,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatal("activity renewal restored revoked session")
	}

	// Infrastructure failures must fail closed.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	request(
		http.MethodPost,
		authActivityPath,
		second.Token(),
		true,
		http.StatusServiceUnavailable,
	)
}
