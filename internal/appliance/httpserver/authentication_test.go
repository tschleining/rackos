package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/session"
)

// Allow tests to inject session validation behavior
// without requiring a database for every test case.
type authenticationValidatorFunc func(
	context.Context,
	string,
) (session.Session, error)

func (f authenticationValidatorFunc) Validate(
	ctx context.Context,
	token string,
) (session.Session, error) {
	return f(ctx, token)
}

func authenticationTestSession() session.Session {
	now := time.UnixMilli(1800000000000).UTC()

	return session.Session{
		ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",

		AdministratorID: "11111111-1111-4111-8111-111111111111",

		PasswordChangedAt: now.Add(-time.Hour),
		CreatedAt:         now,
		LastActivityAt:    now,
		ExpiresAt:         now.Add(session.AbsoluteLifetime),
	}
}

func newAuthenticationTestRequest(
	token string,
) *http.Request {
	request := httptest.NewRequest(
		http.MethodGet,
		"https://127.0.0.1:8080/api/v1/devices",
		nil,
	)

	if token != "" {
		request.Header.Set(
			"Cookie",
			sessionCookieName+"="+token,
		)
	}

	return request
}

func assertAuthenticationError(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
	status int,
	message string,
) {
	t.Helper()

	if recorder.Code != status {
		t.Fatalf(
			"expected status %d, got %d",
			status,
			recorder.Code,
		)
	}

	var response struct {
		Error string `json:"error"`
	}

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatal("invalid JSON error response:", err)
	}

	if response.Error != message {
		t.Fatalf(
			"unexpected authentication message: %q",
			response.Error,
		)
	}

	if recorder.Header().Get(
		"Cache-Control",
	) != "no-store" {
		t.Fatal("authentication error response is cacheable")
	}

	if !strings.HasPrefix(
		recorder.Header().Get("Content-Type"),
		"application/json",
	) {
		t.Fatal("authentication error is not JSON")
	}

	if len(
		recorder.Header().Values("Set-Cookie"),
	) != 0 {
		t.Fatal("authentication failure modified cookies")
	}
}

func TestAuthenticationContextInitiallyEmpty(
	t *testing.T,
) {
	current, ok := authenticatedSession(
		context.Background(),
	)

	if ok || current != (session.Session{}) {
		t.Fatal("unauthenticated context contains identity")
	}
}

func TestAuthenticationValidSession(t *testing.T) {
	expected := authenticationTestSession()

	validationCalls := 0
	handlerCalls := 0

	type requestValueKey struct{}

	request := newAuthenticationTestRequest(
		sessionCookieTestToken,
	)

	request = request.WithContext(
		context.WithValue(
			request.Context(),
			requestValueKey{},
			"original request value",
		),
	)

	validator := authenticationValidatorFunc(
		func(
			ctx context.Context,
			token string,
		) (session.Session, error) {
			validationCalls++

			if token != sessionCookieTestToken {
				t.Error("validator received incorrect token")
			}

			if ctx.Err() != nil {
				t.Error("validation context already canceled")
			}

			deadline, ok := ctx.Deadline()
			if !ok {
				t.Error("validation context has no deadline")
			} else {
				remaining := time.Until(deadline)

				if remaining <= 0 ||
					remaining > authenticationTimeout {
					t.Error("incorrect validation timeout")
				}
			}

			return expected, nil
		},
	)

	next := http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		handlerCalls++

		current, ok := authenticatedSession(
			r.Context(),
		)

		if !ok {
			t.Error("authenticated identity missing")
		}

		if current != expected {
			t.Error("incorrect authenticated identity")
		}

		if r.Context().Err() != nil {
			t.Error("downstream context was canceled")
		}

		if _, ok := r.Context().Deadline(); ok {
			t.Error("validation deadline leaked into handler")
		}

		if r.Context().Value(
			requestValueKey{},
		) != "original request value" {
			t.Error("original request context value was lost")
		}

		encoded, err := json.Marshal(current)
		if err != nil {
			t.Error(err)
		}

		if strings.Contains(
			string(encoded),
			sessionCookieTestToken,
		) {
			t.Error("session metadata exposed authentication token")
		}

		w.WriteHeader(http.StatusNoContent)
	})

	handler := requireAuthenticatedSession(
		validator,
		next,
	)

	recorder := httptest.NewRecorder()

	recorder.Header().Set(
		"Cache-Control",
		"public, max-age=3600",
	)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}

	if validationCalls != 1 || handlerCalls != 1 {
		t.Fatal("unexpected validation or handler call count")
	}

	if recorder.Header().Get(
		"Cache-Control",
	) != "no-store" {
		t.Fatal("authenticated response is cacheable")
	}

	// Authentication must not modify the original
	// request's context.
	if _, ok := authenticatedSession(
		request.Context(),
	); ok {
		t.Fatal("original request context was modified")
	}

	if len(
		recorder.Header().Values("Set-Cookie"),
	) != 0 {
		t.Fatal("ordinary authentication modified cookies")
	}
}

