package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/performance"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func testHistoryPerformancePath(
	deviceID string,
) string {
	return "/api/v1/devices/" +
		deviceID +
		"/performance/history"
}

func performHistoryRequest(
	handler http.Handler,
	deviceID string,
	rawQuery string,
) *httptest.ResponseRecorder {
	path := testHistoryPerformancePath(
		deviceID,
	)

	if rawQuery != "" {
		path += "?" + rawQuery
	}

	request := httptest.NewRequest(
		http.MethodGet,
		path,
		nil,
	)

	request.Host = "127.0.0.1:8080"

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	return recorder
}

func requirePerformanceHistoryResponse(
	t *testing.T,
	recorder *httptest.ResponseRecorder,
) performanceHistoryResponse {
	t.Helper()

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"expected status 200, got %d: %s",
			recorder.Code,
			recorder.Body.String(),
		)
	}

	requirePerformanceHTTPHeaders(
		t,
		recorder,
	)

	var response performanceHistoryResponse

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response.DeviceID != testPerformanceHTTPDeviceID {
		t.Fatal("incorrect response device identity")
	}

	if response.Samples == nil {
		t.Fatal("history returned null instead of an array")
	}

	if response.Count != len(response.Samples) {
		t.Fatal("incorrect response sample count")
	}

	if response.HasMore != (response.NextCursor != nil) {
		t.Fatal("inconsistent continuation state")
	}

	return response
}

func testHistoricalPerformanceRecord(
	character string,
	offset time.Duration,
) performance.Performance {
	record := testPerformanceResponseRecord()

	record.SampleID = strings.Repeat(
		character,
		32,
	)

	record.CollectedAt = record.CollectedAt.Add(
		offset,
	)

	record.ReceivedAt = record.ReceivedAt.Add(
		offset,
	)

	record.Snapshot.CollectedAt = timestamppb.New(
		record.CollectedAt,
	)

	return record
}

func TestPerformanceHistorySuccess(
	t *testing.T,
) {
	first := testHistoricalPerformanceRecord(
		"b",
		0,
	)

	second := testHistoricalPerformanceRecord(
		"a",
		-time.Minute,
	)

	calls := 0

	reader := &stubPerformanceReader{
		history: func(
			ctx context.Context,
			deviceID string,
			options performance.HistoryQuery,
		) (performance.HistoryPage, error) {
			calls++

			if deviceID != testPerformanceHTTPDeviceID {
				t.Fatal("incorrect requested device")
			}

			if err := ctx.Err(); err != nil {
				t.Fatal(err)
			}

			if options.Limit !=
				performance.DefaultHistoryLimit {
				t.Fatal("incorrect default limit")
			}

			if options.FromMillis != nil ||
				options.ToMillis != nil ||
				options.After != nil {
				t.Fatal("unexpected default query bounds")
			}

			return performance.HistoryPage{
				Samples: []performance.Performance{
					first,
					second,
				},
			}, nil
		},
	}

	handler := newPerformanceTestHandler(
		testRegisteredPerformanceDeviceReader(),
		reader,
	)

	recorder := performHistoryRequest(
		handler,
		testPerformanceHTTPDeviceID,
		"",
	)

	response := requirePerformanceHistoryResponse(
		t,
		recorder,
	)

	if calls != 1 {
		t.Fatal("incorrect history query count")
	}

	if response.Count != 2 ||
		response.HasMore {
		t.Fatal("incorrect historical response")
	}

	expected := []performance.Performance{
		first,
		second,
	}

	for i, sample := range response.Samples {
		record := expected[i]

		if sample.DeviceID != record.DeviceID ||
			sample.SampleID != record.SampleID {
			t.Fatal("incorrect sample identity or ordering")
		}

		if !sample.CollectedAt.Equal(
			record.CollectedAt,
		) || !sample.ReceivedAt.Equal(
			record.ReceivedAt,
		) {
			t.Fatal("incorrect sample timestamps")
		}

		if sample.PayloadVersion !=
			record.PayloadVersion {
			t.Fatal("incorrect payload version")
		}

		var snapshot rackosv1.PerformanceSnapshot

		if err := protojson.Unmarshal(
			sample.Snapshot,
			&snapshot,
		); err != nil {
			t.Fatal(err)
		}

		if !proto.Equal(
			&snapshot,
			record.Snapshot,
		) {
			t.Fatal("historical snapshot changed")
		}
	}
}

