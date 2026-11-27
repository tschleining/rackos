package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

type ReadinessCheck func(context.Context) error

func New(
	addr string,
	version string,
	check ReadinessCheck,
) *http.Server {
	return NewWithDevices(
		addr,
		version,
		check,
		nil,
	)
}

func NewWithDevices(
	addr string,
	version string,
	check ReadinessCheck,
	repository DeviceReader,
) *http.Server {
	return NewWithManagement(
		addr,
		version,
		check,
		repository,
		nil,
	)
}

func NewWithManagement(
	addr string,
	version string,
	check ReadinessCheck,
	deviceRepository DeviceReader,
	performanceRepository PerformanceReader,
) *http.Server {
	return NewWithManagementInventory(
		addr,
		version,
		check,
		deviceRepository,
		performanceRepository,
		nil,
	)
}

// NewWithManagementInventory creates the appliance
// management HTTP server with optional inventory
// and performance APIs.
func NewWithManagementInventory(
	addr string,
	version string,
	check ReadinessCheck,
	deviceRepository DeviceReader,
	performanceRepository PerformanceReader,
	inventoryRepository InventoryReader,
) *http.Server {
	handler := NewHandlerWithManagementInventory(
		version,
		check,
		deviceRepository,
		performanceRepository,
		inventoryRepository,
	)

	return &http.Server{
		Addr: addr,

		Handler: protectLocalManagement(handler),

		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func NewHandler(
	version string,
	check ReadinessCheck,
) http.Handler {
	return NewHandlerWithDevices(
		version,
		check,
		nil,
	)
}

func NewHandlerWithDevices(
	version string,
	check ReadinessCheck,
	repository DeviceReader,
) http.Handler {
	return NewHandlerWithManagement(
		version,
		check,
		repository,
		nil,
	)
}

func NewHandlerWithManagement(
	version string,
	check ReadinessCheck,
	deviceRepository DeviceReader,
	performanceRepository PerformanceReader,
) http.Handler {
	return NewHandlerWithManagementInventory(
		version,
		check,
		deviceRepository,
		performanceRepository,
		nil,
	)
}

// NewHandlerWithManagementInventory creates the
// management API router.
//
// Inventory and performance endpoints require
// a device reader to verify registration.
//
// Network-facing servers must apply the local
// management security middleware.
func NewHandlerWithManagementInventory(
	version string,
	check ReadinessCheck,
	deviceRepository DeviceReader,
	performanceRepository PerformanceReader,
	inventoryRepository InventoryReader,
) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(
		"GET /healthz",
		handleHealth,
	)

	mux.HandleFunc(
		"GET /readyz",
		handleReady(check),
	)

	mux.HandleFunc(
		"GET /api/v1/version",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(
				w,
				http.StatusOK,
				map[string]string{
					"name":    "RackOS",
					"version": version,
				},
			)
		},
	)

	if deviceRepository != nil {
		registerDeviceRoutes(
			mux,
			deviceRepository,
		)

		if performanceRepository != nil {
			registerPerformanceRoutes(
				mux,
				deviceRepository,
				performanceRepository,
			)
		}

		if inventoryRepository != nil {
			registerInventoryRoutes(
				mux,
				deviceRepository,
				inventoryRepository,
			)
		}
	}

	return mux
}

func handleHealth(
	w http.ResponseWriter,
	r *http.Request,
) {
	writeJSON(
		w,
		http.StatusOK,
		map[string]string{
			"status": "healthy",
		},
	)
}

func handleReady(
	check ReadinessCheck,
) http.HandlerFunc {
	return func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		ctx, cancel := context.WithTimeout(
			r.Context(),
			2*time.Second,
		)
		defer cancel()

		if check == nil || check(ctx) != nil {
			writeJSON(
				w,
				http.StatusServiceUnavailable,
				map[string]string{
					"status": "not_ready",
				},
			)
			return
		}

		writeJSON(
			w,
			http.StatusOK,
			map[string]string{
				"status": "ready",
			},
		)
	}
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	data any,
) {
	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(data)
}

// Listen creates a loopback-only management API listener.
//
// Remote management access must remain disabled until
// administrator authentication and HTTPS are implemented.
func Listen(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid HTTP address: %w",
			err,
		)
	}

	ip := net.ParseIP(host)

	if ip == nil || !ip.IsLoopback() {
		return nil, fmt.Errorf(
			"HTTP management API requires an explicit loopback IP address",
		)
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf(
			"listen on HTTP address: %w",
			err,
		)
	}

	return listener, nil
}