func TestAuthenticationRejectsInvalidCookies(
	t *testing.T,
) {
	validCookie := sessionCookieName +
		"=" + sessionCookieTestToken

	tests := []struct {
		name    string
		cookies []string
	}{
		{
			name: "missing",
		},
		{
			name: "unrelated cookie",
			cookies: []string{
				"theme=dark",
			},
		},
		{
			name: "malformed",
			cookies: []string{
				sessionCookieName + "=invalid",
			},
		},
		{
			name: "duplicate",
			cookies: []string{
				validCookie,
				validCookie,
			},
		},
		{
			name: "noncanonical",
			cookies: []string{
				sessionCookieName + "=" +
					sessionCookieTestToken + "=",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			validator := authenticationValidatorFunc(
				func(
					context.Context,
					string,
				) (session.Session, error) {
					t.Error("invalid cookie reached validator")

					return session.Session{},
						session.ErrInvalidSession
				},
			)

			next := http.HandlerFunc(func(
				http.ResponseWriter,
				*http.Request,
			) {
				t.Error("unauthenticated handler executed")
			})

			handler := requireAuthenticatedSession(
				validator,
				next,
			)

			request := newAuthenticationTestRequest("")

			for _, cookie := range tc.cookies {
				request.Header.Add("Cookie", cookie)
			}

			// Authentication cannot fall back to
			// a bearer token when cookies fail.
			request.Header.Set(
				"Authorization",
				"Bearer "+sessionCookieTestToken,
			)

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(
				recorder,
				request,
			)

			assertAuthenticationError(
				t,
				recorder,
				http.StatusUnauthorized,
				"authentication required",
			)
		})
	}
}

func TestAuthenticationRejectsPlaintextHTTP(
	t *testing.T,
) {
	validator := authenticationValidatorFunc(
		func(
			context.Context,
			string,
		) (session.Session, error) {
			t.Error("plaintext request reached validator")

			return authenticationTestSession(), nil
		},
	)

	next := http.HandlerFunc(func(
		http.ResponseWriter,
		*http.Request,
	) {
		t.Error("plaintext request reached protected handler")
	})

	handler := requireAuthenticatedSession(
		validator,
		next,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"http://127.0.0.1:8080/api/v1/devices",
		nil,
	)

	request.Header.Set(
		"Cookie",
		sessionCookieName+"="+sessionCookieTestToken,
	)

	request.Header.Set(
		"X-Forwarded-Proto",
		"https",
	)

	request.Header.Set(
		"Forwarded",
		"proto=https",
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	assertAuthenticationError(
		t,
		recorder,
		http.StatusForbidden,
		"administrator authentication requires HTTPS",
	)
}

func TestAuthenticationValidationFailures(
	t *testing.T,
) {
	internalError := errors.New(
		"simulated SQLite failure: private diagnostic",
	)

	tests := []struct {
		name    string
		current session.Session
		err     error
		status  int
		message string
	}{
		{
			name:    "invalid session",
			err:     session.ErrInvalidSession,
			status:  http.StatusUnauthorized,
			message: "authentication required",
		},
		{
			name: "wrapped invalid session",
			err: fmt.Errorf(
				"validation failed: %w",
				session.ErrInvalidSession,
			),
			status:  http.StatusUnauthorized,
			message: "authentication required",
		},
		{
			name:    "database failure",
			err:     internalError,
			status:  http.StatusServiceUnavailable,
			message: "authentication service unavailable",
		},
		{
			name:    "validation timeout",
			err:     context.DeadlineExceeded,
			status:  http.StatusServiceUnavailable,
			message: "authentication service unavailable",
		},
		{
			name:    "invalid appliance clock",
			err:     session.ErrInvalidClock,
			status:  http.StatusServiceUnavailable,
			message: "authentication service unavailable",
		},
		{
			name:    "empty successful result",
			status:  http.StatusServiceUnavailable,
			message: "authentication service unavailable",
		},
		{
			name: "invalid administrator identity",
			current: session.Session{
				ID:              "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				AdministratorID: "invalid",
			},
			status:  http.StatusServiceUnavailable,
			message: "authentication service unavailable",
		},
		{
			name:    "metadata returned alongside error",
			current: authenticationTestSession(),
			err:     internalError,
			status:  http.StatusServiceUnavailable,
			message: "authentication service unavailable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0

			validator := authenticationValidatorFunc(
				func(
					context.Context,
					string,
				) (session.Session, error) {
					calls++

					return tc.current, tc.err
				},
			)

			next := http.HandlerFunc(func(
				http.ResponseWriter,
				*http.Request,
			) {
				t.Error("failed authentication reached handler")
			})

			handler := requireAuthenticatedSession(
				validator,
				next,
			)

			request := newAuthenticationTestRequest(
				sessionCookieTestToken,
			)

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(
				recorder,
				request,
			)

			if calls != 1 {
				t.Fatal("unexpected validation call count")
			}

			assertAuthenticationError(
				t,
				recorder,
				tc.status,
				tc.message,
			)

			if strings.Contains(
				recorder.Body.String(),
				"private diagnostic",
			) {
				t.Fatal("internal database details were exposed")
			}

			if strings.Contains(
				recorder.Body.String(),
				sessionCookieTestToken,
			) {
				t.Fatal("authentication token was exposed")
			}
		})
	}
}