func TestPerformanceHistoryEmpty(
	t *testing.T,
) {
	reader := &stubPerformanceReader{
		history: func(
			context.Context,
			string,
			performance.HistoryQuery,
		) (performance.HistoryPage, error) {
			return performance.HistoryPage{
				Samples: make(
					[]performance.Performance,
					0,
				),
			}, nil
		},
	}

	handler := newPerformanceTestHandler(
		testRegisteredPerformanceDeviceReader(),
		reader,
	)

	recorder := performHistoryRequest(
		handler,
		testPerformanceHTTPDeviceID,
		"",
	)

	response := requirePerformanceHistoryResponse(
		t,
		recorder,
	)

	if response.Count != 0 ||
		response.HasMore ||
		response.NextCursor != nil {
		t.Fatal("incorrect empty history")
	}

	object := performanceResponseJSONObject(
		t,
		recorder.Body.Bytes(),
	)

	requirePerformanceResponseField(
		t,
		object,
		"samples",
		"[]",
	)

	requirePerformanceResponseField(
		t,
		object,
		"next_cursor",
		"null",
	)
}

func TestPerformanceHistoryTimeRange(
	t *testing.T,
) {
	expectedFrom := time.Date(
		2026, 1, 1,
		10, 0, 0, 0,
		time.UTC,
	).UnixMilli()

	expectedTo := time.Date(
		2026, 1, 1,
		13, 0, 0, 0,
		time.UTC,
	).UnixMilli()

	calls := 0

	reader := &stubPerformanceReader{
		history: func(
			_ context.Context,
			deviceID string,
			options performance.HistoryQuery,
		) (performance.HistoryPage, error) {
			calls++

			if deviceID != testPerformanceHTTPDeviceID {
				t.Fatal("incorrect device identity")
			}

			if options.FromMillis == nil ||
				*options.FromMillis != expectedFrom {
				t.Fatal("incorrect lower time bound")
			}

			if options.ToMillis == nil ||
				*options.ToMillis != expectedTo {
				t.Fatal("incorrect upper time bound")
			}

			if options.Limit != 5 {
				t.Fatal("incorrect requested limit")
			}

			if options.After != nil {
				t.Fatal("unexpected continuation cursor")
			}

			return performance.HistoryPage{}, nil
		},
	}

	handler := newPerformanceTestHandler(
		testRegisteredPerformanceDeviceReader(),
		reader,
	)

	values := url.Values{
		"from": {
			"2026-01-01T12:00:00+02:00",
		},
		"to": {
			"2026-01-01T13:00:00Z",
		},
		"limit": {"5"},
	}

	recorder := performHistoryRequest(
		handler,
		testPerformanceHTTPDeviceID,
		values.Encode(),
	)

	response := requirePerformanceHistoryResponse(
		t,
		recorder,
	)

	if calls != 1 || response.Count != 0 {
		t.Fatal("incorrect bounded history response")
	}
}

