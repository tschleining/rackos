package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"

	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/performance"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type stubPerformanceReader struct {
	latest func(
		context.Context,
		string,
	) (performance.Performance, error)

	history func(
		context.Context,
		string,
		performance.HistoryQuery,
	) (performance.HistoryPage, error)
}

func (s *stubPerformanceReader) Latest(
	ctx context.Context,
	deviceID string,
) (performance.Performance, error) {
	if s.latest == nil {
		panic("unexpected latest performance query")
	}

	return s.latest(ctx, deviceID)
}

func (s *stubPerformanceReader) History(
	ctx context.Context,
	deviceID string,
	options performance.HistoryQuery,
) (performance.HistoryPage, error) {
	if s.history == nil {
		panic("unexpected historical performance query")
	}

	return s.history(ctx, deviceID, options)
}

type performanceTestDeviceReader struct {
	get func(
		context.Context,
		string,
	) (devices.Device, error)
}

func (s *performanceTestDeviceReader) List(
	context.Context,
) ([]devices.Device, error) {
	panic("unexpected device list query")
}

func (s *performanceTestDeviceReader) Get(
	ctx context.Context,
	deviceID string,
) (devices.Device, error) {
	if s.get == nil {
		panic("unexpected device lookup")
	}

	return s.get(ctx, deviceID)
}

func newPerformanceTestHandler(
	deviceRepository DeviceReader,
	performanceRepository PerformanceReader,
) http.Handler {
	mux := http.NewServeMux()

	registerPerformanceRoutes(
		mux,
		deviceRepository,
		performanceRepository,
	)

	return mux
}

func testLatestPerformancePath(
	deviceID string,
) string {
	return "/api/v1/devices/" +
		deviceID +
		"/performance/latest"
}

func testRegisteredPerformanceDeviceReader() DeviceReader {
	return &stubDeviceReader{
		records: []devices.Device{
			{
				ID: testPerformanceHTTPDeviceID,

				// An offline device must still
				// expose stored performance data.
				LastSeenAt: time.Now().UTC().
					Add(-time.Hour),
			},
		},
	}
}

func requirePerformanceHTTPHeaders(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
) {
	t.Helper()

	if recorder.Header().Get("Content-Type") !=
		"application/json; charset=utf-8" {
		t.Fatal("incorrect performance content type")
	}

	if recorder.Header().Get("Cache-Control") !=
		"no-store" {
		t.Fatal("missing performance cache-control")
	}
}

func requirePerformanceHTTPError(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
	status int,
	message string,
) {
	t.Helper()

	if recorder.Code != status {
		t.Fatalf(
			"expected status %d, got %d: %s",
			status,
			recorder.Code,
			recorder.Body.String(),
		)
	}

	requirePerformanceHTTPHeaders(t, recorder)

	var response deviceAPIError

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response.Error != message {
		t.Fatalf(
			"expected error %q, got %q",
			message,
			response.Error,
		)
	}
}

func TestPerformanceLatestSuccess(
	t *testing.T,
) {
	record := testPerformanceResponseRecord()

	calls := 0

	reader := &stubPerformanceReader{
		latest: func(
			ctx context.Context,
			deviceID string,
		) (performance.Performance, error) {
			calls++

			if deviceID != record.DeviceID {
				t.Fatal("incorrect requested device")
			}

			if err := ctx.Err(); err != nil {
				t.Fatal(err)
			}

			return record, nil
		},
	}

	handler := newPerformanceTestHandler(
		testRegisteredPerformanceDeviceReader(),
		reader,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		testLatestPerformancePath(record.DeviceID),
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d: %s",
			recorder.Code,
			recorder.Body.String(),
		)
	}

	requirePerformanceHTTPHeaders(t, recorder)

	if calls != 1 {
		t.Fatal("incorrect latest query count")
	}

	var response performanceResponse

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response.DeviceID != record.DeviceID ||
		response.SampleID != record.SampleID {
		t.Fatal("incorrect response identity")
	}

	if response.PayloadVersion !=
		record.PayloadVersion {
		t.Fatal("incorrect payload version")
	}

	if !response.CollectedAt.Equal(record.CollectedAt) ||
		!response.ReceivedAt.Equal(record.ReceivedAt) {
		t.Fatal("incorrect response timestamps")
	}

	var snapshot rackosv1.PerformanceSnapshot

	if err := protojson.Unmarshal(
		response.Snapshot,
		&snapshot,
	); err != nil {
		t.Fatal(err)
	}

	if !proto.Equal(
		&snapshot,
		record.Snapshot,
	) {
		t.Fatal("HTTP response changed snapshot")
	}
}

