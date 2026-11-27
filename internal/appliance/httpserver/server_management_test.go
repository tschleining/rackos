package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/performance"
)

// Compile-time assertions ensure that the concrete
// repositories satisfy the management API contracts.
var (
	_ DeviceReader      = (*devices.Repository)(nil)
	_ PerformanceReader = (*performance.Repository)(nil)
)

func testManagementDeviceReader() DeviceReader {
	return &stubDeviceReader{
		records: []devices.Device{
			{
				ID: testPerformanceHTTPDeviceID,

				DeviceInfo: devices.DeviceInfo{
					Hostname:     "test-device.local",
					OS:           "linux",
					Architecture: "arm64",
					AgentVersion: "0.1.0-test",
				},
			},
		},
	}
}

func testManagementPerformanceReader() PerformanceReader {
	return &stubPerformanceReader{
		latest: func(
			_ context.Context,
			deviceID string,
		) (performance.Performance, error) {
			record := testPerformanceResponseRecord()

			record.DeviceID = deviceID

			return record, nil
		},

		history: func(
			_ context.Context,
			deviceID string,
			_ performance.HistoryQuery,
		) (performance.HistoryPage, error) {
			record := testPerformanceResponseRecord()

			record.DeviceID = deviceID

			return performance.HistoryPage{
				Samples: []performance.Performance{
					record,
				},
			}, nil
		},
	}
}

func TestManagementServerAllEndpoints(
	t *testing.T,
) {
	server := NewWithManagement(
		"127.0.0.1:0",
		"0.1.0-test",
		alwaysReady,
		testManagementDeviceReader(),
		testManagementPerformanceReader(),
	)

	paths := []string{
		"/healthz",
		"/readyz",
		"/api/v1/version",
		"/api/v1/devices",
		"/api/v1/devices/summary",
		"/api/v1/devices/" +
			testPerformanceHTTPDeviceID,
		testLatestPerformancePath(
			testPerformanceHTTPDeviceID,
		),
		testHistoryPerformancePath(
			testPerformanceHTTPDeviceID,
		),
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				path,
				nil,
			)

			recorder := httptest.NewRecorder()

			server.Handler.ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code != http.StatusOK {
				t.Fatalf(
					"expected status 200, got %d: %s",
					recorder.Code,
					recorder.Body.String(),
				)
			}

			if !json.Valid(recorder.Body.Bytes()) {
				t.Fatal("endpoint returned invalid JSON")
			}
		})
	}
}

func TestManagementServerPerformanceResponses(
	t *testing.T,
) {
	server := NewWithManagement(
		"127.0.0.1:0",
		"0.1.0-test",
		alwaysReady,
		testManagementDeviceReader(),
		testManagementPerformanceReader(),
	)

	request := httptest.NewRequest(
		http.MethodGet,
		testLatestPerformancePath(
			testPerformanceHTTPDeviceID,
		),
		nil,
	)

	recorder := httptest.NewRecorder()

	server.Handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"latest returned status %d",
			recorder.Code,
		)
	}

	requirePerformanceHTTPHeaders(
		t,
		recorder,
	)

	var latest performanceResponse

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&latest,
	); err != nil {
		t.Fatal(err)
	}

	if latest.DeviceID != testPerformanceHTTPDeviceID ||
		latest.SampleID != testPerformanceHTTPSampleID {
		t.Fatal("incorrect latest sample identity")
	}

	recorder = performHistoryRequest(
		server.Handler,
		testPerformanceHTTPDeviceID,
		"",
	)

	history := requirePerformanceHistoryResponse(
		t,
		recorder,
	)

	if history.Count != 1 ||
		history.HasMore {
		t.Fatal("incorrect integrated history response")
	}

	if history.Samples[0].SampleID != latest.SampleID {
		t.Fatal("latest and history samples differ")
	}
}

func TestManagementServerConstructorCompatibility(
	t *testing.T,
) {
	deviceRepository := testManagementDeviceReader()

	tests := []struct {
		name        string
		handler     http.Handler
		wantDevices bool
	}{
		{
			name: "New",
			handler: New(
				"127.0.0.1:0",
				"0.1.0-test",
				alwaysReady,
			).Handler,
		},
		{
			name: "NewHandler",
			handler: NewHandler(
				"0.1.0-test",
				alwaysReady,
			),
		},
		{
			name: "NewWithDevices",
			handler: NewWithDevices(
				"127.0.0.1:0",
				"0.1.0-test",
				alwaysReady,
				deviceRepository,
			).Handler,
			wantDevices: true,
		},
		{
			name: "NewHandlerWithDevices",
			handler: NewHandlerWithDevices(
				"0.1.0-test",
				alwaysReady,
				deviceRepository,
			),
			wantDevices: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := []struct {
				path   string
				status int
			}{
				{
					"/healthz",
					http.StatusOK,
				},
				{
					"/readyz",
					http.StatusOK,
				},
				{
					"/api/v1/version",
					http.StatusOK,
				},
				{
					testLatestPerformancePath(
						testPerformanceHTTPDeviceID,
					),
					http.StatusNotFound,
				},
				{
					testHistoryPerformancePath(
						testPerformanceHTTPDeviceID,
					),
					http.StatusNotFound,
				},
			}

			deviceStatus := http.StatusNotFound

			if tt.wantDevices {
				deviceStatus = http.StatusOK
			}

			paths = append(paths, struct {
				path   string
				status int
			}{
				"/api/v1/devices",
				deviceStatus,
			})

			for _, route := range paths {
				request := httptest.NewRequest(
					http.MethodGet,
					route.path,
					nil,
				)

				recorder := httptest.NewRecorder()

				tt.handler.ServeHTTP(
					recorder,
					request,
				)

				if recorder.Code != route.status {
					t.Fatalf(
						"%s: expected %d, got %d",
						route.path,
						route.status,
						recorder.Code,
					)
				}
			}
		})
	}
}