func TestPerformanceHistoryPagination(
	t *testing.T,
) {
	first := testHistoricalPerformanceRecord(
		"c",
		0,
	)

	second := testHistoricalPerformanceRecord(
		"b",
		-time.Minute,
	)

	third := testHistoricalPerformanceRecord(
		"a",
		-2*time.Minute,
	)

	expectedCursor := performance.HistoryCursor{
		CollectedAtMillis: second.CollectedAt.UnixMilli(),
		SampleID:          second.SampleID,
	}

	expectedFrom := time.Date(
		2026, 1, 1,
		11, 0, 0, 0,
		time.UTC,
	).UnixMilli()

	expectedTo := time.Date(
		2026, 1, 1,
		13, 0, 0, 0,
		time.UTC,
	).UnixMilli()

	calls := 0

	reader := &stubPerformanceReader{
		history: func(
			_ context.Context,
			deviceID string,
			options performance.HistoryQuery,
		) (performance.HistoryPage, error) {
			calls++

			if deviceID != testPerformanceHTTPDeviceID {
				t.Fatal("incorrect pagination device")
			}

			if options.FromMillis == nil ||
				*options.FromMillis != expectedFrom ||
				options.ToMillis == nil ||
				*options.ToMillis != expectedTo {
				t.Fatal("pagination changed time bounds")
			}

			switch calls {
			case 1:
				if options.Limit != 2 ||
					options.After != nil {
					t.Fatal("incorrect initial query")
				}

				return performance.HistoryPage{
					Samples: []performance.Performance{
						first,
						second,
					},
					NextCursor: &expectedCursor,
				}, nil

			case 2:
				if options.Limit != 1 {
					t.Fatal("page size change was not preserved")
				}

				if options.After == nil ||
					*options.After != expectedCursor {
					t.Fatal("incorrect continuation position")
				}

				return performance.HistoryPage{
					Samples: []performance.Performance{
						third,
					},
				}, nil

			default:
				t.Fatal("unexpected historical query")
				return performance.HistoryPage{}, nil
			}
		},
	}

	handler := newPerformanceTestHandler(
		testRegisteredPerformanceDeviceReader(),
		reader,
	)

	values := url.Values{
		"from": {
			"2026-01-01T11:00:00Z",
		},
		"to": {
			"2026-01-01T13:00:00Z",
		},
		"limit": {"2"},
	}

	recorder := performHistoryRequest(
		handler,
		testPerformanceHTTPDeviceID,
		values.Encode(),
	)

	initial := requirePerformanceHistoryResponse(
		t,
		recorder,
	)

	if initial.Count != 2 ||
		!initial.HasMore ||
		initial.NextCursor == nil {
		t.Fatal("incorrect initial page")
	}

	if initial.Samples[0].SampleID != first.SampleID ||
		initial.Samples[1].SampleID != second.SampleID {
		t.Fatal("incorrect initial ordering")
	}

	// Repeat the original time bounds and pass
	// the opaque cursor returned by the API.
	values.Set(
		"cursor",
		*initial.NextCursor,
	)

	values.Set("limit", "1")

	recorder = performHistoryRequest(
		handler,
		testPerformanceHTTPDeviceID,
		values.Encode(),
	)

	final := requirePerformanceHistoryResponse(
		t,
		recorder,
	)

	if final.Count != 1 ||
		final.HasMore ||
		final.NextCursor != nil {
		t.Fatal("incorrect final page")
	}

	if final.Samples[0].SampleID != third.SampleID {
		t.Fatal("incorrect continuation record")
	}

	if calls != 2 {
		t.Fatal("incorrect pagination query count")
	}
}

func TestPerformanceHistoryInvalidDevice(
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
		recorder := performHistoryRequest(
			handler,
			deviceID,
			"",
		)

		requirePerformanceHTTPError(
			t,
			recorder,
			http.StatusBadRequest,
			"invalid device ID",
		)
	}
}

func TestPerformanceHistoryInvalidQuery(
	t *testing.T,
) {
	handler := newPerformanceTestHandler(
		&performanceTestDeviceReader{},
		&stubPerformanceReader{},
	)

	for _, rawQuery := range []string{
		"limit=0",
		"limit=26",
		"limit=1&limit=2",
		"from=invalid",
		"to=invalid",
		"unknown=value",
		"cursor=invalid",
		"cursor=",
		"limit=%zz",
		"from=2026-01-02T00:00:00Z&" +
			"to=2026-01-01T00:00:00Z",
	} {
		recorder := performHistoryRequest(
			handler,
			testPerformanceHTTPDeviceID,
			rawQuery,
		)

		requirePerformanceHTTPError(
			t,
			recorder,
			http.StatusBadRequest,
			"invalid performance query",
		)
	}
}

