package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"

	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/performance"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	integrationEmptyDeviceID = "33333333-3333-4333-8333-333333333333"

	integrationUnknownDeviceID = "44444444-4444-4444-8444-444444444444"
)

func integrationPerformanceGET(
	t *testing.T,
	server *httptest.Server,
	path string,
	expectedStatus int,
) []byte {
	t.Helper()

	response, err := server.Client().Get(
		server.URL + path,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	if response.StatusCode != expectedStatus {
		t.Fatalf(
			"GET %s: expected %d, got %d: %s",
			path,
			expectedStatus,
			response.StatusCode,
			data,
		)
	}

	if response.Header.Get("Content-Type") !=
		"application/json; charset=utf-8" {
		t.Fatal("incorrect content type")
	}

	if response.Header.Get("Cache-Control") !=
		"no-store" {
		t.Fatal("missing cache-control header")
	}

	if !json.Valid(data) {
		t.Fatal("invalid JSON response")
	}

	return data
}

func integrationPerformanceHistory(
	t *testing.T,
	server *httptest.Server,
	deviceID string,
	values url.Values,
) performanceHistoryResponse {
	t.Helper()

	path := testHistoryPerformancePath(deviceID)

	if query := values.Encode(); query != "" {
		path += "?" + query
	}

	data := integrationPerformanceGET(
		t,
		server,
		path,
		http.StatusOK,
	)

	var response performanceHistoryResponse

	if err := json.Unmarshal(
		data,
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response.DeviceID != deviceID {
		t.Fatal("incorrect response device")
	}

	if response.Samples == nil {
		t.Fatal("history returned null samples")
	}

	if response.Count != len(response.Samples) {
		t.Fatal("incorrect history count")
	}

	if response.HasMore != (response.NextCursor != nil) {
		t.Fatal("inconsistent continuation state")
	}

	return response
}

func requireIntegrationPerformanceSample(
	t *testing.T,
	actual performanceResponse,
	expected performance.Performance,
) {
	t.Helper()

	if actual.DeviceID != expected.DeviceID ||
		actual.SampleID != expected.SampleID {
		t.Fatal("incorrect performance sample identity")
	}

	if actual.PayloadVersion != expected.PayloadVersion {
		t.Fatal("incorrect payload version")
	}

	if !actual.CollectedAt.Equal(expected.CollectedAt) ||
		!actual.ReceivedAt.Equal(expected.ReceivedAt) {
		t.Fatal("incorrect performance timestamps")
	}

	var snapshot rackosv1.PerformanceSnapshot

	if err := protojson.Unmarshal(
		actual.Snapshot,
		&snapshot,
	); err != nil {
		t.Fatal(err)
	}

	if !proto.Equal(
		&snapshot,
		expected.Snapshot,
	) {
		t.Fatal("HTTP snapshot differs from stored snapshot")
	}
}

func TestPerformanceAPIIntegration(
	t *testing.T,
) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	dir := t.TempDir()

	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	deviceRepository := devices.NewRepository(db)

	for _, deviceID := range []string{
		testPerformanceHTTPDeviceID,
		testPerformanceHTTPOtherDeviceID,
		integrationEmptyDeviceID,
	} {
		_, err := deviceRepository.Create(
			ctx,
			deviceID,
			devices.DeviceInfo{
				Hostname:     "integration-device.local",
				OS:           "linux",
				Architecture: "arm64",
				AgentVersion: "0.1.0-test",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	repository := performance.NewRepository(db)

	oldest := testHistoricalPerformanceRecord(
		"a",
		-2*time.Minute,
	)

	middle := testHistoricalPerformanceRecord(
		"b",
		-time.Minute,
	)

	tied := testHistoricalPerformanceRecord(
		"c",
		-time.Minute,
	)

	newest := testHistoricalPerformanceRecord(
		"d",
		0,
	)

	// The oldest measurement arrives after
	// the newest measurement.
	oldest.ReceivedAt = newest.ReceivedAt.Add(
		2 * time.Second,
	)

	other := testHistoricalPerformanceRecord(
		"d",
		0,
	)

	other.DeviceID = testPerformanceHTTPOtherDeviceID
	other.Snapshot.Cpu.UsagePercent = 77

	// Insert records out of chronological order.
	for _, record := range []performance.Performance{
		newest,
		oldest,
		tied,
		middle,
		other,
	} {
		receipt, err := repository.Insert(
			ctx,
			&rackosv1.ReportPerformanceRequest{
				DeviceId: record.DeviceID,
				SampleId: record.SampleID,
				Snapshot: record.Snapshot,
			},
			record.ReceivedAt,
		)
		if err != nil {
			t.Fatal(err)
		}

		if !receipt.Equal(record.ReceivedAt) {
			t.Fatal("unexpected receipt timestamp")
		}
	}

	server := httptest.NewServer(
		NewHandlerWithManagement(
			"0.1.0-test",
			db.PingContext,
			deviceRepository,
			repository,
		),
	)
	defer server.Close()

	server.Client().Timeout = 5 * time.Second

	t.Run("latest", func(t *testing.T) {
		data := integrationPerformanceGET(
			t,
			server,
			testLatestPerformancePath(
				testPerformanceHTTPDeviceID,
			),
			http.StatusOK,
		)

		var response performanceResponse

		if err := json.Unmarshal(
			data,
			&response,
		); err != nil {
			t.Fatal(err)
		}

		requireIntegrationPerformanceSample(
			t,
			response,
			newest,
		)
	})

	t.Run("time range", func(t *testing.T) {
		values := url.Values{
			"from": {
				middle.CollectedAt.
					Truncate(time.Millisecond).
					Format(time.RFC3339Nano),
			},
			"to": {
				newest.CollectedAt.
					Truncate(time.Millisecond).
					Format(time.RFC3339Nano),
			},
			"limit": {"25"},
		}

		page := integrationPerformanceHistory(
			t,
			server,
			testPerformanceHTTPDeviceID,
			values,
		)

		if page.Count != 2 || page.HasMore {
			t.Fatal("incorrect bounded history")
		}

		requireIntegrationPerformanceSample(
			t, page.Samples[0], tied,
		)

		requireIntegrationPerformanceSample(
			t, page.Samples[1], middle,
		)
	})

	values := url.Values{
		"limit": {"2"},
	}

	first := integrationPerformanceHistory(
		t,
		server,
		testPerformanceHTTPDeviceID,
		values,
	)

	if first.Count != 2 ||
		!first.HasMore ||
		first.NextCursor == nil {
		t.Fatal("incorrect first page")
	}

	requireIntegrationPerformanceSample(
		t, first.Samples[0], newest,
	)

	requireIntegrationPerformanceSample(
		t, first.Samples[1], tied,
	)

	values.Set("cursor", *first.NextCursor)

	t.Run("pagination", func(t *testing.T) {
		second := integrationPerformanceHistory(
			t,
			server,
			testPerformanceHTTPDeviceID,
			values,
		)

		if second.Count != 2 || second.HasMore {
			t.Fatal("incorrect continuation page")
		}

		requireIntegrationPerformanceSample(
			t, second.Samples[0], middle,
		)

		requireIntegrationPerformanceSample(
			t, second.Samples[1], oldest,
		)
	})

	t.Run("device isolation", func(t *testing.T) {
		page := integrationPerformanceHistory(
			t,
			server,
			testPerformanceHTTPOtherDeviceID,
			nil,
		)

		if page.Count != 1 || page.HasMore {
			t.Fatal("incorrect other device history")
		}

		requireIntegrationPerformanceSample(
			t, page.Samples[0], other,
		)

		// A cursor from another device must
		// be rejected before querying history.
		integrationPerformanceGET(
			t,
			server,
			testHistoryPerformancePath(
				testPerformanceHTTPOtherDeviceID,
			)+"?"+values.Encode(),
			http.StatusBadRequest,
		)
	})

	t.Run("empty and unknown devices", func(t *testing.T) {
		page := integrationPerformanceHistory(
			t,
			server,
			integrationEmptyDeviceID,
			nil,
		)

		if page.Count != 0 || page.HasMore {
			t.Fatal("incorrect empty device history")
		}

		integrationPerformanceGET(
			t,
			server,
			testLatestPerformancePath(
				integrationEmptyDeviceID,
			),
			http.StatusNotFound,
		)

		integrationPerformanceGET(
			t,
			server,
			testLatestPerformancePath(
				integrationUnknownDeviceID,
			),
			http.StatusNotFound,
		)

		integrationPerformanceGET(
			t,
			server,
			testHistoryPerformancePath(
				integrationUnknownDeviceID,
			),
			http.StatusNotFound,
		)
	})

	// Stop the HTTP server and close the database
	// before simulating an appliance restart.
	server.Close()

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	restarted := httptest.NewServer(
		NewHandlerWithManagement(
			"0.1.0-test",
			reopened.PingContext,
			devices.NewRepository(reopened),
			performance.NewRepository(reopened),
		),
	)
	defer restarted.Close()

	restarted.Client().Timeout = 5 * time.Second

	t.Run("restart persistence", func(t *testing.T) {
		page := integrationPerformanceHistory(
			t,
			restarted,
			testPerformanceHTTPDeviceID,
			values,
		)

		if page.Count != 2 || page.HasMore {
			t.Fatal("incorrect history after restart")
		}

		requireIntegrationPerformanceSample(
			t, page.Samples[0], middle,
		)

		requireIntegrationPerformanceSample(
			t, page.Samples[1], oldest,
		)
	})

	t.Run("corrupt stored payload", func(t *testing.T) {
		_, err := reopened.ExecContext(ctx, `
			UPDATE device_performance
			SET payload = X'FF'
			WHERE device_id = ?
				AND sample_id = ?
		`, newest.DeviceID, newest.SampleID)
		if err != nil {
			t.Fatal(err)
		}

		integrationPerformanceGET(
			t,
			restarted,
			testLatestPerformancePath(
				newest.DeviceID,
			),
			http.StatusInternalServerError,
		)

		integrationPerformanceGET(
			t,
			restarted,
			testHistoryPerformancePath(
				newest.DeviceID,
			),
			http.StatusInternalServerError,
		)
	})

	t.Run("device removal", func(t *testing.T) {
		_, err := reopened.ExecContext(ctx, `
			DELETE FROM devices
			WHERE id = ?
		`, testPerformanceHTTPDeviceID)
		if err != nil {
			t.Fatal(err)
		}

		var count int

		err = reopened.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM device_performance
			WHERE device_id = ?
		`, testPerformanceHTTPDeviceID).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}

		if count != 0 {
			t.Fatal("device history was not deleted")
		}

		integrationPerformanceGET(
			t,
			restarted,
			testHistoryPerformancePath(
				testPerformanceHTTPDeviceID,
			),
			http.StatusNotFound,
		)

		page := integrationPerformanceHistory(
			t,
			restarted,
			testPerformanceHTTPOtherDeviceID,
			nil,
		)

		if page.Count != 1 {
			t.Fatal("unrelated device history was affected")
		}
	})
}
