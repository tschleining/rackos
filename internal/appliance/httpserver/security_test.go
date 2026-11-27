package httpserver

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalManagementSecurity(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		origins []string
		sites   []string
		status  int
	}{
		{
			name:   "local CLI",
			host:   "127.0.0.1:8080",
			status: http.StatusOK,
		},
		{
			name:    "same origin",
			host:    "127.0.0.1:8080",
			origins: []string{"http://127.0.0.1:8080"},
			sites:   []string{"same-origin"},
			status:  http.StatusOK,
		},
		{
			name:   "direct browser navigation",
			host:   "127.0.0.1:8080",
			sites:  []string{"none"},
			status: http.StatusOK,
		},
		{
			name:   "IPv6",
			host:   "[::1]:8080",
			status: http.StatusOK,
		},
		{
			name:    "IPv6 origin",
			host:    "[::1]:8080",
			origins: []string{"http://[::1]:8080"},
			status:  http.StatusOK,
		},
		{
			name:    "default port",
			host:    "127.0.0.1:80",
			origins: []string{"http://127.0.0.1"},
			status:  http.StatusOK,
		},
		{
			name:   "implicit default port",
			host:   "127.0.0.1",
			status: http.StatusOK,
		},
		{
			name:   "external hostname",
			host:   "external.example:8080",
			status: http.StatusForbidden,
		},
		{
			name:   "localhost hostname",
			host:   "localhost:8080",
			status: http.StatusForbidden,
		},
		{
			name:   "nonloopback address",
			host:   "192.168.1.10:8080",
			status: http.StatusForbidden,
		},
		{
			name:   "invalid port",
			host:   "127.0.0.1:65536",
			status: http.StatusForbidden,
		},
		{
			name:   "empty port",
			host:   "127.0.0.1:",
			status: http.StatusForbidden,
		},
		{
			name:   "zero port",
			host:   "127.0.0.1:0",
			status: http.StatusForbidden,
		},
		{
			name:   "invalid authority",
			host:   "127.0.0.1@external.example:8080",
			status: http.StatusForbidden,
		},
		{
			name:    "external origin",
			host:    "127.0.0.1:8080",
			origins: []string{"https://external.example"},
			status:  http.StatusForbidden,
		},
		{
			name:    "different local port",
			host:    "127.0.0.1:8080",
			origins: []string{"http://127.0.0.1:5173"},
			status:  http.StatusForbidden,
		},
		{
			name:    "different scheme",
			host:    "127.0.0.1:8080",
			origins: []string{"https://127.0.0.1:8080"},
			status:  http.StatusForbidden,
		},
		{
			name:    "null origin",
			host:    "127.0.0.1:8080",
			origins: []string{"null"},
			status:  http.StatusForbidden,
		},
		{
			name:    "empty origin",
			host:    "127.0.0.1:8080",
			origins: []string{""},
			status:  http.StatusForbidden,
		},
		{
			name: "duplicate origin",
			host: "127.0.0.1:8080",
			origins: []string{
				"http://127.0.0.1:8080",
				"http://127.0.0.1:8080",
			},
			status: http.StatusForbidden,
		},
		{
			name:   "cross-site",
			host:   "127.0.0.1:8080",
			sites:  []string{"cross-site"},
			status: http.StatusForbidden,
		},
		{
			name:   "same-site different origin",
			host:   "127.0.0.1:8080",
			sites:  []string{"same-site"},
			status: http.StatusForbidden,
		},
		{
			name:   "unknown metadata",
			host:   "127.0.0.1:8080",
			sites:  []string{"invalid"},
			status: http.StatusForbidden,
		},
		{
			name:   "duplicate metadata",
			host:   "127.0.0.1:8080",
			sites:  []string{"same-origin", "cross-site"},
			status: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0

			handler := protectLocalManagement(
				http.HandlerFunc(func(
					w http.ResponseWriter,
					r *http.Request,
				) {
					calls++
					w.WriteHeader(http.StatusOK)
				}),
			)

			request := httptest.NewRequest(
				http.MethodGet,
				"/api/v1/devices",
				nil,
			)

			request.Host = tt.host

			for _, origin := range tt.origins {
				request.Header.Add("Origin", origin)
			}

			for _, site := range tt.sites {
				request.Header.Add(
					"Sec-Fetch-Site",
					site,
				)
			}

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.status {
				t.Fatalf(
					"expected %d, got %d",
					tt.status,
					recorder.Code,
				)
			}

			if tt.status == http.StatusOK {
				if calls != 1 {
					t.Fatal("valid request was not forwarded")
				}
			} else if calls != 0 {
				t.Fatal("blocked request reached API")
			}

			headers := map[string]string{
				"Cache-Control":                "no-store",
				"X-Content-Type-Options":       "nosniff",
				"X-Frame-Options":              "DENY",
				"Referrer-Policy":              "no-referrer",
				"Cross-Origin-Resource-Policy": "same-origin",
			}

			for name, expected := range headers {
				if recorder.Header().Get(name) != expected {
					t.Errorf(
						"incorrect %s header",
						name,
					)
				}
			}

			if recorder.Header().Get(
				"Access-Control-Allow-Origin",
			) != "" {
				t.Fatal("unexpected CORS permission")
			}
		})
	}
}

func TestLocalManagementSecurityListenerAuthority(
	t *testing.T,
) {
	tests := []struct {
		host   string
		status int
	}{
		{"127.0.0.1:8080", http.StatusOK},
		{"127.0.0.1:8081", http.StatusForbidden},
		{"127.0.0.2:8080", http.StatusForbidden},
		{"[::1]:8080", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				"/healthz",
				nil,
			)

			request.Host = tt.host

			ctx := context.WithValue(
				request.Context(),
				http.LocalAddrContextKey,
				&net.TCPAddr{
					IP:   net.ParseIP("127.0.0.1"),
					Port: 8080,
				},
			)

			recorder := httptest.NewRecorder()

			handler := protectLocalManagement(
				http.HandlerFunc(func(
					w http.ResponseWriter,
					r *http.Request,
				) {
					w.WriteHeader(http.StatusOK)
				}),
			)

			handler.ServeHTTP(
				recorder,
				request.WithContext(ctx),
			)

			if recorder.Code != tt.status {
				t.Fatalf(
					"expected %d, got %d",
					tt.status,
					recorder.Code,
				)
			}
		})
	}
}

func TestLocalManagementSecurityServerIntegration(
	t *testing.T,
) {
	server := New(
		"127.0.0.1:0",
		"0.1.0-test",
		alwaysReady,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/healthz",
		nil,
	)

	request.Host = "external.example"

	recorder := httptest.NewRecorder()

	server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatal("HTTP server did not apply security")
	}

	request = httptest.NewRequest(
		http.MethodGet,
		"/healthz",
		nil,
	)

	request.Host = "127.0.0.1:8080"

	recorder = httptest.NewRecorder()

	server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatal("valid management request rejected")
	}
}
