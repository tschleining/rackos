package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/auth"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/session"
)

func TestAuthLoginPersistentHTTPS(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	administrators := admin.NewRepository(db)
	sessions := session.NewRepository(db)

	const password = "Synthetic-RackOS-Login-Test-42"

	passwordBytes := []byte(password)
	defer clear(passwordBytes)

	digest, err := auth.HashPassword(
		ctx,
		"admin",
		passwordBytes,
	)
	if err != nil {
		t.Fatal(err)
	}

	defer clear(digest.Salt[:])
	defer clear(digest.Hash[:])

	account, err := administrators.CreateInitial(
		ctx,
		"admin",
		digest,
	)
	if err != nil {
		t.Fatal(err)
	}

	api := &authAPIHandler{
		administrators: administrators,
		sessions:       sessions,
	}

	limiter := newLoginRateLimiter()

	mux := http.NewServeMux()

	mux.Handle(
		"POST "+authLoginPath,
		api.loginHandler(limiter),
	)

	server := httptest.NewTLSServer(
		protectLocalManagement(mux),
	)
	defer server.Close()

	client := server.Client()
	client.Timeout = 10 * time.Second

	login := func(
		body string,
		csrf bool,
		expected int,
	) (*http.Response, []byte) {
		t.Helper()

		request, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			server.URL+authLoginPath,
			strings.NewReader(body),
		)
		if err != nil {
			t.Fatal(err)
		}

		request.Header.Set(
			"Content-Type",
			"application/json",
		)

		request.Header.Set(
			"Origin",
			server.URL,
		)

		if csrf {
			request.Header.Set(
				authCSRFHeaderName,
				authCSRFHeaderValue,
			)
		}

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
				"expected status %d, got %d",
				expected,
				response.StatusCode,
			)
		}

		if response.Header.Get(
			"Cache-Control",
		) != "no-store" {
			t.Fatal("missing cache protection")
		}

		if expected != http.StatusOK {
			if len(response.Cookies()) != 0 {
				t.Fatal("failed login issued a cookie")
			}
		}

		if bytes.Contains(
			payload,
			[]byte(password),
		) {
			t.Fatal("response exposed plaintext password")
		}

		return response, payload
	}

	validBody := `{"username":"ADMIN","password":"` +
		password + `"}`

	// Rejected CSRF requests must not consume
	// the appliance-wide login attempt budget.
	for i := 0; i < loginMaxAttempts+1; i++ {
		login(
			validBody,
			false,
			http.StatusForbidden,
		)
	}

	// Attempt 1: malformed request.
	login(
		`{"username":"admin"}`,
		true,
		http.StatusBadRequest,
	)

	// Attempt 2: incorrect password.
	_, incorrectBody := login(
		`{"username":"admin","password":"Incorrect-Test-Password-42"}`,
		true,
		http.StatusUnauthorized,
	)

	// Attempt 3: nonexistent administrator.
	_, unknownBody := login(
		`{"username":"unknown","password":"`+
			password+`"}`,
		true,
		http.StatusUnauthorized,
	)

	if !bytes.Equal(
		incorrectBody,
		unknownBody,
	) {
		t.Fatal("login errors disclosed account existence")
	}

	// Force transactional audit logging to fail.
	// Session issuance must roll back completely.
	_, err = db.ExecContext(ctx, `
		CREATE TRIGGER test_reject_auth_login_audit
		BEFORE INSERT ON audit_events
		WHEN NEW.event_type = 'auth.login.succeeded'
		BEGIN
			SELECT RAISE(
				ABORT,
				'private-audit-failure'
			);
		END
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Attempt 4: correct password, failed transaction.
	_, failureBody := login(
		validBody,
		true,
		http.StatusServiceUnavailable,
	)

	if bytes.Contains(
		failureBody,
		[]byte("private-audit-failure"),
	) {
		t.Fatal("response exposed database failure")
	}

	var sessionCount int
	var auditCount int

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM administrator_sessions`,
	).Scan(&sessionCount)
	if err != nil {
		t.Fatal(err)
	}

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM audit_events
		 WHERE event_type = 'auth.login.succeeded'`,
	).Scan(&auditCount)
	if err != nil {
		t.Fatal(err)
	}

	if sessionCount != 0 || auditCount != 0 {
		t.Fatal("failed login left committed session state")
	}

	currentAccount, err := administrators.Get(
		ctx,
		account.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if currentAccount.LastLoginAt != nil {
		t.Fatal("failed login modified last login timestamp")
	}

	_, err = db.ExecContext(ctx, `
		DROP TRIGGER test_reject_auth_login_audit
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Attempt 5: successful transactional login.
	response, payload := login(
		validBody,
		true,
		http.StatusOK,
	)

	cookies := response.Cookies()

	if len(cookies) != 1 {
		t.Fatal("expected exactly one session cookie")
	}

	cookie := cookies[0]

	if cookie.Name != sessionCookieName {
		t.Fatal("incorrect authentication cookie name")
	}

	if !cookie.Secure ||
		!cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("incorrect session cookie security attributes")
	}

	if cookie.Path != "/" ||
		cookie.Domain != "" {
		t.Fatal("incorrect session cookie scope")
	}

	if cookie.MaxAge != 0 ||
		!cookie.Expires.IsZero() {
		t.Fatal("cookie was not issued as a browser session cookie")
	}

	if err := session.ValidateTokenEncoding(
		cookie.Value,
	); err != nil {
		t.Fatal("issued cookie contained an invalid token")
	}

	if bytes.Contains(
		payload,
		[]byte(cookie.Value),
	) {
		t.Fatal("authentication token appeared in JSON")
	}

	var result authSessionResponse

	if err := json.Unmarshal(
		payload,
		&result,
	); err != nil {
		t.Fatal(err)
	}

	if result.AdministratorID != account.ID {
		t.Fatal("incorrect authenticated administrator")
	}

	// The issued browser token must authenticate
	// against the actual persistent repository.
	current, err := sessions.Validate(
		ctx,
		cookie.Value,
	)
	if err != nil {
		t.Fatal(err)
	}

	if current.AdministratorID != account.ID ||
		!current.PasswordChangedAt.Equal(
			account.PasswordChangedAt,
		) {
		t.Fatal("issued session has incorrect identity")
	}

	if !result.CreatedAt.Equal(
		current.CreatedAt,
	) ||
		!result.LastActivityAt.Equal(
			current.LastActivityAt,
		) ||
		!result.ExpiresAt.Equal(
			current.ExpiresAt,
		) {
		t.Fatal("response did not match persisted session")
	}

	if !result.IdleExpiresAt.Equal(
		current.LastActivityAt.Add(
			session.IdleTimeout,
		),
	) {
		t.Fatal("incorrect inactivity expiration")
	}

	if bytes.Contains(
		payload,
		[]byte(current.ID),
	) {
		t.Fatal("internal session ID was exposed")
	}

	// Verify the successful login audit record.
	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM audit_events
		 WHERE event_type = 'auth.login.succeeded'
		   AND actor_id = ?
		   AND target_id = ?`,
		account.ID,
		current.ID,
	).Scan(&auditCount)
	if err != nil {
		t.Fatal(err)
	}

	if auditCount != 1 {
		t.Fatal("missing successful login audit event")
	}

	currentAccount, err = administrators.Get(
		ctx,
		account.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if currentAccount.LastLoginAt == nil {
		t.Fatal("successful login did not update last login")
	}

	if !currentAccount.LastLoginAt.Equal(
		current.CreatedAt,
	) {
		t.Fatal("last login timestamp was inconsistent")
	}

	// Attempt 6: successful login must not reset
	// the appliance-wide rate limiting budget.
	limited, _ := login(
		validBody,
		true,
		http.StatusTooManyRequests,
	)

	if limited.Header.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM administrator_sessions`,
	).Scan(&sessionCount)
	if err != nil {
		t.Fatal(err)
	}

	if sessionCount != 1 {
		t.Fatal("unexpected persisted session count")
	}
}
