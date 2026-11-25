package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestEndpoints(t *testing.T) {
	handler := NewHandler("0.1.0-test")

	tests := []struct {
		name string
		path string
		want map[string]string
	}{
		{
			name: "health",
			path: "/healthz",
			want: map[string]string{
				"status": "healthy",
			},
		},
		{
			name: "readiness",
			path: "/readyz",
			want: map[string]string{
				"status": "ready",
			},
		},
		{
			name: "version",
			path: "/api/v1/version",
			want: map[string]string{
				"name":    "RackOS",
				"version": "0.1.0-test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodGet,
				tt.path,
				nil,
			)

			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rec.Code)
			}

			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("unexpected content type: %s", got)
			}

			var got map[string]string

			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestNotFound(t *testing.T) {
	handler := NewHandler("0.1.0-test")

	req := httptest.NewRequest(
		http.MethodGet,
		"/unknown",
		nil,
	)

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	handler := NewHandler("0.1.0-test")

	req := httptest.NewRequest(
		http.MethodPost,
		"/healthz",
		nil,
	)

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}