func TestAuthenticationMissingValidator(
	t *testing.T,
) {
	next := http.HandlerFunc(func(
		http.ResponseWriter,
		*http.Request,
	) {
		t.Error("missing validator bypassed authentication")
	})

	handler := requireAuthenticatedSession(
		nil,
		next,
	)

	request := newAuthenticationTestRequest(
		sessionCookieTestToken,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	assertAuthenticationError(
		t,
		recorder,
		http.StatusServiceUnavailable,
		"authentication service unavailable",
	)
}

func TestAuthenticationCanceledRequest(
	t *testing.T,
) {
	validator := authenticationValidatorFunc(
		func(
			ctx context.Context,
			token string,
		) (session.Session, error) {
			if !errors.Is(
				ctx.Err(),
				context.Canceled,
			) {
				t.Error("request cancellation was not propagated")
			}

			// Even if a faulty validator returns
			// success, cancellation must fail closed.
			return authenticationTestSession(), nil
		},
	)

	next := http.HandlerFunc(func(
		http.ResponseWriter,
		*http.Request,
	) {
		t.Error("canceled authentication reached handler")
	})

	handler := requireAuthenticatedSession(
		validator,
		next,
	)

	request := newAuthenticationTestRequest(
		sessionCookieTestToken,
	)

	ctx, cancel := context.WithCancel(
		request.Context(),
	)

	cancel()

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request.WithContext(ctx),
	)

	assertAuthenticationError(
		t,
		recorder,
		http.StatusServiceUnavailable,
		"authentication service unavailable",
	)
}

func TestAuthenticationDatabaseIntegration(
	t *testing.T,
) {
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

	// Synthetic verification material is sufficient
	// here because this test exercises authentication
	// of issued sessions, not password verification.
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

	issued, err := repository.Issue(
		ctx,
		account,
	)
	if err != nil {
		t.Fatal(err)
	}

	handlerCalls := 0

	next := http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		handlerCalls++

		current, ok := authenticatedSession(
			r.Context(),
		)

		if !ok || current != issued.Session {
			t.Error("incorrect authenticated database session")
		}

		w.WriteHeader(http.StatusNoContent)
	})

	handler := requireAuthenticatedSession(
		repository,
		next,
	)

	// Use the actual outer local management
	// security layer as well.
	handler = protectLocalManagement(handler)

	request := func(
		token string,
	) *httptest.ResponseRecorder {
		t.Helper()

		recorder := httptest.NewRecorder()

		handler.ServeHTTP(
			recorder,
			newAuthenticationTestRequest(token),
		)

		return recorder
	}

	// The database uses one pooled connection.
	// Reject all database writes during ordinary
	// authentication to prove it is read-only.
	if _, err := db.Exec(
		"PRAGMA query_only = ON",
	); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		recorder := request(issued.Token())

		if recorder.Code != http.StatusNoContent {
			t.Fatalf(
				"valid session rejected: %d",
				recorder.Code,
			)
		}
	}

	var lastActivity int64

	err = db.QueryRow(`
		SELECT last_activity_at
		FROM administrator_sessions
		WHERE id = ?
	`, issued.Session.ID).Scan(&lastActivity)
	if err != nil {
		t.Fatal(err)
	}

	if lastActivity !=
		issued.Session.LastActivityAt.UnixMilli() {
		t.Fatal("ordinary authentication renewed session")
	}

	// Restore writes before testing revocation.
	if _, err := db.Exec(
		"PRAGMA query_only = OFF",
	); err != nil {
		t.Fatal(err)
	}

	// A syntactically valid but unknown token
	// must not authenticate.
	recorder := request(sessionCookieTestToken)

	assertAuthenticationError(
		t,
		recorder,
		http.StatusUnauthorized,
		"authentication required",
	)

	// Revoke the real server-side session.
	revoked, err := repository.Revoke(
		ctx,
		issued.Token(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !revoked {
		t.Fatal("session revocation failed")
	}

	// A copied token must no longer authenticate.
	recorder = request(issued.Token())

	assertAuthenticationError(
		t,
		recorder,
		http.StatusUnauthorized,
		"authentication required",
	)

	// Simulate an actual unavailable database,
	// rather than just a mocked validation error.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	recorder = request(issued.Token())

	assertAuthenticationError(
		t,
		recorder,
		http.StatusServiceUnavailable,
		"authentication service unavailable",
	)

	if handlerCalls != 5 {
		t.Fatalf(
			"expected 5 authenticated handler calls, got %d",
			handlerCalls,
		)
	}
}
