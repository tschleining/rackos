package httpserver

import "net/http"

// NewAuthenticatedManagementHandler creates a
// management router that requires administrator
// authentication by default.
//
// Only explicitly public endpoints are exempt.
// Local management security checks always run
// before administrator authentication.
func NewAuthenticatedManagementHandler(
	version string,
	check ReadinessCheck,
	deviceRepository DeviceReader,
	performanceRepository PerformanceReader,
	inventoryRepository InventoryReader,
	validator SessionValidator,
) http.Handler {
	management := NewHandlerWithManagementInventory(
		version,
		check,
		deviceRepository,
		performanceRepository,
		inventoryRepository,
	)

	return protectLocalManagement(
		protectManagementAuthentication(
			validator,
			management,
		),
	)
}

// protectManagementAuthentication applies session
// authentication to all nonpublic management requests.
func protectManagementAuthentication(
	validator SessionValidator,
	next http.Handler,
) http.Handler {
	protected := requireAuthenticatedSession(
		validator,
		next,
	)

	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		if isPublicManagementRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		protected.ServeHTTP(w, r)
	})
}

// isPublicManagementRequest recognizes only explicitly
// permitted methods and canonical endpoint paths.
//
// Encoded paths, alternate paths and future endpoints
// do not implicitly inherit public access.
func isPublicManagementRequest(
	r *http.Request,
) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	default:
		return false
	}

	switch r.URL.EscapedPath() {
	case "/healthz",
		"/readyz",
		"/api/v1/version":
		return true

	default:
		return false
	}
}
