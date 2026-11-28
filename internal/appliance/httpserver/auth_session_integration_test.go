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

func TestAuthCurrentSessionPersistentHTTPS(t *testing.T) {
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

	issued, err := sessions.Issue(
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
		token string,
		expected int,
	) []byte {
		t.Helper()

		req, err := http.NewRequestWithContext(
			ctx,
			method,
			server.URL+authSessionPath,
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

		payload, err := io.ReadAll(
			response.Body,
		)
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
			t.Fatal("session inspection modified cookie")
		}

		return payload
	}

	// Unauthenticated requests must not receive
	// administrator session metadata.
	request(
		http.MethodGet,
		"",
		http.StatusUnauthorized,
	)

	// Reject every attempted session update.
	// Session inspection must remain read-only.
	_, err = db.ExecContext(ctx, `
		CREATE TRIGGER test_reject_auth_session_update
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

	expected := toAuthSessionResponse(
		issued.Session,
	)

	// Repeated frontend session checks must
	// not extend the authenticated session.
	for i := 0; i < 10; i++ {
		payload := request(
			http.MethodGet,
			issued.Token(),
			http.StatusOK,
		)

		var response authSessionResponse

		if err := json.Unmarshal(
			payload,
			&response,
		); err != nil {
			t.Fatal(err)
		}

		if response != expected {
			t.Fatal("session metadata changed during inspection")
		}
	}

	// A real HTTP HEAD response must not
	// contain a response body.
	payload := request(
		http.MethodHead,
		issued.Token(),
		http.StatusOK,
	)

	if len(payload) != 0 {
		t.Fatal("HEAD response contained a body")
	}

	var lastActivityAt int64
	var expiresAt int64

	err = db.QueryRowContext(
		ctx,
		`SELECT last_activity_at, expires_at
		 FROM administrator_sessions
		 WHERE id = ?`,
		issued.Session.ID,
	).Scan(
		&lastActivityAt,
		&expiresAt,
	)
	if err != nil {
		t.Fatal(err)
	}

	if lastActivityAt !=
		issued.Session.LastActivityAt.UnixMilli() {
		t.Fatal("session inspection renewed activity")
	}

	if expiresAt !=
		issued.Session.ExpiresAt.UnixMilli() {
		t.Fatal("session inspection extended expiration")
	}

	// Session inspection must not create
	// additional successful login audit events.
	var loginCount int

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM audit_events
		 WHERE event_type = 'auth.login.succeeded'`,
	).Scan(&loginCount)
	if err != nil {
		t.Fatal(err)
	}

	if loginCount != 1 {
		t.Fatal("session inspection created login audit events")
	}

	_, err = db.ExecContext(ctx, `
		DROP TRIGGER test_reject_auth_session_update
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Revoked sessions must immediately fail
	// subsequent session inspection requests.
	revoked, err := sessions.Revoke(
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
		http.MethodGet,
		issued.Token(),
		http.StatusUnauthorized,
	)

	// Issue another session to verify that
	// infrastructure failures fail closed.
	second, err := sessions.Issue(
		ctx,
		account,
	)
	if err != nil {
		t.Fatal(err)
	}

	request(
		http.MethodGet,
		second.Token(),
		http.StatusOK,
	)

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	request(
		http.MethodGet,
		second.Token(),
		http.StatusServiceUnavailable,
	)
}
