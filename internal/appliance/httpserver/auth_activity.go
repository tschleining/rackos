package httpserver

import (
	"context"
	"errors"
	"net/http"

	"github.com/tschleining/rackos/internal/appliance/session"
)

const authActivityPath = "/api/v1/auth/activity"

// activityHandler composes CSRF protection and
// administrator session authentication.
//
// The enclosing router must apply the existing
// local management security middleware.
func (h *authAPIHandler) activityHandler() http.Handler {
	var validator SessionValidator

	if h != nil {
		validator = h.sessions
	}

	return requireAuthMutationCSRF(
		requireAuthenticatedSession(
			validator,
			http.HandlerFunc(h.activity),
		),
	)
}

// activity renews the inactivity window of the
// current authenticated administrator session.
//
// It must never extend the absolute lifetime,
// revive an expired session or issue a new token.
func (h *authAPIHandler) activity(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set(
		"Cache-Control",
		"no-store",
	)

	if err := validateEmptyAuthMutation(r); err != nil {
		writeDeviceError(
			w,
			http.StatusBadRequest,
			"invalid authentication request",
		)
		return
	}

	if h == nil || h.sessions == nil {
		writeAuthenticationUnavailable(w)
		return
	}

	previous, ok := authenticatedSession(
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

	// Authentication tokens are deliberately
	// excluded from the request context.
	token, err := readSessionCookie(r)
	if err != nil {
		writeDeviceError(
			w,
			http.StatusUnauthorized,
			"authentication required",
		)
		return
	}

	ctx, cancel := context.WithTimeout(
		r.Context(),
		authOperationTimeout,
	)
	defer cancel()

	// RecordActivity independently validates
	// the session within its write transaction.
	//
	// Middleware authentication alone cannot
	// authorize renewal of an expired or
	// concurrently revoked session.
	current, err := h.sessions.RecordActivity(
		ctx,
		token,
	)

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

	// Verify that renewal preserved the original
	// session identity and immutable timestamps.
	if current.ID != previous.ID ||
		current.AdministratorID != previous.AdministratorID ||
		!current.PasswordChangedAt.Equal(
			previous.PasswordChangedAt,
		) ||
		!current.CreatedAt.Equal(
			previous.CreatedAt,
		) ||
		!current.ExpiresAt.Equal(
			previous.ExpiresAt,
		) ||
		current.LastActivityAt.Before(
			previous.LastActivityAt,
		) ||
		!current.LastActivityAt.Before(
			current.ExpiresAt,
		) {
		writeAuthenticationUnavailable(w)
		return
	}

	// Renewal has committed successfully.
	// Return updated nonsecret session metadata.
	//
	// The original authentication cookie remains
	// unchanged.
	writeDeviceJSON(
		w,
		http.StatusOK,
		toAuthSessionResponse(current),
	)
}
