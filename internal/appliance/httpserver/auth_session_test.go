package httpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/session"
)

type authSessionTestStore struct {
	AdministratorSessionStore

	current session.Session
	err     error
	calls   int
}

func (s *authSessionTestStore) Validate(
	_ context.Context,
	_ string,
) (session.Session, error) {
	s.calls++

	return s.current, s.err
}

func TestAuthCurrentSessionHandler(t *testing.T) {
	now := time.Date(
		2026, time.November, 27,
		12, 0, 0, 0, time.UTC,
	)

	current := session.Session{
		ID:              "0123456789abcdef0123456789abcdef",
		AdministratorID: "12345678-1234-4000-8000-123456789abc",

		PasswordChangedAt: now.Add(-time.Hour),
		CreatedAt:         now,
		LastActivityAt:    now.Add(10 * time.Minute),
		ExpiresAt:         now.Add(session.AbsoluteLifetime),
	}

	token := base64.RawURLEncoding.EncodeToString(
		make([]byte, session.TokenSize),
	)

	tests := []struct {
		name     string
		method   string
		scheme   string
		query    string
		body     string
		origin   string
		noCookie bool
		err      error
		expected int
		calls    int
	}{
		{
			name:     "valid authenticated session",
			expected: http.StatusOK,
			calls:    1,
		},
		{
			name:     "HEAD request",
			method:   http.MethodHead,
			expected: http.StatusOK,
			calls:    1,
		},
		{
			name:     "missing session cookie",
			noCookie: true,
			expected: http.StatusUnauthorized,
		},
		{
			name:     "invalid session",
			err:      session.ErrInvalidSession,
			expected: http.StatusUnauthorized,
			calls:    1,
		},
		{
			name:     "database failure",
			err:      errors.New("private-session-error"),
			expected: http.StatusServiceUnavailable,
			calls:    1,
		},
		{
			name:     "plaintext request",
			scheme:   "http",
			expected: http.StatusForbidden,
		},
		{
			name:     "cross-origin request",
			origin:   "https://example.com",
			expected: http.StatusForbidden,
		},
		{
			name:     "unsupported POST request",
			method:   http.MethodPost,
			expected: http.StatusMethodNotAllowed,
		},
		{
			name:     "unsupported DELETE request",
			method:   http.MethodDelete,
			expected: http.StatusMethodNotAllowed,
		},
		{
			name:     "unexpected query parameter",
			query:    "?refresh=true",
			expected: http.StatusBadRequest,
			calls:    1,
		},
		{
			name:     "empty query marker",
			query:    "?",
			expected: http.StatusBadRequest,
			calls:    1,
		},
		{
			name:     "unexpected request body",
			body:     `{}`,
			expected: http.StatusBadRequest,
			calls:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &authSessionTestStore{
				current: current,
				err:     tt.err,
			}

			api := &authAPIHandler{
				sessions: store,
			}

			handler := protectLocalManagement(
				api.currentSessionHandler(),
			)

			method := tt.method
			if method == "" {
				method = http.MethodGet
			}

			scheme := tt.scheme
			if scheme == "" {
				scheme = "https"
			}

			request := httptest.NewRequest(
				method,
				scheme+"://127.0.0.1:8443"+
					authSessionPath+tt.query,
				strings.NewReader(tt.body),
			)

			if tt.origin != "" {
				request.Header.Set(
					"Origin",
					tt.origin,
				)
			}

			if !tt.noCookie {
				request.AddCookie(&http.Cookie{
					Name:  sessionCookieName,
					Value: token,
				})
			}

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(
				recorder,
				request,
			)

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
				"Set-Cookie",
			) != "" {
				t.Fatal("session inspection modified cookie")
			}

			if strings.Contains(
				recorder.Body.String(),
				"private-session-error",
			) {
				t.Fatal("internal error was exposed")
			}

			if tt.expected ==
				http.StatusMethodNotAllowed {
				if recorder.Header().Get(
					"Allow",
				) != "GET, HEAD" {
					t.Fatal("incorrect Allow header")
				}
			}

			if tt.expected != http.StatusOK {
				return
			}

			// Real HTTP servers suppress HEAD bodies.
			// ResponseRecorder does not simulate this
			// transport-level behavior.
			if method == http.MethodHead {
				return
			}

			var response authSessionResponse

			if err := json.Unmarshal(
				recorder.Body.Bytes(),
				&response,
			); err != nil {
				t.Fatal(err)
			}

			if response.AdministratorID !=
				current.AdministratorID {
				t.Fatal("incorrect administrator identity")
			}

			if !response.CreatedAt.Equal(
				current.CreatedAt,
			) ||
				!response.LastActivityAt.Equal(
					current.LastActivityAt,
				) ||
				!response.ExpiresAt.Equal(
					current.ExpiresAt,
				) {
				t.Fatal("incorrect session timestamps")
			}

			if !response.IdleExpiresAt.Equal(
				current.LastActivityAt.Add(
					session.IdleTimeout,
				),
			) {
				t.Fatal("incorrect inactivity expiration")
			}

			for _, forbidden := range []string{
				token,
				current.ID,
				"password",
				"token",
				"credential",
			} {
				if strings.Contains(
					recorder.Body.String(),
					forbidden,
				) {
					t.Fatal("response exposed internal session data")
				}
			}
		})
	}
}

func TestAuthCurrentSessionMissingStore(t *testing.T) {
	api := &authAPIHandler{}

	handler := protectLocalManagement(
		api.currentSessionHandler(),
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"https://127.0.0.1:8443"+authSessionPath,
		nil,
	)

	request.AddCookie(&http.Cookie{
		Name: sessionCookieName,
		Value: base64.RawURLEncoding.EncodeToString(
			make([]byte, session.TokenSize),
		),
	})

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"expected 503, got %d",
			recorder.Code,
		)
	}
}

func TestAuthCurrentSessionMissingContext(t *testing.T) {
	api := &authAPIHandler{}

	request := httptest.NewRequest(
		http.MethodGet,
		"https://127.0.0.1:8443"+authSessionPath,
		nil,
	)

	recorder := httptest.NewRecorder()

	// Invoke the underlying handler directly
	// without authentication middleware.
	api.currentSession(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf(
			"expected 401, got %d",
			recorder.Code,
		)
	}
}
