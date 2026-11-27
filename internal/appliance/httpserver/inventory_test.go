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

	"github.com/tschleining/rackos/internal/appliance/inventory"
)

type stubInventoryReader struct {
	record inventory.Inventory
	err    error
	calls  int
}

func (s *stubInventoryReader) Get(
	ctx context.Context,
	id string,
) (inventory.Inventory, error) {
	s.calls++

	if err := ctx.Err(); err != nil {
		return inventory.Inventory{}, err
	}

	return s.record, s.err
}

func testHTTPInventoryRecord() inventory.Inventory {
	collected := time.Date(
		2026, time.November, 27,
		12, 0, 0, 125000000,
		time.UTC,
	)

	boot := collected.Add(-time.Hour)

	return inventory.Inventory{
		DeviceID:        onlineDeviceID,
		Hostname:        "test-mac.local",
		OS:              "darwin",
		Architecture:    "arm64",
		LogicalCPUs:     14,
		HostLogicalCPUs: 14,
		PhysicalCores:   14,
		CPUModel:        "Apple M4 Pro",
		CPUManufacturer: "Apple",
		MemoryBytes:     25769803776,
		HardwareModel:   "Mac16,8",
		OSVersion:       "26.6.2",
		KernelVersion:   "25.6.0",
		BootTime:        &boot,
		CollectedAt:     collected,
		ReceivedAt: collected.Add(
			time.Millisecond,
		),
	}
}

func TestInventoryAPIResponse(t *testing.T) {
	record := testHTTPInventoryRecord()

	// Deliberately exceed JavaScript's maximum
	// safe integer to verify exact serialization.
	record.MemoryBytes = 9007199254740993

	reader := &stubInventoryReader{
		record: record,
	}

	handler := NewHandlerWithManagementInventory(
		"0.1.0-test",
		alwaysReady,
		&stubDeviceReader{
			records: testDeviceRecords(),
		},
		nil,
		reader,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/"+onlineDeviceID+
			"/inventory",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"unexpected status %d: %s",
			recorder.Code,
			recorder.Body.String(),
		)
	}

	if recorder.Header().Get("Content-Type") !=
		"application/json; charset=utf-8" {
		t.Fatal("incorrect content type")
	}

	if recorder.Header().Get("Cache-Control") !=
		"no-store" {
		t.Fatal("missing cache control")
	}

	var response inventoryResponse

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response.DeviceID != record.DeviceID ||
		response.Hostname != record.Hostname ||
		response.OS != record.OS ||
		response.Architecture != record.Architecture ||
		response.LogicalCPUs != record.LogicalCPUs ||
		response.HostLogicalCPUs != record.HostLogicalCPUs ||
		response.PhysicalCores != record.PhysicalCores ||
		response.CPUModel != record.CPUModel ||
		response.CPUManufacturer != record.CPUManufacturer ||
		response.HardwareModel != record.HardwareModel ||
		response.OSVersion != record.OSVersion ||
		response.KernelVersion != record.KernelVersion {
		t.Fatal("inventory response differs from record")
	}

	if response.MemoryBytes != "9007199254740993" {
		t.Fatal("memory capacity lost precision")
	}

	if !response.CollectedAt.Equal(
		record.CollectedAt,
	) || !response.ReceivedAt.Equal(
		record.ReceivedAt,
	) {
		t.Fatal("incorrect inventory timestamps")
	}

	if response.BootTime == nil ||
		!response.BootTime.Equal(*record.BootTime) {
		t.Fatal("incorrect boot time")
	}

	if reader.calls != 1 {
		t.Fatal("unexpected inventory retrieval count")
	}
}

func TestInventoryAPIOptionalFields(t *testing.T) {
	now := time.Now().UTC()

	reader := &stubInventoryReader{
		record: inventory.Inventory{
			DeviceID:     onlineDeviceID,
			Hostname:     "minimal.local",
			OS:           "linux",
			Architecture: "amd64",
			LogicalCPUs:  4,
			CollectedAt:  now,
			ReceivedAt:   now,
		},
	}

	handler := NewHandlerWithManagementInventory(
		"0.1.0-test",
		alwaysReady,
		&stubDeviceReader{
			records: testDeviceRecords(),
		},
		nil,
		reader,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/devices/"+onlineDeviceID+
				"/inventory",
			nil,
		),
	)

	if recorder.Code != http.StatusOK {
		t.Fatal(recorder.Body.String())
	}

	var fields map[string]json.RawMessage

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&fields,
	); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"host_logical_cpus",
		"physical_cores",
		"cpu_model",
		"cpu_manufacturer",
		"memory_bytes",
		"hardware_model",
		"os_version",
		"kernel_version",
		"boot_time",
	} {
		if _, exists := fields[name]; exists {
			t.Errorf(
				"unavailable field was included: %s",
				name,
			)
		}
	}

	for _, name := range []string{
		"device_id",
		"hostname",
		"os",
		"architecture",
		"logical_cpus",
		"collected_at",
		"received_at",
	} {
		if _, exists := fields[name]; !exists {
			t.Errorf(
				"required field missing: %s",
				name,
			)
		}
	}
}

