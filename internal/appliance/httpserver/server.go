package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type ReadinessCheck func(context.Context) error

func New(
	addr string,
	version string,
	check ReadinessCheck,
) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           NewHandler(version, check),
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
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", handleHealth)
	mux.HandleFunc("GET /readyz", handleReady(check))

	mux.HandleFunc("GET /api/v1/version",
		func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{
				"name":    "RackOS",
				"version": version,
			})
		},
	)

	return mux
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "healthy",
	})
}

func handleReady(check ReadinessCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		writeJSON(w, http.StatusOK, map[string]string{
			"status": "ready",
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