func TestPerformanceHistoryCursorMismatch(
	t *testing.T,
) {
	handler := newPerformanceTestHandler(
		&performanceTestDeviceReader{},
		&stubPerformanceReader{},
	)

	cursor := &performance.HistoryCursor{
		CollectedAtMillis: 2000,
		SampleID:          testPerformanceHTTPSampleID,
	}

	options := performance.HistoryQuery{
		Limit: 10,
	}

	encoded, err := encodePerformanceCursor(
		testPerformanceHTTPOtherDeviceID,
		options,
		cursor,
	)
	if err != nil {
		t.Fatal(err)
	}

	values := url.Values{
		"cursor": {encoded},
	}

	recorder := performHistoryRequest(
		handler,
		testPerformanceHTTPDeviceID,
		values.Encode(),
	)

	requirePerformanceHTTPError(
		t,
		recorder,
		http.StatusBadRequest,
		"invalid performance query",
	)

	// Encode a cursor belonging to a bounded
	// query, then omit its original time range.
	options.FromMillis = performanceHTTPMillisPtr(
		1000,
	)

	encoded, err = encodePerformanceCursor(
		testPerformanceHTTPDeviceID,
		options,
		cursor,
	)
	if err != nil {
		t.Fatal(err)
	}

	values.Set("cursor", encoded)

	recorder = performHistoryRequest(
		handler,
		testPerformanceHTTPDeviceID,
		values.Encode(),
	)

	requirePerformanceHTTPError(
		t,
		recorder,
		http.StatusBadRequest,
		"invalid performance query",
	)
}

func TestPerformanceHistoryDeviceNotFound(
	t *testing.T,
) {
	handler := newPerformanceTestHandler(
		&stubDeviceReader{},
		&stubPerformanceReader{},
	)

	recorder := performHistoryRequest(
		handler,
		testPerformanceHTTPDeviceID,
		"",
	)

	requirePerformanceHTTPError(
		t,
		recorder,
		http.StatusNotFound,
		"device not found",
	)
}

func TestPerformanceHistoryDeviceRepositoryFailure(
	t *testing.T,
) {
	const privateMessage = "private device database error"

	handler := newPerformanceTestHandler(
		&stubDeviceReader{
			err: errors.New(privateMessage),
		},
		&stubPerformanceReader{},
	)

	recorder := performHistoryRequest(
		handler,
		testPerformanceHTTPDeviceID,
		"",
	)

	requirePerformanceHTTPError(
		t,
		recorder,
		http.StatusInternalServerError,
		"failed to retrieve device",
	)

	if strings.Contains(
		recorder.Body.String(),
		privateMessage,
	) {
		t.Fatal("private database error leaked")
	}
}