func TestInventoryAPIErrors(t *testing.T) {
	base := "/api/v1/devices/"

	tests := []struct {
		name      string
		path      string
		deviceErr error
		recordErr error
		recordID  string
		status    int
		message   string
		calls     int
	}{
		{
			name:    "invalid device",
			path:    base + "invalid/inventory",
			status:  http.StatusBadRequest,
			message: "invalid device ID",
		},
		{
			name: "uppercase device",
			path: base +
				strings.ToUpper(onlineDeviceID) +
				"/inventory",
			status:  http.StatusBadRequest,
			message: "invalid device ID",
		},
		{
			name: "unknown query",
			path: base + onlineDeviceID +
				"/inventory?limit=1",
			status:  http.StatusBadRequest,
			message: "invalid inventory query",
		},
		{
			name: "unknown device",
			path: base +
				"33333333-3333-4333-8333-333333333333" +
				"/inventory",
			status:  http.StatusNotFound,
			message: "device not found",
		},
		{
			name: "missing inventory",
			path: base + onlineDeviceID +
				"/inventory",
			recordErr: inventory.ErrNotFound,
			status:    http.StatusNotFound,
			message:   "no inventory snapshot found",
			calls:     1,
		},
		{
			name: "device repository failure",
			path: base + onlineDeviceID +
				"/inventory",
			deviceErr: errors.New("private database error"),
			status:    http.StatusInternalServerError,
			message:   "failed to retrieve device",
		},
		{
			name: "inventory repository failure",
			path: base + onlineDeviceID +
				"/inventory",
			recordErr: errors.New("private database error"),
			status:    http.StatusInternalServerError,
			message:   "failed to retrieve inventory",
			calls:     1,
		},
		{
			name: "cross-device record",
			path: base + onlineDeviceID +
				"/inventory",
			recordID: offlineDeviceID,
			status:   http.StatusInternalServerError,
			message:  "failed to retrieve inventory",
			calls:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := testHTTPInventoryRecord()

			if tt.recordID != "" {
				record.DeviceID = tt.recordID
			}

			reader := &stubInventoryReader{
				record: record,
				err:    tt.recordErr,
			}

			handler :=
				NewHandlerWithManagementInventory(
					"0.1.0-test",
					alwaysReady,
					&stubDeviceReader{
						records: testDeviceRecords(),
						err:     tt.deviceErr,
					},
					nil,
					reader,
				)

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(
				recorder,
				httptest.NewRequest(
					http.MethodGet,
					tt.path,
					nil,
				),
			)

			if recorder.Code != tt.status {
				t.Fatalf(
					"expected %d, got %d: %s",
					tt.status,
					recorder.Code,
					recorder.Body.String(),
				)
			}

			var response deviceAPIError

			if err := json.Unmarshal(
				recorder.Body.Bytes(),
				&response,
			); err != nil {
				t.Fatal(err)
			}

			if response.Error != tt.message {
				t.Fatalf(
					"unexpected error: %s",
					response.Error,
				)
			}

			if reader.calls != tt.calls {
				t.Fatal("unexpected inventory reader calls")
			}
		})
	}
}

func TestInventoryAPIMethodsAndRegistration(
	t *testing.T,
) {
	devices := &stubDeviceReader{
		records: testDeviceRecords(),
	}

	reader := &stubInventoryReader{
		record: testHTTPInventoryRecord(),
	}

	path := "/api/v1/devices/" +
		onlineDeviceID + "/inventory"

	handler := NewHandlerWithManagementInventory(
		"0.1.0-test",
		alwaysReady,
		devices,
		nil,
		reader,
	)

	for _, method := range []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
	} {
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(
			recorder,
			httptest.NewRequest(method, path, nil),
		)

		if recorder.Code != http.StatusMethodNotAllowed {
			t.Errorf(
				"%s returned %d",
				method,
				recorder.Code,
			)
		}
	}

	if reader.calls != 0 {
		t.Fatal("unsupported method reached repository")
	}

	for _, handler := range []http.Handler{
		NewHandlerWithManagement(
			"0.1.0-test",
			alwaysReady,
			devices,
			nil,
		),
		NewHandlerWithManagementInventory(
			"0.1.0-test",
			alwaysReady,
			nil,
			nil,
			reader,
		),
	} {
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(
			recorder,
			httptest.NewRequest(
				http.MethodGet,
				path,
				nil,
			),
		)

		if recorder.Code != http.StatusNotFound {
			t.Fatal("inventory route unexpectedly registered")
		}
	}
}
