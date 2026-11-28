package httpserver

import "net/http"

const (
	authCSRFHeaderName  = "X-RackOS-CSRF"
	authCSRFHeaderValue = "1"
)

// requireAuthMutationCSRF protects authentication
// endpoints that modify administrator session state.
//
// This middleware supplements the existing local
// management security middleware.
//
// The custom header is not an authentication token.
// It must be combined with strict origin validation
// and a restrictive cross-origin access policy.
func requireAuthMutationCSRF(
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		w.Header().Set(
			"Cache-Control",
			"no-store",
		)

		if r.Method != http.MethodPost {
			w.Header().Set(
				"Allow",
				http.MethodPost,
			)

			writeDeviceError(
				w,
				http.StatusMethodNotAllowed,
				"method not allowed",
			)
			return
		}

		// Never trust forwarded protocol headers.
		if r.TLS == nil {
			writeAuthCSRFFailure(w)
			return
		}

		// Authentication mutations require an
		// explicit, unambiguous Origin header.
		origins := r.Header.Values("Origin")

		if len(origins) != 1 ||
			origins[0] == "" {
			writeAuthCSRFFailure(w)
			return
		}

		// Reuse the existing canonical management
		// authority, Origin and Fetch Metadata checks.
		//
		// Unlike ordinary management requests,
		// authentication mutations cannot omit Origin.
		if !isLocalManagementRequest(r) {
			writeAuthCSRFFailure(w)
			return
		}

		// Require exactly one custom CSRF header.
		// Do not accept alternate values, whitespace
		// or comma-separated header combinations.
		values := r.Header.Values(
			authCSRFHeaderName,
		)

		if len(values) != 1 ||
			values[0] != authCSRFHeaderValue {
			writeAuthCSRFFailure(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Return a generic security rejection without
// reflecting untrusted request header values.
func writeAuthCSRFFailure(
	w http.ResponseWriter,
) {
	writeDeviceError(
		w,
		http.StatusForbidden,
		"authentication request forbidden",
	)
}
