package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/inventory"
)

const inventoryRequestTimeout = 5 * time.Second

// InventoryReader defines the read-only operations
// required by the hardware inventory management API.
type InventoryReader interface {
	Get(
		context.Context,
		string,
	) (inventory.Inventory, error)
}

// inventoryResponse defines the public HTTP contract
// independently of the persistent inventory model.
//
// Memory capacity is encoded as a decimal string to
// preserve unsigned 64-bit integer precision.
type inventoryResponse struct {
	DeviceID string `json:"device_id"`

	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`

	LogicalCPUs     uint32 `json:"logical_cpus"`
	HostLogicalCPUs uint32 `json:"host_logical_cpus,omitempty"`
	PhysicalCores   uint32 `json:"physical_cores,omitempty"`

	CPUModel        string `json:"cpu_model,omitempty"`
	CPUManufacturer string `json:"cpu_manufacturer,omitempty"`

	MemoryBytes string `json:"memory_bytes,omitempty"`

	HardwareModel string `json:"hardware_model,omitempty"`
	OSVersion     string `json:"os_version,omitempty"`
	KernelVersion string `json:"kernel_version,omitempty"`

	BootTime *time.Time `json:"boot_time,omitempty"`

	CollectedAt time.Time `json:"collected_at"`
	ReceivedAt  time.Time `json:"received_at"`
}

type inventoryHandler struct {
	devices   DeviceReader
	inventory InventoryReader
}

func registerInventoryRoutes(
	mux *http.ServeMux,
	deviceRepository DeviceReader,
	inventoryRepository InventoryReader,
) {
	handler := &inventoryHandler{
		devices:   deviceRepository,
		inventory: inventoryRepository,
	}

	mux.HandleFunc(
		"GET /api/v1/devices/{id}/inventory",
		handler.get,
	)
}

func (h *inventoryHandler) get(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(
		r.Context(),
		inventoryRequestTimeout,
	)
	defer cancel()

	deviceID := r.PathValue("id")

	// Reuse the existing canonical device ID
	// validation used by performance endpoints.
	if err := validatePerformanceDeviceID(
		deviceID,
	); err != nil {
		writeDeviceError(
			w,
			http.StatusBadRequest,
			"invalid device ID",
		)
		return
	}

	if r.URL.RawQuery != "" {
		writeDeviceError(
			w,
			http.StatusBadRequest,
			"invalid inventory query",
		)
		return
	}

	device, err := h.devices.Get(
		ctx,
		deviceID,
	)

	switch {
	case errors.Is(err, devices.ErrNotFound):
		writeDeviceError(
			w,
			http.StatusNotFound,
			"device not found",
		)
		return

	case errors.Is(err, devices.ErrInvalidDevice):
		writeDeviceError(
			w,
			http.StatusBadRequest,
			"invalid device ID",
		)
		return

	case err != nil || ctx.Err() != nil:
		writeDeviceError(
			w,
			http.StatusInternalServerError,
			"failed to retrieve device",
		)
		return
	}

	if device.ID != deviceID {
		writeDeviceError(
			w,
			http.StatusInternalServerError,
			"failed to retrieve device",
		)
		return
	}

	record, err := h.inventory.Get(
		ctx,
		deviceID,
	)

	if errors.Is(err, inventory.ErrNotFound) {
		writeDeviceError(
			w,
			http.StatusNotFound,
			"no inventory snapshot found",
		)
		return
	}

	if err != nil ||
		ctx.Err() != nil ||
		record.DeviceID != deviceID {
		writeDeviceError(
			w,
			http.StatusInternalServerError,
			"failed to retrieve inventory",
		)
		return
	}

	response := toInventoryResponse(record)

	// Serialize before committing HTTP success.
	data, err := json.Marshal(response)
	if err != nil {
		writeDeviceError(
			w,
			http.StatusInternalServerError,
			"failed to serialize inventory response",
		)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)

	w.Header().Set(
		"Cache-Control",
		"no-store",
	)

	w.WriteHeader(http.StatusOK)

	_, _ = w.Write(append(data, '\n'))
}

func toInventoryResponse(
	record inventory.Inventory,
) inventoryResponse {
	response := inventoryResponse{
		DeviceID:        record.DeviceID,
		Hostname:        record.Hostname,
		OS:              record.OS,
		Architecture:    record.Architecture,
		LogicalCPUs:     record.LogicalCPUs,
		HostLogicalCPUs: record.HostLogicalCPUs,
		PhysicalCores:   record.PhysicalCores,
		CPUModel:        record.CPUModel,
		CPUManufacturer: record.CPUManufacturer,
		HardwareModel:   record.HardwareModel,
		OSVersion:       record.OSVersion,
		KernelVersion:   record.KernelVersion,
		CollectedAt:     record.CollectedAt.UTC(),
		ReceivedAt:      record.ReceivedAt.UTC(),
	}

	if record.MemoryBytes != 0 {
		response.MemoryBytes = strconv.FormatUint(
			record.MemoryBytes,
			10,
		)
	}

	if record.BootTime != nil {
		boot := record.BootTime.UTC()

		response.BootTime = &boot
	}

	return response
}