func TestPerformanceHistoryRepositoryFailure(
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
			message: "failed to retrieve performance history",
		},
		{
			name: "corrupt snapshot",
			err: fmt.Errorf(
				"%w: %s",
				performance.ErrCorruptPerformance,
				privateMessage,
			),
			status:  http.StatusInternalServerError,
			message: "failed to retrieve performance history",
		},
		{
			name:    "unsupported payload version",
			err:     performance.ErrUnsupportedPayloadVersion,
			status:  http.StatusInternalServerError,
			message: "failed to retrieve performance history",
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
			message: "failed to retrieve performance history",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &stubPerformanceReader{
				history: func(
					context.Context,
					string,
					performance.HistoryQuery,
				) (performance.HistoryPage, error) {
					return performance.HistoryPage{},
						tt.err
				},
			}

			handler := newPerformanceTestHandler(
				testRegisteredPerformanceDeviceReader(),
				reader,
			)

			recorder := performHistoryRequest(
				handler,
				testPerformanceHTTPDeviceID,
				"",
			)

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

func TestPerformanceHistorySerializationFailure(
	t *testing.T,
) {
	tests := []struct {
		name   string
		change func(*performance.HistoryPage)
	}{
		{
			name: "missing snapshot",
			change: func(p *performance.HistoryPage) {
				p.Samples[1].Snapshot = nil
			},
		},
		{
			name: "different device",
			change: func(p *performance.HistoryPage) {
				p.Samples[1].DeviceID =
					testPerformanceHTTPOtherDeviceID
			},
		},
		{
			name: "invalid cursor",
			change: func(p *performance.HistoryPage) {
				p.NextCursor =
					&performance.HistoryCursor{
						CollectedAtMillis: 1000,
						SampleID:          "invalid",
					}
			},
		},
		{
			name: "invalid envelope timestamp",
			change: func(p *performance.HistoryPage) {
				p.Samples[1].ReceivedAt = time.Date(
					10000, 1, 1,
					0, 0, 0, 0,
					time.UTC,
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := performance.HistoryPage{
				Samples: []performance.Performance{
					testHistoricalPerformanceRecord(
						"b",
						0,
					),
					testHistoricalPerformanceRecord(
						"a",
						-time.Minute,
					),
				},
			}

			tt.change(&page)

			reader := &stubPerformanceReader{
				history: func(
					context.Context,
					string,
					performance.HistoryQuery,
				) (performance.HistoryPage, error) {
					return page, nil
				},
			}

			handler := newPerformanceTestHandler(
				testRegisteredPerformanceDeviceReader(),
				reader,
			)

			recorder := performHistoryRequest(
				handler,
				testPerformanceHTTPDeviceID,
				"",
			)

			requirePerformanceHTTPError(
				t,
				recorder,
				http.StatusInternalServerError,
				"failed to serialize performance response",
			)

			object := performanceResponseJSONObject(
				t,
				recorder.Body.Bytes(),
			)

			requirePerformanceResponseAbsent(
				t,
				object,
				"samples",
			)

			if strings.Contains(
				recorder.Body.String(),
				testPerformanceHTTPOtherDeviceID,
			) {
				t.Fatal("another device identity leaked")
			}
		})
	}
}

func TestPerformanceHistoryRequestDeadline(
	t *testing.T,
) {
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
		history: func(
			ctx context.Context,
			_ string,
			_ performance.HistoryQuery,
		) (performance.HistoryPage, error) {
			if ctx != lookupContext {
				t.Fatal("history did not reuse request context")
			}

			return performance.HistoryPage{}, nil
		},
	}

	handler := newPerformanceTestHandler(
		deviceReader,
		reader,
	)

	recorder := performHistoryRequest(
		handler,
		testPerformanceHTTPDeviceID,
		"",
	)

	requirePerformanceHistoryResponse(
		t,
		recorder,
	)

	if lookupContext == nil ||
		!errors.Is(
			lookupContext.Err(),
			context.Canceled,
		) {
		t.Fatal("request context was not canceled")
	}
}

func TestPerformanceHistoryCanceledRequest(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	defer cancel()

	reader := &stubPerformanceReader{
		history: func(
			requestContext context.Context,
			_ string,
			_ performance.HistoryQuery,
		) (performance.HistoryPage, error) {
			cancel()

			if !errors.Is(
				requestContext.Err(),
				context.Canceled,
			) {
				t.Fatal("request cancellation was not propagated")
			}

			// Even if the repository returns success,
			// a canceled request must not return data.
			return performance.HistoryPage{
				Samples: []performance.Performance{
					testPerformanceResponseRecord(),
				},
			}, nil
		},
	}

	handler := newPerformanceTestHandler(
		testRegisteredPerformanceDeviceReader(),
		reader,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		testHistoryPerformancePath(
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
		"failed to retrieve performance history",
	)
}

func TestPerformanceHistoryUnsupportedMethods(
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
			testHistoryPerformancePath(
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
