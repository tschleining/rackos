package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestEndpoints(t *testing.T) {
	handler := NewHandler("0.1.0-test", alwaysReady)

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
	handler := NewHandler("0.1.0-test", alwaysReady)

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
	handler := NewHandler("0.1.0-test", alwaysReady)

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

func alwaysReady(ctx context.Context) error {
	return nil
}

func TestReadinessUnavailable(t *testing.T) {
	handler := NewHandler(
		"0.1.0-test",
		func(ctx context.Context) error {
			return errors.New("database unavailable")
		},
	)

	req := httptest.NewRequest(
		http.MethodGet,
		"/readyz",
		nil,
	)

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", rec.Code)
	}

	var got map[string]string

	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}

	if got["status"] != "not_ready" {
		t.Errorf("unexpected readiness status: %v", got)
	}
}

func TestRejectNonLoopbackHTTPAddresses(t *testing.T) {
	addresses := []string{
		"0.0.0.0:0",
		":0",
		"[::]:0",
		"192.168.1.50:0",
		"10.0.0.50:0",
		"172.16.0.50:0",
		"[fd00::50]:0",
		"localhost:0",
		"224.0.0.1:0",
		"invalid",
		"127.0.0.1:-1",
		"127.0.0.1:65536",
		"127.0.0.1:abc",
	}

	for _, addr := range addresses {
		t.Run(addr, func(t *testing.T) {
			listener, err := Listen(addr)

			if err == nil {
				listener.Close()
				t.Fatalf(
					"invalid HTTP address accepted: %s",
					addr,
				)
			}
		})
	}
}

func TestHTTPListenIPv4Loopback(t *testing.T) {
	listener, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("expected TCP listener")
	}

	if !addr.IP.IsLoopback() {
		t.Fatal("listener is not bound to loopback")
	}

	if addr.Port == 0 {
		t.Fatal("expected an assigned port")
	}
}

func TestHTTPListenIPv6Loopback(t *testing.T) {
	probe, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 loopback unavailable")
	}
	probe.Close()

	listener, err := Listen("[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("expected TCP listener")
	}

	if !addr.IP.IsLoopback() {
		t.Fatal("listener is not bound to loopback")
	}
}

func TestServerWithDevices(t *testing.T) {
	repository := &stubDeviceReader{
		records: testDeviceRecords(),
	}

	server := NewWithDevices(
		"127.0.0.1:0",
		"0.1.0-test",
		func(_ context.Context) error {
			return nil
		},
		repository,
	)

	paths := []string{
		"/healthz",
		"/readyz",
		"/api/v1/version",
		"/api/v1/devices",
		"/api/v1/devices/" + onlineDeviceID,
		"/api/v1/devices/summary",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				path,
				nil,
			)

			request.Host = "127.0.0.1:8080"
			recorder := httptest.NewRecorder()

			server.Handler.ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code != http.StatusOK {
				t.Fatalf(
					"expected status 200 for %s, got %d",
					path,
					recorder.Code,
				)
			}
		})
	}
}

func TestServerWithoutDevices(t *testing.T) {
	server := New(
		"127.0.0.1:0",
		"0.1.0-test",
		func(_ context.Context) error {
			return nil
		},
	)

	paths := []string{
		"/api/v1/devices",
		"/api/v1/devices/" + onlineDeviceID,
		"/api/v1/devices/summary",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				path,
				nil,
			)

			request.Host = "127.0.0.1:8080"
			recorder := httptest.NewRecorder()

			server.Handler.ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf(
					"expected status 404, got %d",
					recorder.Code,
				)
			}
		})
	}
}

func TestIntegratedDeviceSummary(t *testing.T) {
	server := NewWithDevices(
		"127.0.0.1:0",
		"0.1.0-test",
		func(_ context.Context) error {
			return nil
		},
		&stubDeviceReader{
			records: testDeviceRecords(),
		},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/summary",
		nil,
	)

	request.Host = "127.0.0.1:8080"
	recorder := httptest.NewRecorder()

	server.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d",
			recorder.Code,
		)
	}

	var response deviceSummaryResponse

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response.Total != 2 ||
		response.Online != 1 ||
		response.Offline != 1 {
		t.Fatalf(
			"unexpected device summary: %+v",
			response,
		)
	}
}
