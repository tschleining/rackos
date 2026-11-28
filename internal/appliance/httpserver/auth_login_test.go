package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/auth"
	"github.com/tschleining/rackos/internal/appliance/session"
)

func TestAuthLoginFailureResponses(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected int
		message  string
	}{
		{
			"invalid credentials",
			errInvalidAdministratorCredentials,
			http.StatusUnauthorized,
			"invalid administrator credentials",
		},
		{
			"credentials changed",
			session.ErrCredentialsChanged,
			http.StatusUnauthorized,
			"invalid administrator credentials",
		},
		{
			"password hashing busy",
			auth.ErrPasswordHashBusy,
			http.StatusTooManyRequests,
			"too many authentication attempts",
		},
		{
			"session capacity exhausted",
			session.ErrSessionLimit,
			http.StatusConflict,
			"administrator session limit reached",
		},
		{
			"database failure",
			errors.New("private-database-error"),
			http.StatusServiceUnavailable,
			"authentication service unavailable",
		},
		{
			"operation canceled",
			context.Canceled,
			http.StatusServiceUnavailable,
			"authentication service unavailable",
		},
		{
			"operation timed out",
			context.DeadlineExceeded,
			http.StatusServiceUnavailable,
			"authentication service unavailable",
		},
		{
			"invalid authentication record",
			errInvalidAuthenticationRecord,
			http.StatusServiceUnavailable,
			"authentication service unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()

			writeAuthLoginFailure(
				recorder,
				tt.err,
			)

			if recorder.Code != tt.expected {
				t.Fatalf(
					"expected %d, got %d",
					tt.expected,
					recorder.Code,
				)
			}

			if !strings.Contains(
				recorder.Body.String(),
				tt.message,
			) {
				t.Fatal("incorrect public error message")
			}

			if strings.Contains(
				recorder.Body.String(),
				"private-database-error",
			) {
				t.Fatal("internal error was exposed")
			}

			if recorder.Header().Get(
				"Set-Cookie",
			) != "" {
				t.Fatal("failed login issued a cookie")
			}

			if recorder.Header().Get(
				"Cache-Control",
			) != "no-store" {
				t.Fatal("missing cache protection")
			}

			if tt.expected == http.StatusTooManyRequests {
				if recorder.Header().Get(
					"Retry-After",
				) != "1" {
					t.Fatal("incorrect Retry-After header")
				}
			}
		})
	}
}

func TestAuthLoginRequestErrors(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
		expected    int
	}{
		{
			"invalid JSON",
			`{"username":`,
			"application/json",
			http.StatusBadRequest,
		},
		{
			"missing password",
			`{"username":"admin"}`,
			"application/json",
			http.StatusBadRequest,
		},
		{
			"unsupported media type",
			"username=admin&password=test",
			"application/x-www-form-urlencoded",
			http.StatusUnsupportedMediaType,
		},
		{
			"oversized request",
			strings.Repeat(
				" ",
				int(maxLoginRequestBytes)+1,
			),
			"application/json",
			http.StatusRequestEntityTooLarge,
		},
		{
			"missing authentication dependencies",
			`{"username":"admin","password":"test"}`,
			"application/json",
			http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &authAPIHandler{}

			handler := protectLocalManagement(
				api.loginHandler(
					newLoginRateLimiter(),
				),
			)

			const origin = "https://127.0.0.1:8443"

			request := httptest.NewRequest(
				http.MethodPost,
				origin+authLoginPath,
				strings.NewReader(tt.body),
			)

			request.Header.Set("Origin", origin)

			request.Header.Set(
				authCSRFHeaderName,
				authCSRFHeaderValue,
			)

			request.Header.Set(
				"Content-Type",
				tt.contentType,
			)

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.expected {
				t.Fatalf(
					"expected %d, got %d",
					tt.expected,
					recorder.Code,
				)
			}

			if recorder.Header().Get(
				"Set-Cookie",
			) != "" {
				t.Fatal("failed login issued a cookie")
			}

			if recorder.Header().Get(
				"Cache-Control",
			) != "no-store" {
				t.Fatal("missing cache protection")
			}
		})
	}
}
