package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/performance"
)

const performanceRequestTimeout = 5 * time.Second

// PerformanceReader defines the read-only repository
// operations required by the performance management API.
type PerformanceReader interface {
	Latest(
		context.Context,
		string,
	) (performance.Performance, error)

	History(
		context.Context,
		string,
		performance.HistoryQuery,
	) (performance.HistoryPage, error)
}

type performanceHandler struct {
	devices     DeviceReader
	performance PerformanceReader
}

// registerPerformanceRoutes registers the read-only
// performance management endpoints.

func registerPerformanceRoutes(
	mux *http.ServeMux,
	deviceRepository DeviceReader,
	performanceRepository PerformanceReader,
) {
	handler := &performanceHandler{
		devices:     deviceRepository,
		performance: performanceRepository,
	}

	mux.HandleFunc(
		"GET /api/v1/devices/{id}/performance/latest",
		handler.latest,
	)

	mux.HandleFunc(
		"GET /api/v1/devices/{id}/performance/history",
		handler.history,
	)
}

// latest retrieves the most recently collected
// performance snapshot for a registered device.
func (h *performanceHandler) latest(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(
		r.Context(),
		performanceRequestTimeout,
	)
	defer cancel()

	deviceID := r.PathValue("id")

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

	if err := validateLatestPerformanceQuery(
		r.URL.RawQuery,
	); err != nil {
		writeDeviceError(
			w,
			http.StatusBadRequest,
			"invalid performance query",
		)
		return
	}

	if !h.requireDevice(ctx, w, deviceID) {
		return
	}

	record, err := h.performance.Latest(
		ctx,
		deviceID,
	)

	if errors.Is(err, performance.ErrNotFound) {
		writeDeviceError(
			w,
			http.StatusNotFound,
			"no performance samples found",
		)
		return
	}

	if errors.Is(err, performance.ErrInvalidQuery) {
		writeDeviceError(
			w,
			http.StatusBadRequest,
			"invalid performance query",
		)
		return
	}

	if err != nil || ctx.Err() != nil {
		writeDeviceError(
			w,
			http.StatusInternalServerError,
			"failed to retrieve performance",
		)
		return
	}

	// Defend against a repository returning a
	// record belonging to another device.
	if record.DeviceID != deviceID {
		writeDeviceError(
			w,
			http.StatusInternalServerError,
			"failed to retrieve performance",
		)
		return
	}

	response, err := toPerformanceResponse(
		record,
	)
	if err != nil {
		writeDeviceError(
			w,
			http.StatusInternalServerError,
			"failed to serialize performance response",
		)
		return
	}

	writePerformanceJSON(w, response)
}

// requireDevice verifies that the requested device
// remains registered with the appliance.
//
// Unknown devices are distinguished from registered
// devices without performance measurements.
func (h *performanceHandler) requireDevice(
	ctx context.Context,
	w http.ResponseWriter,
	deviceID string,
) bool {
	_, err := h.devices.Get(
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

	case errors.Is(err, devices.ErrInvalidDevice):
		writeDeviceError(
			w,
			http.StatusBadRequest,
			"invalid device ID",
		)

	case err != nil || ctx.Err() != nil:
		writeDeviceError(
			w,
			http.StatusInternalServerError,
			"failed to retrieve device",
		)

	default:
		return true
	}

	return false
}

// writePerformanceJSON serializes the complete
// response before committing the HTTP success status.
//
// This prevents serialization failures from producing
// incomplete successful responses.
func writePerformanceJSON(
	w http.ResponseWriter,
	value any,
) {
	data, err := json.Marshal(value)
	if err != nil {
		writeDeviceError(
			w,
			http.StatusInternalServerError,
			"failed to serialize performance response",
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

// history retrieves a bounded page of historical
// performance snapshots for a registered device.
//
// Query parameters support time-range filtering,
// page limits, and continuation cursors.
func (h *performanceHandler) history(
	w http.ResponseWriter,
	r *http.Request,
) {
	ctx, cancel := context.WithTimeout(
		r.Context(),
		performanceRequestTimeout,
	)
	defer cancel()

	deviceID := r.PathValue("id")

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

	options, err := parsePerformanceHistoryQuery(
		deviceID,
		r.URL.RawQuery,
	)
	if err != nil {
		writeDeviceError(
			w,
			http.StatusBadRequest,
			"invalid performance query",
		)
		return
	}

	if !h.requireDevice(ctx, w, deviceID) {
		return
	}

	page, err := h.performance.History(
		ctx,
		deviceID,
		options,
	)

	if errors.Is(err, performance.ErrInvalidQuery) {
		writeDeviceError(
			w,
			http.StatusBadRequest,
			"invalid performance query",
		)
		return
	}

	if err != nil || ctx.Err() != nil {
		writeDeviceError(
			w,
			http.StatusInternalServerError,
			"failed to retrieve performance history",
		)
		return
	}

	response, err := toPerformanceHistoryResponse(
		deviceID,
		options,
		page,
	)
	if err != nil {
		writeDeviceError(
			w,
			http.StatusInternalServerError,
			"failed to serialize performance response",
		)
		return
	}

	writePerformanceJSON(w, response)
}
