package httpserver

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthMutationCSRF(t *testing.T) {
	const (
		address = "127.0.0.1:8443"
		origin  = "https://" + address
		path    = "/api/v1/auth/login"
	)

	tests := []struct {
		name       string
		method     string
		scheme     string
		host       string
		origins    []string
		headers    []string
		fetchSites []string
		forwarded  string
		expected   int
	}{
		{
			name:     "valid same-origin request",
			origins:  []string{origin},
			headers:  []string{"1"},
			expected: http.StatusNoContent,
		},
		{
			name:       "valid fetch metadata",
			origins:    []string{origin},
			headers:    []string{"1"},
			fetchSites: []string{"same-origin"},
			expected:   http.StatusNoContent,
		},
		{
			name:     "missing origin",
			headers:  []string{"1"},
			expected: http.StatusForbidden,
		},
		{
			name:     "empty origin",
			origins:  []string{""},
			headers:  []string{"1"},
			expected: http.StatusForbidden,
		},
		{
			name:     "null origin",
			origins:  []string{"null"},
			headers:  []string{"1"},
			expected: http.StatusForbidden,
		},
		{
			name:     "cross-origin request",
			origins:  []string{"https://example.com"},
			headers:  []string{"1"},
			expected: http.StatusForbidden,
		},
		{
			name:     "different origin port",
			origins:  []string{"https://127.0.0.1:9443"},
			headers:  []string{"1"},
			expected: http.StatusForbidden,
		},
		{
			name:     "different origin scheme",
			origins:  []string{"http://" + address},
			headers:  []string{"1"},
			expected: http.StatusForbidden,
		},
		{
			name:     "duplicate origins",
			origins:  []string{origin, origin},
			headers:  []string{"1"},
			expected: http.StatusForbidden,
		},
		{
			name:     "combined origins",
			origins:  []string{origin + ", " + origin},
			headers:  []string{"1"},
			expected: http.StatusForbidden,
		},
		{
			name:     "missing CSRF header",
			origins:  []string{origin},
			expected: http.StatusForbidden,
		},
		{
			name:     "empty CSRF header",
			origins:  []string{origin},
			headers:  []string{""},
			expected: http.StatusForbidden,
		},
		{
			name:     "invalid CSRF header",
			origins:  []string{origin},
			headers:  []string{"true"},
			expected: http.StatusForbidden,
		},
		{
			name:     "duplicate CSRF headers",
			origins:  []string{origin},
			headers:  []string{"1", "1"},
			expected: http.StatusForbidden,
		},
		{
			name:     "combined CSRF header",
			origins:  []string{origin},
			headers:  []string{"1, 1"},
			expected: http.StatusForbidden,
		},
		{
			name:     "noncanonical header value",
			origins:  []string{origin},
			headers:  []string{"01"},
			expected: http.StatusForbidden,
		},
		{
			name:     "whitespace in header value",
			origins:  []string{origin},
			headers:  []string{" 1 "},
			expected: http.StatusForbidden,
		},
		{
			name:       "cross-site fetch metadata",
			origins:    []string{origin},
			headers:    []string{"1"},
			fetchSites: []string{"cross-site"},
			expected:   http.StatusForbidden,
		},
		{
			name:       "same-site cross-origin metadata",
			origins:    []string{origin},
			headers:    []string{"1"},
			fetchSites: []string{"same-site"},
			expected:   http.StatusForbidden,
		},
		{
			name:       "duplicate fetch metadata",
			origins:    []string{origin},
			headers:    []string{"1"},
			fetchSites: []string{"same-origin", "same-origin"},
			expected:   http.StatusForbidden,
		},
		{
			name:     "plaintext request",
			scheme:   "http",
			origins:  []string{"http://" + address},
			headers:  []string{"1"},
			expected: http.StatusForbidden,
		},
		{
			name:      "spoofed forwarded HTTPS",
			scheme:    "http",
			origins:   []string{origin},
			headers:   []string{"1"},
			forwarded: "https",
			expected:  http.StatusForbidden,
		},
		{
			name:     "nonlocal host",
			host:     "example.com",
			origins:  []string{"https://example.com"},
			headers:  []string{"1"},
			expected: http.StatusForbidden,
		},
		{
			name:     "DNS rebinding host",
			host:     "attacker.example",
			origins:  []string{"https://attacker.example"},
			headers:  []string{"1"},
			expected: http.StatusForbidden,
		},
		{
			name:     "GET request",
			method:   http.MethodGet,
			origins:  []string{origin},
			headers:  []string{"1"},
			expected: http.StatusMethodNotAllowed,
		},
		{
			name:     "HEAD request",
			method:   http.MethodHead,
			origins:  []string{origin},
			headers:  []string{"1"},
			expected: http.StatusMethodNotAllowed,
		},
		{
			name:     "OPTIONS preflight",
			method:   http.MethodOptions,
			origins:  []string{origin},
			headers:  []string{"1"},
			expected: http.StatusMethodNotAllowed,
		},
		{
			name:     "PUT request",
			method:   http.MethodPut,
			origins:  []string{origin},
			headers:  []string{"1"},
			expected: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := tt.method
			if method == "" {
				method = http.MethodPost
			}

			scheme := tt.scheme
			if scheme == "" {
				scheme = "https"
			}

			request := httptest.NewRequest(
				method,
				scheme+"://"+address+path,
				nil,
			)

			if tt.host != "" {
				request.Host = tt.host
			}

			for _, value := range tt.origins {
				request.Header.Add("Origin", value)
			}

			for _, value := range tt.headers {
				request.Header.Add(
					authCSRFHeaderName,
					value,
				)
			}

			for _, value := range tt.fetchSites {
				request.Header.Add(
					"Sec-Fetch-Site",
					value,
				)
			}

			if tt.forwarded != "" {
				request.Header.Set(
					"X-Forwarded-Proto",
					tt.forwarded,
				)
			}

			calls := 0

			handler := requireAuthMutationCSRF(
				http.HandlerFunc(func(
					w http.ResponseWriter,
					r *http.Request,
				) {
					calls++
					w.WriteHeader(
						http.StatusNoContent,
					)
				}),
			)

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.expected {
				t.Fatalf(
					"expected status %d, got %d: %s",
					tt.expected,
					recorder.Code,
					recorder.Body.String(),
				)
			}

			expectedCalls := 0
			if tt.expected == http.StatusNoContent {
				expectedCalls = 1
			}

			if calls != expectedCalls {
				t.Fatalf(
					"expected %d handler calls, got %d",
					expectedCalls,
					calls,
				)
			}

			if recorder.Header().Get(
				"Cache-Control",
			) != "no-store" {
				t.Fatal("missing cache protection")
			}

			if recorder.Header().Get(
				"Access-Control-Allow-Origin",
			) != "" {
				t.Fatal("unexpected CORS authorization")
			}

			if recorder.Header().Get(
				"Set-Cookie",
			) != "" {
				t.Fatal("CSRF middleware modified cookies")
			}

			if tt.expected ==
				http.StatusMethodNotAllowed {
				if recorder.Header().Get(
					"Allow",
				) != http.MethodPost {
					t.Fatal("incorrect Allow header")
				}
			}
		})
	}
}

