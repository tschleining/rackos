package httpserver

import (
	"net/http"
	"strings"
)

// NewAuthenticatedManagementHandlerWithAuth creates
// the complete authenticated management API.
//
// Authentication endpoints and protected management
// routes share the same persistent session store.
//
// Only the exact POST login route is exempt from
// administrator session authentication.
//
// The caller must retain the same handler instance
// throughout the lifetime of the appliance server.
func NewAuthenticatedManagementHandlerWithAuth(
	version string,
	check ReadinessCheck,
	deviceRepository DeviceReader,
	performanceRepository PerformanceReader,
	inventoryRepository InventoryReader,
	administrators AdministratorAuthenticationReader,
	sessions AdministratorSessionStore,
) http.Handler {
	api := &authAPIHandler{
		administrators: administrators,
		sessions:       sessions,
	}

	limiter := newLoginRateLimiter()

	management := NewHandlerWithManagementInventory(
		version,
		check,
		deviceRepository,
		performanceRepository,
		inventoryRepository,
	)

	protectedManagement := protectManagementAuthentication(
		sessions,
		management,
	)

	login := authEndpointMethods(
		sessions,
		api.loginHandler(limiter),
		http.MethodPost,
	)

	logout := authEndpointMethods(
		sessions,
		api.logoutHandler(),
		http.MethodPost,
	)

	currentSession := authEndpointMethods(
		sessions,
		api.currentSessionHandler(),
		http.MethodGet,
		http.MethodHead,
	)

	activity := authEndpointMethods(
		sessions,
		api.activityHandler(),
		http.MethodPost,
	)

	// Dispatch authentication endpoints before
	// ServeMux performs path normalization.
	//
	// Alternate paths must never inherit the
	// public login exception.
	router := http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		switch r.URL.EscapedPath() {
		case authLoginPath:
			login.ServeHTTP(w, r)

		case authLogoutPath:
			logout.ServeHTTP(w, r)

		case authSessionPath:
			currentSession.ServeHTTP(w, r)

		case authActivityPath:
			activity.ServeHTTP(w, r)

		default:
			protectedManagement.ServeHTTP(w, r)
		}
	})

	// Local management security always executes
	// before authentication endpoint dispatch.
	return protectLocalManagement(router)
}

// authEndpointMethods permits only explicitly
// supported methods for an authentication endpoint.
//
// Unsupported methods remain authenticated by
// default, including unsupported login methods.
//
// Supported endpoint handlers enforce their own
// authentication and mutation security policies.
func authEndpointMethods(
	validator SessionValidator,
	next http.Handler,
	methods ...string,
) http.Handler {
	allow := strings.Join(methods, ", ")

	rejected := requireAuthenticatedSession(
		validator,
		http.HandlerFunc(func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			w.Header().Set("Allow", allow)

			writeDeviceError(
				w,
				http.StatusMethodNotAllowed,
				"method not allowed",
			)
		}),
	)

	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		for _, method := range methods {
			if r.Method == method {
				next.ServeHTTP(w, r)
				return
			}
		}

		rejected.ServeHTTP(w, r)
	})
}