func TestPerformanceLatestInvalidDevice(
	t *testing.T,
) {
	handler := newPerformanceTestHandler(
		&performanceTestDeviceReader{},
		&stubPerformanceReader{},
	)

	for _, deviceID := range []string{
		"invalid-id",
		strings.ToUpper(testPerformanceHTTPDeviceID),
		"e2a104d8-9c72-1b31-8fa2-63001b24c9e8",
	} {
		request := httptest.NewRequest(
			http.MethodGet,
			testLatestPerformancePath(deviceID),
			nil,
		)

		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, request)

		requirePerformanceHTTPError(
			t,
			recorder,
			http.StatusBadRequest,
			"invalid device ID",
		)
	}
}

func TestPerformanceLatestInvalidQuery(
	t *testing.T,
) {
	handler := newPerformanceTestHandler(
		&performanceTestDeviceReader{},
		&stubPerformanceReader{},
	)

	for _, query := range []string{
		"limit=1",
		"from=2026-01-01T00:00:00Z",
		"cursor=invalid",
		"unknown=value",
		"limit=%zz",
	} {
		request := httptest.NewRequest(
			http.MethodGet,
			testLatestPerformancePath(
				testPerformanceHTTPDeviceID,
			)+"?"+query,
			nil,
		)

		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, request)

		requirePerformanceHTTPError(
			t,
			recorder,
			http.StatusBadRequest,
			"invalid performance query",
		)
	}
}

func TestPerformanceLatestDeviceNotFound(
	t *testing.T,
) {
	handler := newPerformanceTestHandler(
		&stubDeviceReader{},
		&stubPerformanceReader{},
	)

	request := httptest.NewRequest(
		http.MethodGet,
		testLatestPerformancePath(
			testPerformanceHTTPDeviceID,
		),
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	requirePerformanceHTTPError(
		t,
		recorder,
		http.StatusNotFound,
		"device not found",
	)
}

func TestPerformanceLatestNoSamples(
	t *testing.T,
) {
	reader := &stubPerformanceReader{
		latest: func(
			context.Context,
			string,
		) (performance.Performance, error) {
			return performance.Performance{},
				performance.ErrNotFound
		},
	}

	handler := newPerformanceTestHandler(
		testRegisteredPerformanceDeviceReader(),
		reader,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		testLatestPerformancePath(
			testPerformanceHTTPDeviceID,
		),
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	requirePerformanceHTTPError(
		t,
		recorder,
		http.StatusNotFound,
		"no performance samples found",
	)
}

func TestPerformanceLatestDeviceRepositoryFailure(
	t *testing.T,
) {
	const privateMessage = "private database error"

	tests := []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{
			name:    "database failure",
			err:     errors.New(privateMessage),
			status:  http.StatusInternalServerError,
			message: "failed to retrieve device",
		},
		{
			name:    "invalid device",
			err:     devices.ErrInvalidDevice,
			status:  http.StatusBadRequest,
			message: "invalid device ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newPerformanceTestHandler(
				&stubDeviceReader{
					err: tt.err,
				},
				&stubPerformanceReader{},
			)

			request := httptest.NewRequest(
				http.MethodGet,
				testLatestPerformancePath(
					testPerformanceHTTPDeviceID,
				),
				nil,
			)

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			requirePerformanceHTTPError(
				t,
				recorder,
				tt.status,
				tt.message,
			)

			if strings.Contains(
				recorder.Body.String(),
				privateMessage,
			) {
				t.Fatal("private database error leaked")
			}
		})
	}
}

func TestPerformanceLatestRepositoryFailure(
	t *testing.T,
) {
	const privateMessage = "private storage information"

	tests := []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{
			name:    "database failure",
			err:     errors.New(privateMessage),
			status:  http.StatusInternalServerError,
			message: "failed to retrieve performance",
		},
		{
			name: "corrupt snapshot",
			err: fmt.Errorf(
				"%w: %s",
				performance.ErrCorruptPerformance,
				privateMessage,
			),
			status:  http.StatusInternalServerError,
			message: "failed to retrieve performance",
		},
		{
			name:    "unsupported payload version",
			err:     performance.ErrUnsupportedPayloadVersion,
			status:  http.StatusInternalServerError,
			message: "failed to retrieve performance",
		},
		{
			name:    "invalid repository query",
			err:     performance.ErrInvalidQuery,
			status:  http.StatusBadRequest,
			message: "invalid performance query",
		},
		{
			name:    "deadline exceeded",
			err:     context.DeadlineExceeded,
			status:  http.StatusInternalServerError,
			message: "failed to retrieve performance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &stubPerformanceReader{
				latest: func(
					context.Context,
					string,
				) (performance.Performance, error) {
					return performance.Performance{},
						tt.err
				},
			}

			handler := newPerformanceTestHandler(
				testRegisteredPerformanceDeviceReader(),
				reader,
			)

			request := httptest.NewRequest(
				http.MethodGet,
				testLatestPerformancePath(
					testPerformanceHTTPDeviceID,
				),
				nil,
			)

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			requirePerformanceHTTPError(
				t,
				recorder,
				tt.status,
				tt.message,
			)

			if strings.Contains(
				recorder.Body.String(),
				privateMessage,
			) {
				t.Fatal("private storage error leaked")
			}
		})
	}
}