func TestAuthMutationCSRFIPv6(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"https://[::1]:8443/api/v1/auth/login",
		nil,
	)

	request.Header.Set(
		"Origin",
		"https://[::1]:8443",
	)

	request.Header.Set(
		authCSRFHeaderName,
		authCSRFHeaderValue,
	)

	handler := requireAuthMutationCSRF(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf(
			"IPv6 request rejected: %d",
			recorder.Code,
		)
	}
}

func TestAuthMutationCSRFSecurityComposition(
	t *testing.T,
) {
	calls := 0

	handler := protectLocalManagement(
		requireAuthMutationCSRF(
			http.HandlerFunc(func(
				w http.ResponseWriter,
				r *http.Request,
			) {
				calls++
				w.WriteHeader(
					http.StatusNoContent,
				)
			}),
		),
	)

	for _, path := range []string{
		"/api/v1/auth/login",
		"/api/v1/auth/logout",
		"/api/v1/auth/activity",
	} {
		request := httptest.NewRequest(
			http.MethodPost,
			"https://127.0.0.1:8443"+path,
			nil,
		)

		request.Header.Set(
			"Origin",
			"https://127.0.0.1:8443",
		)

		request.Header.Set(
			authCSRFHeaderName,
			authCSRFHeaderValue,
		)

		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusNoContent {
			t.Fatalf(
				"%s rejected: %d",
				path,
				recorder.Code,
			)
		}
	}

	if calls != 3 {
		t.Fatal("unexpected protected handler count")
	}
}

func TestAuthMutationCSRFLocalAuthorityMismatch(
	t *testing.T,
) {
	request := httptest.NewRequest(
		http.MethodPost,
		"https://127.0.0.1:8443/api/v1/auth/login",
		nil,
	)

	request.Header.Set(
		"Origin",
		"https://127.0.0.1:8443",
	)

	request.Header.Set(
		authCSRFHeaderName,
		authCSRFHeaderValue,
	)

	// Simulate a connection whose actual local
	// authority differs from the supplied Host.
	request = request.WithContext(
		withAuthCSRFLocalAddress(
			request,
			&net.TCPAddr{
				IP:   net.ParseIP("127.0.0.1"),
				Port: 9443,
			},
		),
	)

	handler := requireAuthMutationCSRF(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			t.Fatal("authority mismatch reached handler")
		}),
	)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf(
			"expected 403, got %d",
			recorder.Code,
		)
	}
}

func TestAuthMutationCSRFErrorRedaction(t *testing.T) {
	const untrusted = "untrusted-origin-marker"

	request := httptest.NewRequest(
		http.MethodPost,
		"https://127.0.0.1:8443/api/v1/auth/login",
		nil,
	)

	request.Header.Set("Origin", untrusted)

	request.Header.Set(
		authCSRFHeaderName,
		untrusted,
	)

	handler := requireAuthMutationCSRF(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			t.Fatal("invalid request reached handler")
		}),
	)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatal("invalid request was not rejected")
	}

	if strings.Contains(
		recorder.Body.String(),
		untrusted,
	) {
		t.Fatal("response reflected untrusted input")
	}
}
func withAuthCSRFLocalAddress(
	r *http.Request,
	address *net.TCPAddr,
) context.Context {
	return context.WithValue(
		r.Context(),
		http.LocalAddrContextKey,
		address,
	)
}
