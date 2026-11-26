package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/tschleining/rackos/internal/appliance/devices"
)

const deviceRequestTimeout = 5 * time.Second

// DeviceReader defines the read-only repository operations
// required by the device management API.
type DeviceReader interface {
	List(context.Context) ([]devices.Device, error)
	Get(context.Context, string) (devices.Device, error)
}

type deviceResponse struct {
	ID           string    `json:"id"`
	Hostname     string    `json:"hostname"`
	OS           string    `json:"os"`
	Architecture string    `json:"architecture"`
	AgentVersion string    `json:"agent_version"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

type deviceListResponse struct {
	Devices []deviceResponse `json:"devices"`
	Total   int              `json:"total"`
}

type deviceSummaryResponse struct {
	Total   int `json:"total"`
	Online  int `json:"online"`
	Offline int `json:"offline"`
}

type deviceAPIError struct {
	Error string `json:"error"`
}

type deviceHandler struct {
	repository DeviceReader
}

// registerDeviceRoutes registers the read-only
// device management endpoints.
func registerDeviceRoutes(
	mux *http.ServeMux,
	repository DeviceReader,
) {
	handler := &deviceHandler{
		repository: repository,
	}

	mux.HandleFunc(
		"GET /api/v1/devices",
		handler.list,
	)

	mux.HandleFunc(
		"GET /api/v1/devices/{id}",
		handler.get,
	)

	mux.HandleFunc(
		"GET /api/v1/devices/summary",
		handler.summary,
	)
}

func (h *deviceHandler) list(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(
		r.Context(),
		deviceRequestTimeout,
	)
	defer cancel()

	records, err := h.repository.List(ctx)
	if err != nil {
		writeDeviceError(
			w,
			http.StatusInternalServerError,
			"failed to list devices",
		)
		return
	}

	now := time.Now().UTC()

	response := deviceListResponse{
		Devices: make([]deviceResponse, 0, len(records)),
		Total:   len(records),
	}

	for _, device := range records {
		response.Devices = append(
			response.Devices,
			toDeviceResponse(device, now),
		)
	}

	writeDeviceJSON(w, http.StatusOK, response)
}

func (h *deviceHandler) get(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(
		r.Context(),
		deviceRequestTimeout,
	)
	defer cancel()

	id := r.PathValue("id")

	device, err := h.repository.Get(ctx, id)

	if errors.Is(err, devices.ErrNotFound) {
		writeDeviceError(
			w,
			http.StatusNotFound,
			"device not found",
		)
		return
	}

	if errors.Is(err, devices.ErrInvalidDevice) {
		writeDeviceError(
			w,
			http.StatusBadRequest,
			"invalid device ID",
		)
		return
	}

	if err != nil {
		writeDeviceError(
			w,
			http.StatusInternalServerError,
			"failed to retrieve device",
		)
		return
	}

	response := toDeviceResponse(
		device,
		time.Now().UTC(),
	)

	writeDeviceJSON(w, http.StatusOK, response)
}

func (h *deviceHandler) summary(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(
		r.Context(),
		deviceRequestTimeout,
	)
	defer cancel()

	records, err := h.repository.List(ctx)
	if err != nil {
		writeDeviceError(
			w,
			http.StatusInternalServerError,
			"failed to summarize devices",
		)
		return
	}

	now := time.Now().UTC()

	response := deviceSummaryResponse{
		Total: len(records),
	}

	for _, device := range records {
		if device.Status(
			now,
			devices.DefaultOfflineTimeout,
		) == devices.StatusOnline {
			response.Online++
		} else {
			response.Offline++
		}
	}

	writeDeviceJSON(w, http.StatusOK, response)
}

func toDeviceResponse(
	device devices.Device,
	now time.Time,
) deviceResponse {
	return deviceResponse{
		ID:           device.ID,
		Hostname:     device.Hostname,
		OS:           device.OS,
		Architecture: device.Architecture,
		AgentVersion: device.AgentVersion,

		Status: string(device.Status(
			now,
			devices.DefaultOfflineTimeout,
		)),

		CreatedAt:  device.CreatedAt.UTC(),
		UpdatedAt:  device.UpdatedAt.UTC(),
		LastSeenAt: device.LastSeenAt.UTC(),
	}
}

func writeDeviceError(
	w http.ResponseWriter,
	code int,
	message string,
) {
	writeDeviceJSON(
		w,
		code,
		deviceAPIError{
			Error: message,
		},
	)
}

func writeDeviceJSON(
	w http.ResponseWriter,
	code int,
	value any,
) {
	w.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)

	w.Header().Set(
		"Cache-Control",
		"no-store",
	)

	w.WriteHeader(code)

	_ = json.NewEncoder(w).Encode(value)
}
