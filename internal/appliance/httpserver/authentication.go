package httpserver

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/session"
)

const authenticationTimeout = 5 * time.Second

// SessionValidator defines the read-only session
// operation required by authentication middleware.
//
// The implementation must verify session expiration,
// administrator status and credential revision.
//
// Authentication must never renew session activity.
type SessionValidator interface {
	Validate(
		context.Context,
		string,
	) (session.Session, error)
}

// Ensure the existing session repository implements
// the middleware's validation interface.
var _ SessionValidator = (*session.Repository)(nil)

// An unexported context key prevents collisions
// with context values belonging to other packages.
type authenticatedSessionKey struct{}

// authenticatedSession retrieves the administrator
// session established by authentication middleware.
//
// It contains nonsecret session metadata only.
// The authentication token is not stored here.
func authenticatedSession(
	ctx context.Context,
) (session.Session, bool) {
	current, ok := ctx.Value(
		authenticatedSessionKey{},
	).(session.Session)

	return current, ok
}

// requireAuthenticatedSession authenticates requests
// before allowing them to reach a protected handler.
//
// This middleware must run inside the existing
// local management security middleware.
//
// Authorization and CSRF protection are separate
// responsibilities.
func requireAuthenticatedSession(
	validator SessionValidator,
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

		// Authentication requires a real TLS
		// connection, not forwarded headers.
		if r.TLS == nil {
			writeDeviceError(
				w,
				http.StatusForbidden,
				"administrator authentication requires HTTPS",
			)
			return
		}

		token, err := readSessionCookie(r)
		if err != nil {
			writeDeviceError(
				w,
				http.StatusUnauthorized,
				"authentication required",
			)
			return
		}

		// A missing validator is a server
		// configuration failure. Never bypass
		// authentication when it is unavailable.
		if validator == nil {
			writeAuthenticationUnavailable(w)
			return
		}

		// Limit the session validation operation
		// without imposing the same deadline on
		// the downstream management handler.
		ctx, cancel := context.WithTimeout(
			r.Context(),
			authenticationTimeout,
		)

		current, err := validator.Validate(
			ctx,
			token,
		)

		// Check cancellation before releasing the
		// validation context.
		contextErr := ctx.Err()

		cancel()

		if contextErr != nil {
			writeAuthenticationUnavailable(w)
			return
		}

		if errors.Is(err, session.ErrInvalidSession) {
			writeDeviceError(
				w,
				http.StatusUnauthorized,
				"authentication required",
			)
			return
		}

		if err != nil {
			writeAuthenticationUnavailable(w)
			return
		}

		// Defensively reject an invalid successful
		// response from the validation dependency.
		if current.ID == "" ||
			!admin.IsValidID(
				current.AdministratorID,
			) {
			writeAuthenticationUnavailable(w)
			return
		}

		// Attach only the validated, nonsecret
		// session metadata to the original request
		// context.
		authenticatedContext := context.WithValue(
			r.Context(),
			authenticatedSessionKey{},
			current,
		)

		next.ServeHTTP(
			w,
			r.WithContext(authenticatedContext),
		)
	})
}

// writeAuthenticationUnavailable distinguishes
// infrastructure failure from invalid credentials
// without exposing internal database details.
func writeAuthenticationUnavailable(
	w http.ResponseWriter,
) {
	writeDeviceError(
		w,
		http.StatusServiceUnavailable,
		"authentication service unavailable",
	)
}
