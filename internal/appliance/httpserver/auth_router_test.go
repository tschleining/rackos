package httpserver

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/session"
)

func TestAuthRouterAccessPolicy(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)

	token := base64.RawURLEncoding.EncodeToString(
		make([]byte, session.TokenSize),
	)

	tests := []struct {
		name     string
		method   string
		path     string
		scheme   string
		csrf     bool
		cookie   bool
		body     string
		expected int
		calls    int
		allow    string
	}{
		{
			name:     "public health",
			method:   http.MethodGet,
			path:     "/healthz",
			scheme:   "http",
			expected: http.StatusOK,
		},
		{
			name:     "public readiness",
			method:   http.MethodGet,
			path:     "/readyz",
			expected: http.StatusOK,
		},
		{
			name:     "public version",
			method:   http.MethodGet,
			path:     "/api/v1/version",
			expected: http.StatusOK,
		},
		{
			name:     "public login",
			method:   http.MethodPost,
			path:     authLoginPath,
			csrf:     true,
			body:     `{}`,
			expected: http.StatusBadRequest,
		},
		{
			name:     "login without CSRF",
			method:   http.MethodPost,
			path:     authLoginPath,
			expected: http.StatusForbidden,
		},
		{
			name:     "plaintext login",
			method:   http.MethodPost,
			path:     authLoginPath,
			scheme:   "http",
			csrf:     true,
			expected: http.StatusForbidden,
		},
		{
			name:     "unauthenticated login GET",
			method:   http.MethodGet,
			path:     authLoginPath,
			expected: http.StatusUnauthorized,
		},
		{
			name:     "authenticated login GET",
			method:   http.MethodGet,
			path:     authLoginPath,
			cookie:   true,
			expected: http.StatusMethodNotAllowed,
			calls:    1,
			allow:    "POST",
		},
		{
			name:     "login HEAD is not public",
			method:   http.MethodHead,
			path:     authLoginPath,
			expected: http.StatusUnauthorized,
		},
		{
			name:     "login OPTIONS is not public",
			method:   http.MethodOptions,
			path:     authLoginPath,
			expected: http.StatusUnauthorized,
		},
		{
			name:     "logout requires authentication",
			method:   http.MethodPost,
			path:     authLogoutPath,
			csrf:     true,
			expected: http.StatusUnauthorized,
		},
		{
			name:     "logout requires CSRF",
			method:   http.MethodPost,
			path:     authLogoutPath,
			cookie:   true,
			expected: http.StatusForbidden,
		},
		{
			name:     "session requires authentication",
			method:   http.MethodGet,
			path:     authSessionPath,
			expected: http.StatusUnauthorized,
		},
		{
			name:     "authenticated session",
			method:   http.MethodGet,
			path:     authSessionPath,
			cookie:   true,
			expected: http.StatusOK,
			calls:    1,
		},
		{
			name:     "authenticated session HEAD",
			method:   http.MethodHead,
			path:     authSessionPath,
			cookie:   true,
			expected: http.StatusOK,
			calls:    1,
		},
		{
			name:     "unsupported session method",
			method:   http.MethodPost,
			path:     authSessionPath,
			cookie:   true,
			expected: http.StatusMethodNotAllowed,
			calls:    1,
			allow:    "GET, HEAD",
		},
		{
			name:     "activity requires authentication",
			method:   http.MethodPost,
			path:     authActivityPath,
			csrf:     true,
			expected: http.StatusUnauthorized,
		},
		{
			name:     "activity requires CSRF",
			method:   http.MethodPost,
			path:     authActivityPath,
			cookie:   true,
			expected: http.StatusForbidden,
		},
		{
			name:     "protected device API",
			method:   http.MethodGet,
			path:     "/api/v1/devices",
			expected: http.StatusUnauthorized,
		},
		{
			name:     "authenticated device API",
			method:   http.MethodGet,
			path:     "/api/v1/devices",
			cookie:   true,
			expected: http.StatusOK,
			calls:    1,
		},
		{
			name:     "unknown authentication route",
			method:   http.MethodPost,
			path:     "/api/v1/auth/future",
			csrf:     true,
			expected: http.StatusUnauthorized,
		},
		{
			name:     "unknown management route",
			method:   http.MethodGet,
			path:     "/api/v1/future",
			expected: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &authSessionTestStore{
				current: session.Session{
					ID:              "0123456789abcdef0123456789abcdef",
					AdministratorID: "12345678-1234-4000-8000-123456789abc",

					PasswordChangedAt: now,
					CreatedAt:         now,
					LastActivityAt:    now,
					ExpiresAt: now.Add(
						session.AbsoluteLifetime,
					),
				},
			}

			handler :=
				NewAuthenticatedManagementHandlerWithAuth(
					"0.1.0-test",
					alwaysReady,
					testManagementDeviceReader(),
					nil,
					nil,
					nil,
					store,
				)

			scheme := tt.scheme
			if scheme == "" {
				scheme = "https"
			}

			origin := scheme + "://127.0.0.1:8443"

			request := httptest.NewRequest(
				tt.method,
				origin+tt.path,
				strings.NewReader(tt.body),
			)

			if tt.csrf {
				request.Header.Set("Origin", origin)

				request.Header.Set(
					authCSRFHeaderName,
					authCSRFHeaderValue,
				)
			}

			if tt.path == authLoginPath {
				request.Header.Set(
					"Content-Type",
					"application/json",
				)
			}

			if tt.cookie {
				request.AddCookie(&http.Cookie{
					Name:  sessionCookieName,
					Value: token,
				})
			}

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.expected {
				t.Fatalf(
					"expected %d, got %d: %s",
					tt.expected,
					recorder.Code,
					recorder.Body.String(),
				)
			}

			if store.calls != tt.calls {
				t.Fatalf(
					"expected %d validations, got %d",
					tt.calls,
					store.calls,
				)
			}

			if recorder.Header().Get(
				"Cache-Control",
			) != "no-store" {
				t.Fatal("missing cache protection")
			}

			if recorder.Header().Get(
				"Allow",
			) != tt.allow {
				t.Fatal("incorrect Allow header")
			}

			if recorder.Header().Get(
				"Set-Cookie",
			) != "" {
				t.Fatal("unexpected cookie modification")
			}

			if recorder.Header().Get(
				"Access-Control-Allow-Origin",
			) != "" {
				t.Fatal("unexpected CORS authorization")
			}
		})
	}
}

