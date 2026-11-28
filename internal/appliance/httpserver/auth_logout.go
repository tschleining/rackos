package httpserver

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/tschleining/rackos/internal/appliance/session"
)

const authLogoutPath = "/api/v1/auth/logout"

var errInvalidAuthMutationRequest = errors.New(
	"invalid authentication request",
)

// logoutHandler composes CSRF protection and
// administrator session authentication.
//
// The enclosing router must apply the existing
// local management security middleware.
func (h *authAPIHandler) logoutHandler() http.Handler {
	var validator SessionValidator

	if h != nil {
		validator = h.sessions
	}

	return requireAuthMutationCSRF(
		requireAuthenticatedSession(
			validator,
			http.HandlerFunc(h.logout),
		),
	)
}

// validateEmptyAuthMutation rejects unexpected
// query parameters, request bodies and encodings.
//
// This helper is shared by authentication mutations
// that do not require a request payload.
func validateEmptyAuthMutation(
	r *http.Request,
) error {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return errInvalidAuthMutationRequest
	}

	if len(r.Header.Values("Content-Encoding")) != 0 {
		return errInvalidAuthMutationRequest
	}

	if r.ContentLength > 0 {
		return errInvalidAuthMutationRequest
	}

	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}

	// ContentLength may be unknown, including
	// requests using chunked transfer encoding.
	// Read at most one byte to detect a body.
	var probe [1]byte

	n, err := io.ReadFull(
		r.Body,
		probe[:],
	)

	clear(probe[:])

	if n != 0 || err != io.EOF {
		return errInvalidAuthMutationRequest
	}

	return nil
}

// logout permanently revokes the authenticated
// session before deleting the browser cookie.
func (h *authAPIHandler) logout(
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

	// Require the authentication middleware to
	// have established a validated session.
	if _, ok := authenticatedSession(
		r.Context(),
	); !ok {
		writeDeviceError(
			w,
			http.StatusUnauthorized,
			"authentication required",
		)
		return
	}

	// The authentication token is deliberately
	// excluded from the request context.
	//
	// Read it again from the validated cookie.
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

	// Persistent revocation revalidates the
	// session within its write transaction.
	//
	// Session deletion and logout audit logging
	// must commit together.
	revoked, err := h.sessions.Revoke(
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

	if !revoked {
		// The session may have expired or been
		// revoked after middleware validation.
		writeDeviceError(
			w,
			http.StatusUnauthorized,
			"authentication required",
		)
		return
	}

	// Persistent revocation has committed.
	// Only now remove the browser credential.
	if err := clearSessionCookie(
		w,
		r,
	); err != nil {
		writeAuthenticationUnavailable(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
