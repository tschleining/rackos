package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/inventory"
	"github.com/tschleining/rackos/internal/appliance/performance"
)

func TestInventoryAPIIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	deviceRepository := devices.NewRepository(db)
	inventoryRepository := inventory.NewRepository(db)

	for _, id := range []string{
		onlineDeviceID,
		offlineDeviceID,
	} {
		_, err := deviceRepository.Create(
			ctx,
			id,
			devices.DeviceInfo{
				Hostname:     "integration.local",
				OS:           "darwin",
				Architecture: "arm64",
				AgentVersion: "0.1.0-test",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	configuredServer := NewWithManagementInventory(
		"127.0.0.1:0",
		"0.1.0-test",
		db.PingContext,
		deviceRepository,
		performance.NewRepository(db),
		inventoryRepository,
	)

	server := httptest.NewServer(
		configuredServer.Handler,
	)
	defer server.Close()

	server.Client().Timeout = 5 * time.Second

	path := "/api/v1/devices/" +
		onlineDeviceID + "/inventory"

	// A registered device without inventory
	// must return a distinct not-found response.
	data := integrationPerformanceGET(
		t,
		server,
		path,
		http.StatusNotFound,
	)

	var apiError deviceAPIError

	if err := json.Unmarshal(
		data,
		&apiError,
	); err != nil {
		t.Fatal(err)
	}

	if apiError.Error != "no inventory snapshot found" {
		t.Fatal("incorrect missing inventory response")
	}

	record := testHTTPInventoryRecord()

	if err := inventoryRepository.Upsert(
		ctx,
		record,
	); err != nil {
		t.Fatal(err)
	}

	data = integrationPerformanceGET(
		t,
		server,
		path,
		http.StatusOK,
	)

	var response inventoryResponse

	if err := json.Unmarshal(
		data,
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response.DeviceID != onlineDeviceID ||
		response.CPUModel != record.CPUModel ||
		response.MemoryBytes != "25769803776" {
		t.Fatal("incorrect stored inventory response")
	}

	if !response.CollectedAt.Equal(
		record.CollectedAt,
	) || !response.ReceivedAt.Equal(
		record.ReceivedAt,
	) {
		t.Fatal("incorrect stored inventory timestamps")
	}

	// Inventory belonging to one device must
	// not be returned for another device.
	integrationPerformanceGET(
		t,
		server,
		"/api/v1/devices/"+offlineDeviceID+
			"/inventory",
		http.StatusNotFound,
	)

	integrationPerformanceGET(
		t,
		server,
		"/api/v1/devices/"+
			"33333333-3333-4333-8333-333333333333"+
			"/inventory",
		http.StatusNotFound,
	)

	// The existing performance API must
	// remain registered and functional.
	integrationPerformanceGET(
		t,
		server,
		"/api/v1/devices/"+onlineDeviceID+
			"/performance/history",
		http.StatusOK,
	)

	// Cross-site browser requests must be
	// rejected by the network-facing server.
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		server.URL+path,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set(
		"Origin",
		"https://external.example",
	)

	request.Header.Set(
		"Sec-Fetch-Site",
		"cross-site",
	)

	blocked, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer blocked.Body.Close()

	if blocked.StatusCode != http.StatusForbidden {
		t.Fatal("cross-site request was not blocked")
	}
}