func TestAuthRouterNoncanonicalLoginPaths(
	t *testing.T,
) {
	handler :=
		NewAuthenticatedManagementHandlerWithAuth(
			"0.1.0-test",
			alwaysReady,
			nil,
			nil,
			nil,
			nil,
			nil,
		)

	paths := []string{
		"/api/v1/auth/login/",
		"/api/v1/auth//login",
		"//api/v1/auth/login",
		"/api/v1/auth/./login",
		"/api/v1/auth/other/../login",
		"/api/v1/auth/%6cogin",
		"/api/v1/%61uth/login",
		"/api/v1/auth%2flogin",
		"/api/v1/auth/login%2f",
		"/api/v1/auth/LOGIN",
		"/api/v1/auth/login/extra",
		"/api/v1/auth/login;extra",
	}

	const origin = "https://127.0.0.1:8443"

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				origin+path,
				strings.NewReader(`{}`),
			)

			request.Header.Set("Origin", origin)

			request.Header.Set(
				authCSRFHeaderName,
				authCSRFHeaderValue,
			)

			request.Header.Set(
				"Content-Type",
				"application/json",
			)

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf(
					"alternate login path returned %d",
					recorder.Code,
				)
			}

			if recorder.Header().Get("Location") != "" {
				t.Fatal("unauthenticated path was redirected")
			}

			if recorder.Header().Get("Set-Cookie") != "" {
				t.Fatal("alternate login path modified cookie")
			}
		})
	}
}

func TestAuthRouterSharedLoginLimiter(t *testing.T) {
	handler :=
		NewAuthenticatedManagementHandlerWithAuth(
			"0.1.0-test",
			alwaysReady,
			nil,
			nil,
			nil,
			nil,
			nil,
		)

	const origin = "https://127.0.0.1:8443"

	login := func(
		requestOrigin string,
		csrf bool,
		expected int,
	) {
		t.Helper()

		request := httptest.NewRequest(
			http.MethodPost,
			origin+authLoginPath,
			strings.NewReader(`{}`),
		)

		request.Header.Set("Origin", requestOrigin)

		request.Header.Set(
			"Content-Type",
			"application/json",
		)

		if csrf {
			request.Header.Set(
				authCSRFHeaderName,
				authCSRFHeaderValue,
			)
		}

		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, request)

		if recorder.Code != expected {
			t.Fatalf(
				"expected %d, got %d",
				expected,
				recorder.Code,
			)
		}

		if expected == http.StatusTooManyRequests &&
			recorder.Header().Get("Retry-After") == "" {
			t.Fatal("missing Retry-After header")
		}
	}

	// Rejected security requests must not consume
	// the shared login attempt budget.
	for i := 0; i < 20; i++ {
		login(
			"https://example.com",
			true,
			http.StatusForbidden,
		)

		login(
			origin,
			false,
			http.StatusForbidden,
		)
	}

	for i := 0; i < loginMaxAttempts; i++ {
		login(
			origin,
			true,
			http.StatusBadRequest,
		)
	}

	login(
		origin,
		true,
		http.StatusTooManyRequests,
	)
}