func TestPerformanceLatestDeviceMismatch(
	t *testing.T,
) {
	record := testPerformanceResponseRecord()

	record.DeviceID = testPerformanceHTTPOtherDeviceID

	reader := &stubPerformanceReader{
		latest: func(
			context.Context,
			string,
		) (performance.Performance, error) {
			return record, nil
		},
	}

	handler := newPerformanceTestHandler(
		testRegisteredPerformanceDeviceReader(),
		reader,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		testLatestPerformancePath(
			testPerformanceHTTPDeviceID,
		),
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	requirePerformanceHTTPError(
		t,
		recorder,
		http.StatusInternalServerError,
		"failed to retrieve performance",
	)

	if strings.Contains(
		recorder.Body.String(),
		testPerformanceHTTPOtherDeviceID,
	) {
		t.Fatal("another device identity leaked")
	}
}

func TestPerformanceLatestSerializationFailure(
	t *testing.T,
) {
	tests := []struct {
		name   string
		change func(*performance.Performance)
	}{
		{
			name: "missing snapshot",
			change: func(r *performance.Performance) {
				r.Snapshot = nil
			},
		},
		{
			name: "invalid envelope timestamp",
			change: func(r *performance.Performance) {
				r.ReceivedAt = time.Date(
					10000, 1, 1,
					0, 0, 0, 0,
					time.UTC,
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := testPerformanceResponseRecord()

			tt.change(&record)

			reader := &stubPerformanceReader{
				latest: func(
					context.Context,
					string,
				) (performance.Performance, error) {
					return record, nil
				},
			}

			handler := newPerformanceTestHandler(
				testRegisteredPerformanceDeviceReader(),
				reader,
			)

			request := httptest.NewRequest(
				http.MethodGet,
				testLatestPerformancePath(
					testPerformanceHTTPDeviceID,
				),
				nil,
			)

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			requirePerformanceHTTPError(
				t,
				recorder,
				http.StatusInternalServerError,
				"failed to serialize performance response",
			)
		})
	}
}

func TestPerformanceLatestRequestDeadline(
	t *testing.T,
) {
	record := testPerformanceResponseRecord()

	var lookupContext context.Context

	deviceReader := &performanceTestDeviceReader{
		get: func(
			ctx context.Context,
			deviceID string,
		) (devices.Device, error) {
			lookupContext = ctx

			deadline, exists := ctx.Deadline()

			if !exists {
				t.Fatal("missing request deadline")
			}

			remaining := time.Until(deadline)

			if remaining <= 0 ||
				remaining > performanceRequestTimeout {
				t.Fatal("incorrect request deadline")
			}

			return devices.Device{
				ID: deviceID,
			}, nil
		},
	}

	reader := &stubPerformanceReader{
		latest: func(
			ctx context.Context,
			deviceID string,
		) (performance.Performance, error) {
			if ctx != lookupContext {
				t.Fatal("repository did not reuse request context")
			}

			return record, nil
		},
	}

	handler := newPerformanceTestHandler(
		deviceReader,
		reader,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		testLatestPerformancePath(record.DeviceID),
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatal("deadline test request failed")
	}

	// The child context must be released
	// when request processing completes.
	if lookupContext == nil ||
		!errors.Is(
			lookupContext.Err(),
			context.Canceled,
		) {
		t.Fatal("request context was not canceled")
	}
}

func TestPerformanceLatestCanceledRequest(
	t *testing.T,
) {
	deviceReader := &performanceTestDeviceReader{
		get: func(
			ctx context.Context,
			_ string,
		) (devices.Device, error) {
			if !errors.Is(
				ctx.Err(),
				context.Canceled,
			) {
				t.Fatal("request cancellation was not propagated")
			}

			return devices.Device{}, ctx.Err()
		},
	}

	handler := newPerformanceTestHandler(
		deviceReader,
		&stubPerformanceReader{},
	)

	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	request := httptest.NewRequest(
		http.MethodGet,
		testLatestPerformancePath(
			testPerformanceHTTPDeviceID,
		),
		nil,
	).WithContext(ctx)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	requirePerformanceHTTPError(
		t,
		recorder,
		http.StatusInternalServerError,
		"failed to retrieve device",
	)
}

func TestPerformanceLatestUnsupportedMethods(
	t *testing.T,
) {
	handler := newPerformanceTestHandler(
		&performanceTestDeviceReader{},
		&stubPerformanceReader{},
	)

	for _, method := range []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
	} {
		request := httptest.NewRequest(
			method,
			testLatestPerformancePath(
				testPerformanceHTTPDeviceID,
			),
			nil,
		)

		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf(
				"%s: expected status 405, got %d",
				method,
				recorder.Code,
			)
		}

		if !strings.Contains(
			recorder.Header().Get("Allow"),
			http.MethodGet,
		) {
			t.Fatal("missing allowed GET method")
		}
	}
}
