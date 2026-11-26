package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/devices"
)

const (
	onlineDeviceID  = "53fd95de-e34d-4748-ad58-b7d404b600f1"
	offlineDeviceID = "53fd95de-e34d-4748-ad58-b7d404b600f2"
)

type stubDeviceReader struct {
	records []devices.Device
	err     error
}

func (s *stubDeviceReader) List(
	_ context.Context,
) ([]devices.Device, error) {
	if s.err != nil {
		return nil, s.err
	}

	return s.records, nil
}

func (s *stubDeviceReader) Get(
	_ context.Context,
	id string,
) (devices.Device, error) {
	if s.err != nil {
		return devices.Device{}, s.err
	}

	for _, device := range s.records {
		if device.ID == id {
			return device, nil
		}
	}

	return devices.Device{}, devices.ErrNotFound
}

func testDeviceRecords() []devices.Device {
	now := time.Now().UTC()

	return []devices.Device{
		{
			ID: onlineDeviceID,
			DeviceInfo: devices.DeviceInfo{
				Hostname:     "test-mac.local",
				OS:           "darwin",
				Architecture: "arm64",
				AgentVersion: "0.1.0-dev",
			},
			CreatedAt:  now.Add(-time.Hour),
			UpdatedAt:  now.Add(-5 * time.Second),
			LastSeenAt: now.Add(-5 * time.Second),
		},
		{
			ID: offlineDeviceID,
			DeviceInfo: devices.DeviceInfo{
				Hostname:     "test-linux.local",
				OS:           "linux",
				Architecture: "amd64",
				AgentVersion: "0.1.0-dev",
			},
			CreatedAt:  now.Add(-time.Hour),
			UpdatedAt:  now.Add(-5 * time.Minute),
			LastSeenAt: now.Add(-5 * time.Minute),
		},
	}
}

func newDeviceTestHandler(
	reader DeviceReader,
) http.Handler {
	mux := http.NewServeMux()

	registerDeviceRoutes(mux, reader)

	return mux
}

func TestListDevices(t *testing.T) {
	handler := newDeviceTestHandler(
		&stubDeviceReader{
			records: testDeviceRecords(),
		},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d",
			recorder.Code,
		)
	}

	if recorder.Header().Get("Content-Type") !=
		"application/json; charset=utf-8" {
		t.Fatal("incorrect content type")
	}

	if recorder.Header().Get("Cache-Control") !=
		"no-store" {
		t.Fatal("missing cache-control header")
	}

	var response deviceListResponse

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response.Total != 2 ||
		len(response.Devices) != 2 {
		t.Fatal("incorrect device count")
	}

	if response.Devices[0].ID != onlineDeviceID ||
		response.Devices[0].Hostname != "test-mac.local" ||
		response.Devices[0].Status != "online" {
		t.Fatal("incorrect online device information")
	}

	if response.Devices[1].ID != offlineDeviceID ||
		response.Devices[1].Hostname != "test-linux.local" ||
		response.Devices[1].Status != "offline" {
		t.Fatal("incorrect offline device information")
	}
}

func TestEmptyDeviceList(t *testing.T) {
	handler := newDeviceTestHandler(
		&stubDeviceReader{},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d",
			recorder.Code,
		)
	}

	var response deviceListResponse

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response.Total != 0 {
		t.Fatal("expected zero devices")
	}

	if response.Devices == nil {
		t.Fatal("expected empty array, not null")
	}

	if len(response.Devices) != 0 {
		t.Fatal("expected empty device list")
	}
}

func TestGetDevice(t *testing.T) {
	handler := newDeviceTestHandler(
		&stubDeviceReader{
			records: testDeviceRecords(),
		},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/"+onlineDeviceID,
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d",
			recorder.Code,
		)
	}

	var response deviceResponse

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response.ID != onlineDeviceID ||
		response.Hostname != "test-mac.local" ||
		response.OS != "darwin" ||
		response.Architecture != "arm64" ||
		response.AgentVersion != "0.1.0-dev" ||
		response.Status != "online" {
		t.Fatal("incorrect device response")
	}

	if response.CreatedAt.IsZero() ||
		response.UpdatedAt.IsZero() ||
		response.LastSeenAt.IsZero() {
		t.Fatal("missing device timestamps")
	}
}

