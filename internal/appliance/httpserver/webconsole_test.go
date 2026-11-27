package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/webconsole"
)

func TestWebConsolePreservesManagementRoutes(
	t *testing.T,
) {
	management := http.HandlerFunc(
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			w.Header().Set(
				"X-Test-Handler",
				"management",
			)

			http.NotFound(w, r)
		},
	)

	console := http.HandlerFunc(
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			w.Header().Set(
				"X-Test-Handler",
				"console",
			)

			w.WriteHeader(http.StatusOK)
		},
	)

	handler := mountWebConsole(
		management,
		console,
	)

	tests := []struct {
		path        string
		wantHandler string
	}{
		{"/", "console"},
		{"/devices", "console"},
		{"/system", "console"},
		{"/assets/index-abcdefgh.js", "console"},
		{"/api", "management"},
		{"/api/v1/version", "management"},
		{"/api/v1/devices", "management"},
		{"/api/v1/unknown", "management"},
		{"/healthz", "management"},
		{"/healthz/unknown", "management"},
		{"/readyz", "management"},
		{"/readyz/unknown", "management"},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				"http://127.0.0.1:8080"+tc.path,
				nil,
			)

			request.Header.Set(
				"Accept",
				"text/html",
			)

			response := httptest.NewRecorder()

			handler.ServeHTTP(
				response,
				request,
			)

			if got := response.Header().Get(
				"X-Test-Handler",
			); got != tc.wantHandler {
				t.Errorf(
					"handler = %q, want %q; status = %d",
					got,
					tc.wantHandler,
					response.Code,
				)
			}
		})
	}
}

func TestWebConsolePreservesLocalSecurity(
	t *testing.T,
) {
	management := http.NotFoundHandler()

	console := http.HandlerFunc(
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			w.WriteHeader(http.StatusOK)
		},
	)

	handler := mountWebConsole(
		management,
		console,
	)

	tests := []struct {
		name       string
		host       string
		origin     string
		fetchSite  string
		wantStatus int
	}{
		{
			name:       "local navigation",
			host:       "127.0.0.1:8080",
			wantStatus: http.StatusOK,
		},
		{
			name:       "same origin",
			host:       "127.0.0.1:8080",
			origin:     "http://127.0.0.1:8080",
			fetchSite:  "same-origin",
			wantStatus: http.StatusOK,
		},
		{
			name:       "non loopback host",
			host:       "example.com",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "foreign origin",
			host:       "127.0.0.1:8080",
			origin:     "https://example.com",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "cross site request",
			host:       "127.0.0.1:8080",
			fetchSite:  "cross-site",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				"http://127.0.0.1:8080/",
				nil,
			)

			request.Host = tc.host

			if tc.origin != "" {
				request.Header.Set(
					"Origin",
					tc.origin,
				)
			}

			if tc.fetchSite != "" {
				request.Header.Set(
					"Sec-Fetch-Site",
					tc.fetchSite,
				)
			}

			response := httptest.NewRecorder()

			handler.ServeHTTP(
				response,
				request,
			)

			if response.Code != tc.wantStatus {
				t.Errorf(
					"status = %d, want %d",
					response.Code,
					tc.wantStatus,
				)
			}
		})
	}
}

func TestEnableWebConsole(t *testing.T) {
	assets, err := webconsole.Assets()
	if err != nil {
		t.Fatal(err)
	}

	server := New(
		"127.0.0.1:8080",
		"test-version",
		func(context.Context) error {
			return nil
		},
	)

	enabled, err := EnableWebConsole(server)
	if err != nil {
		t.Fatal(err)
	}

	if enabled != (assets != nil) {
		t.Fatal(
			"console availability does not match build configuration",
		)
	}

	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{
			path:       "/healthz",
			wantStatus: http.StatusOK,
			wantBody:   "healthy",
		},
		{
			path:       "/readyz",
			wantStatus: http.StatusOK,
			wantBody:   "ready",
		},
		{
			path:       "/api/v1/version",
			wantStatus: http.StatusOK,
			wantBody:   "test-version",
		},
		{
			path:       "/api/v1/unknown",
			wantStatus: http.StatusNotFound,
		},
	}

	if enabled {
		tests = append(
			tests,
			struct {
				path       string
				wantStatus int
				wantBody   string
			}{
				path:       "/",
				wantStatus: http.StatusOK,
				wantBody:   `id="root"`,
			},
		)
	} else {
		tests = append(
			tests,
			struct {
				path       string
				wantStatus int
				wantBody   string
			}{
				path:       "/",
				wantStatus: http.StatusNotFound,
			},
		)
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				"http://127.0.0.1:8080"+tc.path,
				nil,
			)

			request.Header.Set(
				"Accept",
				"text/html",
			)

			response := httptest.NewRecorder()

			server.Handler.ServeHTTP(
				response,
				request,
			)

			if response.Code != tc.wantStatus {
				t.Fatalf(
					"status = %d, want %d",
					response.Code,
					tc.wantStatus,
				)
			}

			if tc.wantBody != "" &&
				!strings.Contains(
					response.Body.String(),
					tc.wantBody,
				) {
				t.Fatal("unexpected response body")
			}
		})
	}
}