func TestManagementServerRepositoryRequirements(
	t *testing.T,
) {
	deviceRepository := testManagementDeviceReader()

	performanceRepository :=
		testManagementPerformanceReader()

	tests := []struct {
		name        string
		devices     DeviceReader
		performance PerformanceReader
		wantDevices bool
		wantMetrics bool
	}{
		{
			name: "neither repository",
		},
		{
			name:        "device repository only",
			devices:     deviceRepository,
			wantDevices: true,
		},
		{
			name:        "performance repository only",
			performance: performanceRepository,
		},
		{
			name:        "both repositories",
			devices:     deviceRepository,
			performance: performanceRepository,
			wantDevices: true,
			wantMetrics: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandlerWithManagement(
				"0.1.0-test",
				alwaysReady,
				tt.devices,
				tt.performance,
			)

			deviceStatus := http.StatusNotFound

			if tt.wantDevices {
				deviceStatus = http.StatusOK
			}

			performanceStatus := http.StatusNotFound

			if tt.wantMetrics {
				performanceStatus = http.StatusOK
			}

			paths := []struct {
				path   string
				status int
			}{
				{
					"/healthz",
					http.StatusOK,
				},
				{
					"/api/v1/devices",
					deviceStatus,
				},
				{
					testLatestPerformancePath(
						testPerformanceHTTPDeviceID,
					),
					performanceStatus,
				},
				{
					testHistoryPerformancePath(
						testPerformanceHTTPDeviceID,
					),
					performanceStatus,
				},
			}

			for _, route := range paths {
				request := httptest.NewRequest(
					http.MethodGet,
					route.path,
					nil,
				)

				recorder := httptest.NewRecorder()

				handler.ServeHTTP(
					recorder,
					request,
				)

				if recorder.Code != route.status {
					t.Fatalf(
						"%s: expected %d, got %d",
						route.path,
						route.status,
						recorder.Code,
					)
				}
			}
		})
	}
}

func TestManagementServerConfiguration(
	t *testing.T,
) {
	const address = "127.0.0.1:18080"

	server := NewWithManagement(
		address,
		"0.1.0-test",
		alwaysReady,
		testManagementDeviceReader(),
		testManagementPerformanceReader(),
	)

	if server.Addr != address {
		t.Fatal("incorrect management server address")
	}

	if server.Handler == nil {
		t.Fatal("missing management server handler")
	}

	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatal("incorrect read header timeout")
	}

	if server.ReadTimeout != 10*time.Second {
		t.Fatal("incorrect read timeout")
	}

	if server.WriteTimeout != 10*time.Second {
		t.Fatal("incorrect write timeout")
	}

	if server.IdleTimeout != 60*time.Second {
		t.Fatal("incorrect idle timeout")
	}

	if server.MaxHeaderBytes != 1<<20 {
		t.Fatal("incorrect maximum header size")
	}
}

func TestManagementServerReadinessFailure(
	t *testing.T,
) {
	server := NewWithManagement(
		"127.0.0.1:0",
		"0.1.0-test",
		func(context.Context) error {
			return errors.New(
				"private database failure",
			)
		},
		testManagementDeviceReader(),
		testManagementPerformanceReader(),
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/readyz",
		nil,
	)

	recorder := httptest.NewRecorder()

	server.Handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf(
			"expected status 503, got %d",
			recorder.Code,
		)
	}

	var response map[string]string

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response["status"] != "not_ready" {
		t.Fatal("incorrect readiness response")
	}
}

func TestManagementServerUnsupportedMethods(
	t *testing.T,
) {
	handler := NewHandlerWithManagement(
		"0.1.0-test",
		alwaysReady,
		&performanceTestDeviceReader{},
		&stubPerformanceReader{},
	)

	paths := []string{
		testLatestPerformancePath(
			testPerformanceHTTPDeviceID,
		),
		testHistoryPerformancePath(
			testPerformanceHTTPDeviceID,
		),
	}

	methods := []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
	}

	for _, path := range paths {
		for _, method := range methods {
			request := httptest.NewRequest(
				method,
				path,
				nil,
			)

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code !=
				http.StatusMethodNotAllowed {
				t.Fatalf(
					"%s %s: expected 405, got %d",
					method,
					path,
					recorder.Code,
				)
			}
		}
	}
}