func TestGetDeviceNotFound(t *testing.T) {
	handler := newDeviceTestHandler(
		&stubDeviceReader{},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/"+onlineDeviceID,
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf(
			"expected status 404, got %d",
			recorder.Code,
		)
	}

	var response deviceAPIError

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response.Error != "device not found" {
		t.Fatal("incorrect error response")
	}
}

func TestGetInvalidDevice(t *testing.T) {
	handler := newDeviceTestHandler(
		&stubDeviceReader{
			err: devices.ErrInvalidDevice,
		},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/invalid-id",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf(
			"expected status 400, got %d",
			recorder.Code,
		)
	}
}

func TestDeviceSummary(t *testing.T) {
	handler := newDeviceTestHandler(
		&stubDeviceReader{
			records: testDeviceRecords(),
		},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/summary",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

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
		t.Fatal("incorrect device summary")
	}
}

func TestEmptyDeviceSummary(t *testing.T) {
	handler := newDeviceTestHandler(
		&stubDeviceReader{},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/summary",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

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

	if response.Total != 0 ||
		response.Online != 0 ||
		response.Offline != 0 {
		t.Fatal("expected empty device summary")
	}
}

func TestDeviceRepositoryFailure(t *testing.T) {
	const internalMessage = "private database error"

	handler := newDeviceTestHandler(
		&stubDeviceReader{
			err: errors.New(internalMessage),
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

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code !=
				http.StatusInternalServerError {
				t.Fatalf(
					"expected status 500, got %d",
					recorder.Code,
				)
			}

			if strings.Contains(
				recorder.Body.String(),
				internalMessage,
			) {
				t.Fatal("internal database error leaked")
			}
		})
	}
}

func TestDeviceUnsupportedMethods(t *testing.T) {
	handler := newDeviceTestHandler(
		&stubDeviceReader{},
	)

	paths := []string{
		"/api/v1/devices",
		"/api/v1/devices/" + onlineDeviceID,
		"/api/v1/devices/summary",
	}

	methods := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
	}

	for _, path := range paths {
		for _, method := range methods {
			t.Run(method+" "+path, func(t *testing.T) {
				request := httptest.NewRequest(
					method,
					path,
					nil,
				)

				recorder := httptest.NewRecorder()

				handler.ServeHTTP(recorder, request)

				if recorder.Code !=
					http.StatusMethodNotAllowed {
					t.Fatalf(
						"expected status 405, got %d",
						recorder.Code,
					)
				}
			})
		}
	}
}

func TestDeviceResponseStatusBoundary(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name     string
		lastSeen time.Time
		expected string
	}{
		{
			name:     "recent heartbeat",
			lastSeen: now.Add(-30 * time.Second),
			expected: "online",
		},
		{
			name: "just before offline threshold",
			lastSeen: now.Add(
				-devices.DefaultOfflineTimeout +
					time.Millisecond,
			),
			expected: "online",
		},
		{
			name: "exact offline threshold",
			lastSeen: now.Add(
				-devices.DefaultOfflineTimeout,
			),
			expected: "offline",
		},
		{
			name:     "old heartbeat",
			lastSeen: now.Add(-5 * time.Minute),
			expected: "offline",
		},
		{
			name:     "future heartbeat",
			lastSeen: now.Add(time.Minute),
			expected: "offline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := devices.Device{
				ID:         onlineDeviceID,
				LastSeenAt: tt.lastSeen,
			}

			response := toDeviceResponse(device, now)

			if response.Status != tt.expected {
				t.Fatalf(
					"expected %s, got %s",
					tt.expected,
					response.Status,
				)
			}
		})
	}
}

func TestDeviceResponseFields(t *testing.T) {
	device := testDeviceRecords()[0]

	response := toDeviceResponse(
		device,
		time.Now().UTC(),
	)

	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage

	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}

	expected := []string{
		"id",
		"hostname",
		"os",
		"architecture",
		"agent_version",
		"status",
		"created_at",
		"updated_at",
		"last_seen_at",
	}

	if len(fields) != len(expected) {
		t.Fatal("unexpected API response fields")
	}

	for _, field := range expected {
		if _, ok := fields[field]; !ok {
			t.Errorf("missing response field: %s", field)
		}
	}
}
