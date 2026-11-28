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

type authActivityTestStore struct {
	AdministratorSessionStore

	previous session.Session
	current  session.Session

	validateErr error
	activityErr error

	validateCalls int
	activityCalls int

	validatedToken string
	activityToken  string
}

func (s *authActivityTestStore) Validate(
	_ context.Context,
	token string,
) (session.Session, error) {
	s.validateCalls++
	s.validatedToken = token

	return s.previous, s.validateErr
}

func (s *authActivityTestStore) RecordActivity(
	_ context.Context,
	token string,
) (session.Session, error) {
	s.activityCalls++
	s.activityToken = token

	return s.current, s.activityErr
}

func TestAuthActivityHandler(t *testing.T) {
	now := time.Date(
		2026, time.November, 27,
		12, 0, 0, 0, time.UTC,
	)

	previous := session.Session{
		ID:              "0123456789abcdef0123456789abcdef",
		AdministratorID: "12345678-1234-4000-8000-123456789abc",

		PasswordChangedAt: now.Add(-time.Hour),
		CreatedAt:         now,
		LastActivityAt:    now,
		ExpiresAt:         now.Add(session.AbsoluteLifetime),
	}

	token := base64.RawURLEncoding.EncodeToString(
		make([]byte, session.TokenSize),
	)

	tests := []struct {
		name        string
		method      string
		scheme      string
		query       string
		body        string
		noCookie    bool
		noCSRF      bool
		validateErr error
		activityErr error
		mutate      func(*session.Session)
		expected    int
		validations int
		activities  int
	}{
		{
			name:        "successful renewal",
			expected:    http.StatusOK,
			validations: 1,
			activities:  1,
		},
		{
			name:     "missing session cookie",
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
			name:        "validation infrastructure failure",
			validateErr: errors.New("private-validation-error"),
			expected:    http.StatusServiceUnavailable,
			validations: 1,
		},
		{
			name:        "session expired before renewal",
			activityErr: session.ErrInvalidSession,
			expected:    http.StatusUnauthorized,
			validations: 1,
			activities:  1,
		},
		{
			name:        "renewal infrastructure failure",
			activityErr: errors.New("private-renewal-error"),
			expected:    http.StatusServiceUnavailable,
			validations: 1,
			activities:  1,
		},
		{
			name:     "missing CSRF protection",
			noCSRF:   true,
			expected: http.StatusForbidden,
		},
		{
			name:     "plaintext request",
			scheme:   "http",
			expected: http.StatusForbidden,
		},
		{
			name:     "unsupported GET",
			method:   http.MethodGet,
			expected: http.StatusMethodNotAllowed,
		},
		{
			name:        "unexpected query parameters",
			query:       "?renew=true",
			expected:    http.StatusBadRequest,
			validations: 1,
		},
		{
			name:        "unexpected request body",
			body:        `{}`,
			expected:    http.StatusBadRequest,
			validations: 1,
		},
		{
			name: "session identity changed",
			mutate: func(s *session.Session) {
				s.ID = "unexpected-session"
			},
			expected:    http.StatusServiceUnavailable,
			validations: 1,
			activities:  1,
		},
		{
			name: "administrator identity changed",
			mutate: func(s *session.Session) {
				s.AdministratorID =
					"87654321-4321-4000-8000-123456789abc"
			},
			expected:    http.StatusServiceUnavailable,
			validations: 1,
			activities:  1,
		},
		{
			name: "credential revision changed",
			mutate: func(s *session.Session) {
				s.PasswordChangedAt =
					s.PasswordChangedAt.Add(time.Second)
			},
			expected:    http.StatusServiceUnavailable,
			validations: 1,
			activities:  1,
		},
		{
			name: "creation timestamp changed",
			mutate: func(s *session.Session) {
				s.CreatedAt = s.CreatedAt.Add(time.Second)
			},
			expected:    http.StatusServiceUnavailable,
			validations: 1,
			activities:  1,
		},
		{
			name: "absolute expiration extended",
			mutate: func(s *session.Session) {
				s.ExpiresAt = s.ExpiresAt.Add(time.Hour)
			},
			expected:    http.StatusServiceUnavailable,
			validations: 1,
			activities:  1,
		},
		{
			name: "activity timestamp moved backward",
			mutate: func(s *session.Session) {
				s.LastActivityAt =
					previous.LastActivityAt.Add(-time.Second)
			},
			expected:    http.StatusServiceUnavailable,
			validations: 1,
			activities:  1,
		},
		{
			name: "activity reached absolute expiration",
			mutate: func(s *session.Session) {
				s.LastActivityAt = s.ExpiresAt
			},
			expected:    http.StatusServiceUnavailable,
			validations: 1,
			activities:  1,
		},
		{
			name: "same millisecond renewal",
			mutate: func(s *session.Session) {
				s.LastActivityAt = previous.LastActivityAt
			},
			expected:    http.StatusOK,
			validations: 1,
			activities:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := previous

			current.LastActivityAt =
				now.Add(10 * time.Minute)

			if tt.mutate != nil {
				tt.mutate(&current)
			}

			store := &authActivityTestStore{
				previous:    previous,
				current:     current,
				validateErr: tt.validateErr,
				activityErr: tt.activityErr,
			}

			api := &authAPIHandler{
				sessions: store,
			}

			handler := protectLocalManagement(
				api.activityHandler(),
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
				origin+authActivityPath+tt.query,
				strings.NewReader(tt.body),
			)

			request.Header.Set("Origin", origin)

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

			handler.ServeHTTP(recorder, request)

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

			if store.activityCalls != tt.activities {
				t.Fatalf(
					"expected %d activity operations, got %d",
					tt.activities,
					store.activityCalls,
				)
			}

			if store.validateCalls > 0 &&
				store.validatedToken != token {
				t.Fatal("incorrect token was validated")
			}

			if store.activityCalls > 0 &&
				store.activityToken != token {
				t.Fatal("incorrect token was renewed")
			}

			if recorder.Header().Get(
				"Cache-Control",
			) != "no-store" {
				t.Fatal("missing cache protection")
			}

			if recorder.Header().Get(
				"Set-Cookie",
			) != "" {
				t.Fatal("activity renewal modified cookie")
			}

			if strings.Contains(
				recorder.Body.String(),
				"private-",
			) {
				t.Fatal("internal error was exposed")
			}

			if tt.expected != http.StatusOK {
				return
			}

			var response authSessionResponse

			if err := json.Unmarshal(
				recorder.Body.Bytes(),
				&response,
			); err != nil {
				t.Fatal(err)
			}

			expected := toAuthSessionResponse(current)

			if response != expected {
				t.Fatal("incorrect renewed session response")
			}

			for _, forbidden := range []string{
				token,
				current.ID,
				"password",
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

func TestAuthActivityMissingStore(t *testing.T) {
	api := &authAPIHandler{}

	handler := protectLocalManagement(
		api.activityHandler(),
	)

	const origin = "https://127.0.0.1:8443"

	request := httptest.NewRequest(
		http.MethodPost,
		origin+authActivityPath,
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
}
