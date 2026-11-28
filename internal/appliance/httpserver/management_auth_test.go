package httpserver

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/session"
)

type managementAuthTestValidator struct {
	current session.Session
	err     error
	calls   int
}

func (v *managementAuthTestValidator) Validate(
	_ context.Context,
	_ string,
) (session.Session, error) {
	v.calls++

	return v.current, v.err
}

func TestAuthenticatedManagementPublicEndpoints(
	t *testing.T,
) {
	validator := &managementAuthTestValidator{
		err: session.ErrInvalidSession,
	}

	handler := NewAuthenticatedManagementHandler(
		"0.1.0-test",
		alwaysReady,
		nil,
		nil,
		nil,
		validator,
	)

	for _, method := range []string{
		http.MethodGet,
		http.MethodHead,
	} {
		for _, path := range []string{
			"/healthz",
			"/readyz",
			"/api/v1/version",
		} {
			t.Run(method+" "+path, func(t *testing.T) {
				request := httptest.NewRequest(
					method,
					"http://127.0.0.1:8080"+path,
					nil,
				)

				recorder := httptest.NewRecorder()

				handler.ServeHTTP(recorder, request)

				if recorder.Code != http.StatusOK {
					t.Fatalf(
						"expected 200, got %d",
						recorder.Code,
					)
				}
			})
		}
	}

	if validator.calls != 0 {
		t.Fatal("public requests validated a session")
	}
}

func TestAuthenticatedManagementProtectedEndpoints(
	t *testing.T,
) {
	deviceID := testPerformanceHTTPDeviceID

	record := testHTTPInventoryRecord()
	record.DeviceID = deviceID

	inventoryReader := &stubInventoryReader{
		record: record,
	}

	token := base64.RawURLEncoding.EncodeToString(
		make([]byte, session.TokenSize),
	)

	validator := &managementAuthTestValidator{
		current: session.Session{
			ID:              "0123456789abcdef0123456789abcdef",
			AdministratorID: "12345678-1234-4000-8000-123456789abc",
		},
	}

	handler := NewAuthenticatedManagementHandler(
		"0.1.0-test",
		alwaysReady,
		testManagementDeviceReader(),
		testManagementPerformanceReader(),
		inventoryReader,
		validator,
	)

	paths := []string{
		"/api/v1/devices",
		"/api/v1/devices/summary",
		"/api/v1/devices/" + deviceID,
		"/api/v1/devices/" + deviceID + "/inventory",
		"/api/v1/devices/" + deviceID + "/performance/latest",
		"/api/v1/devices/" + deviceID + "/performance/history",
	}

	for _, method := range []string{
		http.MethodGet,
		http.MethodHead,
	} {
		for _, path := range paths {
			t.Run(method+" "+path, func(t *testing.T) {
				tests := []struct {
					name   string
					scheme string
					cookie bool
					err    error
					status int
					calls  int
				}{
					{
						name:   "missing session",
						scheme: "https",
						status: http.StatusUnauthorized,
					},
					{
						name:   "plaintext connection",
						scheme: "http",
						cookie: true,
						status: http.StatusForbidden,
					},
					{
						name:   "invalid session",
						scheme: "https",
						cookie: true,
						err:    session.ErrInvalidSession,
						status: http.StatusUnauthorized,
						calls:  1,
					},
					{
						name:   "valid session",
						scheme: "https",
						cookie: true,
						status: http.StatusOK,
						calls:  1,
					},
				}

				for _, tt := range tests {
					t.Run(tt.name, func(t *testing.T) {
						validator.calls = 0
						validator.err = tt.err

						request := httptest.NewRequest(
							method,
							tt.scheme+"://127.0.0.1:8080"+path,
							nil,
						)

						if tt.cookie {
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

						if recorder.Code != tt.status {
							t.Fatalf(
								"expected %d, got %d: %s",
								tt.status,
								recorder.Code,
								recorder.Body.String(),
							)
						}

						if validator.calls != tt.calls {
							t.Fatalf(
								"expected %d validation calls, got %d",
								tt.calls,
								validator.calls,
							)
						}

						if recorder.Header().Get(
							"Cache-Control",
						) != "no-store" {
							t.Fatal("missing cache protection")
						}
					})
				}
			})
		}
	}
}

func TestAuthenticatedManagementDefaultProtection(
	t *testing.T,
) {
	handler := NewAuthenticatedManagementHandler(
		"0.1.0-test",
		alwaysReady,
		nil,
		nil,
		nil,
		nil,
	)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/future"},
		{http.MethodGet, "/healthz/"},
		{http.MethodGet, "/readyz/"},
		{http.MethodGet, "/api/v1/version/"},
		{http.MethodGet, "/%68ealthz"},
		{http.MethodGet, "/api/v1/%76ersion"},
		{http.MethodGet, "//healthz"},
		{http.MethodPost, "/healthz"},
		{http.MethodPost, "/readyz"},
		{http.MethodPost, "/api/v1/version"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			request := httptest.NewRequest(
				tt.method,
				"https://127.0.0.1:8080"+tt.path,
				nil,
			)

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf(
					"expected 401, got %d",
					recorder.Code,
				)
			}
		})
	}
}

func TestAuthenticatedManagementSecurityOrder(
	t *testing.T,
) {
	validator := &managementAuthTestValidator{}

	handler := NewAuthenticatedManagementHandler(
		"0.1.0-test",
		alwaysReady,
		nil,
		nil,
		nil,
		validator,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"https://127.0.0.1:8080/api/v1/devices",
		nil,
	)

	request.Header.Set(
		"Origin",
		"https://example.com",
	)

	request.AddCookie(&http.Cookie{
		Name: sessionCookieName,
		Value: base64.RawURLEncoding.EncodeToString(
			make([]byte, session.TokenSize),
		),
	})

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf(
			"expected 403, got %d",
			recorder.Code,
		)
	}

	if validator.calls != 0 {
		t.Fatal("forbidden request reached authentication")
	}
}
