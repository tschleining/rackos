package httpserver

import "net/http"

const authSessionPath = "/api/v1/auth/session"

// currentSessionHandler authenticates requests
// for the current administrator session.
//
// Unlike authentication mutations, this endpoint
// does not require a CSRF header because it never
// modifies persistent session state.
//
// The enclosing router must apply the existing
// local management security middleware.
func (h *authAPIHandler) currentSessionHandler() http.Handler {
	var validator SessionValidator

	if h != nil {
		validator = h.sessions
	}

	protected := requireAuthenticatedSession(
		validator,
		http.HandlerFunc(h.currentSession),
	)

	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		w.Header().Set(
			"Cache-Control",
			"no-store",
		)

		switch r.Method {
		case http.MethodGet, http.MethodHead:
			protected.ServeHTTP(w, r)

		default:
			w.Header().Set(
				"Allow",
				"GET, HEAD",
			)

			writeDeviceError(
				w,
				http.StatusMethodNotAllowed,
				"method not allowed",
			)
		}
	})
}

// currentSession returns the validated session
// metadata established by authentication middleware.
//
// This operation must not renew session activity,
// issue cookies or perform another database lookup.
func (h *authAPIHandler) currentSession(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set(
		"Cache-Control",
		"no-store",
	)

	// Reuse the empty request validation rules.
	// Session inspection accepts no payload,
	// query parameters or content encoding.
	if err := validateEmptyAuthMutation(r); err != nil {
		writeDeviceError(
			w,
			http.StatusBadRequest,
			"invalid authentication request",
		)
		return
	}

	current, ok := authenticatedSession(
		r.Context(),
	)

	if !ok {
		writeDeviceError(
			w,
			http.StatusUnauthorized,
			"authentication required",
		)
		return
	}

	writeDeviceJSON(
		w,
		http.StatusOK,
		toAuthSessionResponse(current),
	)
}
