package httpserver

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/session"
)

// Embedding the interface allows this test double
// to implement only the operations logout requires.
type authLogoutTestStore struct {
	AdministratorSessionStore

	current session.Session

	validateErr error
	revokeErr   error
	revoked     bool

	validateCalls int
	revokeCalls   int

	validatedToken string
	revokedToken   string
}

func (s *authLogoutTestStore) Validate(
	_ context.Context,
	token string,
) (session.Session, error) {
	s.validateCalls++
	s.validatedToken = token

	return s.current, s.validateErr
}

func (s *authLogoutTestStore) Revoke(
	_ context.Context,
	token string,
) (bool, error) {
	s.revokeCalls++
	s.revokedToken = token

	return s.revoked, s.revokeErr
}

func TestAuthLogoutHandler(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(
		make([]byte, session.TokenSize),
	)

	tests := []struct {
		name        string
		method      string
		scheme      string
		query       string
		body        string
		noCSRF      bool
		noCookie    bool
		validateErr error
		revokeErr   error
		revoked     bool
		expected    int
		validations int
		revocations int
	}{
		{
			name:        "successful logout",
			revoked:     true,
			expected:    http.StatusNoContent,
			validations: 1,
			revocations: 1,
		},
		{
			name:     "missing session",
			noCookie: true,
			expected: http.StatusUnauthorized,
		},
		{
			name:        "invalid session",
			validateErr: session.ErrInvalidSession,
			expected:    http.StatusUnauthorized,
			validations: 1,
		},
		{
			name:        "validation failure",
			validateErr: errors.New("private-validation-error"),
			expected:    http.StatusServiceUnavailable,
			validations: 1,
		},
		{
			name:        "revocation failure",
			revokeErr:   errors.New("private-revocation-error"),
			expected:    http.StatusServiceUnavailable,
			validations: 1,
			revocations: 1,
		},
		{
			name:        "session expired before revocation",
			revokeErr:   session.ErrInvalidSession,
			expected:    http.StatusUnauthorized,
			validations: 1,
			revocations: 1,
		},
		{
			name:        "session already revoked",
			expected:    http.StatusUnauthorized,
			validations: 1,
			revocations: 1,
		},
		{
			name:     "missing CSRF header",
			noCSRF:   true,
			expected: http.StatusForbidden,
		},
		{
			name:     "plaintext request",
			scheme:   "http",
			expected: http.StatusForbidden,
		},
		{
			name:     "GET request",
			method:   http.MethodGet,
			expected: http.StatusMethodNotAllowed,
		},
		{
			name:        "unexpected query parameters",
			query:       "?logout=true",
			expected:    http.StatusBadRequest,
			validations: 1,
		},
		{
			name:        "empty query marker",
			query:       "?",
			expected:    http.StatusBadRequest,
			validations: 1,
		},
		{
			name:        "unexpected request body",
			body:        `{}`,
			expected:    http.StatusBadRequest,
			validations: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &authLogoutTestStore{
				current: session.Session{
					ID:              "0123456789abcdef0123456789abcdef",
					AdministratorID: "12345678-1234-4000-8000-123456789abc",
				},
				validateErr: tt.validateErr,
				revokeErr:   tt.revokeErr,
				revoked:     tt.revoked,
			}

			api := &authAPIHandler{
				sessions: store,
			}

			handler := protectLocalManagement(
				api.logoutHandler(),
			)

			method := tt.method

			if method == "" {
				method = http.MethodPost
			}

			scheme := tt.scheme

			if scheme == "" {
				scheme = "https"
			}

			origin := scheme + "://127.0.0.1:8443"

			request := httptest.NewRequest(
				method,
				origin+authLogoutPath+tt.query,
				strings.NewReader(tt.body),
			)

			request.Header.Set(
				"Origin",
				origin,
			)

			if !tt.noCSRF {
				request.Header.Set(
					authCSRFHeaderName,
					authCSRFHeaderValue,
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

			if store.validateCalls != tt.validations {
				t.Fatalf(
					"expected %d validations, got %d",
					tt.validations,
					store.validateCalls,
				)
			}

			if store.revokeCalls != tt.revocations {
				t.Fatalf(
					"expected %d revocations, got %d",
					tt.revocations,
					store.revokeCalls,
				)
			}

			if store.validateCalls > 0 &&
				store.validatedToken != token {
				t.Fatal("incorrect token was validated")
			}

			if store.revokeCalls > 0 &&
				store.revokedToken != token {
				t.Fatal("incorrect token was revoked")
			}

			if recorder.Header().Get(
				"Cache-Control",
			) != "no-store" {
				t.Fatal("missing cache protection")
			}

			if strings.Contains(
				recorder.Body.String(),
				"private-",
			) {
				t.Fatal("internal error was exposed")
			}

			cookies := recorder.Result().Cookies()

			if tt.expected != http.StatusNoContent {
				if len(cookies) != 0 {
					t.Fatal("failed logout modified session cookie")
				}
				return
			}

			if recorder.Body.Len() != 0 {
				t.Fatal("successful logout returned a body")
			}

			if len(cookies) != 1 {
				t.Fatal("expected one deletion cookie")
			}

			cookie := cookies[0]

			if cookie.Name != sessionCookieName ||
				cookie.Value != "" ||
				cookie.MaxAge >= 0 ||
				cookie.Expires.IsZero() {
				t.Fatal("incorrect session cookie deletion")
			}

			if !cookie.Secure ||
				!cookie.HttpOnly ||
				cookie.SameSite != http.SameSiteStrictMode ||
				cookie.Path != "/" ||
				cookie.Domain != "" {
				t.Fatal("incorrect deletion cookie scope")
			}
		})
	}
}

func TestAuthLogoutMissingSessionStore(t *testing.T) {
	api := &authAPIHandler{}

	handler := protectLocalManagement(
		api.logoutHandler(),
	)

	const origin = "https://127.0.0.1:8443"

	request := httptest.NewRequest(
		http.MethodPost,
		origin+authLogoutPath,
		nil,
	)

	request.Header.Set("Origin", origin)

	request.Header.Set(
		authCSRFHeaderName,
		authCSRFHeaderValue,
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

	if recorder.Header().Get("Set-Cookie") != "" {
		t.Fatal("missing session store modified cookie")
	}
}

func TestValidateEmptyAuthMutation(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		unknownSize bool
		encoding    string
		expected    error
	}{
		{
			name: "empty request",
		},
		{
			name:        "unknown length empty body",
			unknownSize: true,
		},
		{
			name:     "unexpected body",
			body:     "{}",
			expected: errInvalidAuthMutationRequest,
		},
		{
			name:        "unknown length nonempty body",
			body:        "unexpected",
			unknownSize: true,
			expected:    errInvalidAuthMutationRequest,
		},
		{
			name:     "whitespace body",
			body:     " ",
			expected: errInvalidAuthMutationRequest,
		},
		{
			name:     "encoded body",
			encoding: "gzip",
			expected: errInvalidAuthMutationRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				"https://127.0.0.1"+authLogoutPath,
				strings.NewReader(tt.body),
			)

			if tt.unknownSize {
				request.ContentLength = -1
			}

			if tt.encoding != "" {
				request.Header.Set(
					"Content-Encoding",
					tt.encoding,
				)
			}

			err := validateEmptyAuthMutation(request)

			if !errors.Is(err, tt.expected) {
				t.Fatalf(
					"unexpected error: %v",
					err,
				)
			}
		})
	}
}
