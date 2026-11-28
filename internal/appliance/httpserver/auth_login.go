package httpserver

import (
	"context"
	"errors"
	"net/http"

	"github.com/tschleining/rackos/internal/appliance/auth"
	"github.com/tschleining/rackos/internal/appliance/session"
)

const authLoginPath = "/api/v1/auth/login"

// loginHandler composes the security middleware
// required by the administrator login endpoint.
//
// The limiter must be shared across all login
// requests handled by the appliance.
//
// The enclosing router must apply the existing
// local management security middleware.
func (h *authAPIHandler) loginHandler(
	limiter *loginRateLimiter,
) http.Handler {
	return requireAuthMutationCSRF(
		requireLoginCapacity(
			limiter,
			http.HandlerFunc(h.login),
		),
	)
}

// login verifies administrator credentials and
// creates a persistent authenticated session.
//
// The session token must not be delivered until
// transactional session issuance has committed.
func (h *authAPIHandler) login(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set(
		"Cache-Control",
		"no-store",
	)

	request, err := decodeLoginRequest(w, r)
	if err != nil {
		writeAuthLoginRequestError(w, err)
		return
	}

	defer request.clearPassword()

	if h == nil ||
		h.administrators == nil ||
		h.sessions == nil {
		writeAuthenticationUnavailable(w)
		return
	}

	ctx, cancel := context.WithTimeout(
		r.Context(),
		authOperationTimeout,
	)
	defer cancel()

	account, err := verifyAdministratorCredentials(
		ctx,
		h.administrators,
		request.username,
		request.password,
	)

	// Plaintext credentials are no longer required.
	request.clearPassword()

	if err != nil {
		writeAuthLoginFailure(w, err)
		return
	}

	if ctx.Err() != nil {
		writeAuthenticationUnavailable(w)
		return
	}

	// Pass the original verified account snapshot.
	// Session issuance checks the credential revision
	// again inside its database transaction.
	issued, err := h.sessions.Issue(
		ctx,
		account,
	)
	if err != nil {
		writeAuthLoginFailure(w, err)
		return
	}

	current := issued.Session

	// Defensively reject inconsistent session
	// metadata returned by the session dependency.
	if current.ID == "" ||
		current.AdministratorID != account.ID ||
		!current.PasswordChangedAt.Equal(
			account.PasswordChangedAt,
		) {
		writeAuthenticationUnavailable(w)
		return
	}

	// Session issuance has committed successfully.
	// Only now may the browser receive its token.
	if err := setSessionCookie(
		w,
		r,
		issued.Token(),
	); err != nil {
		writeAuthenticationUnavailable(w)
		return
	}

	writeDeviceJSON(
		w,
		http.StatusOK,
		toAuthSessionResponse(current),
	)
}

// writeAuthLoginRequestError maps request validation
// failures without reflecting supplied credentials
// or underlying JSON decoder errors.
func writeAuthLoginRequestError(
	w http.ResponseWriter,
	err error,
) {
	switch {
	case errors.Is(err, errLoginRequestTooLarge):
		writeDeviceError(
			w,
			http.StatusRequestEntityTooLarge,
			"login request too large",
		)

	case errors.Is(err, errUnsupportedLoginMediaType):
		writeDeviceError(
			w,
			http.StatusUnsupportedMediaType,
			"unsupported login media type",
		)

	default:
		writeDeviceError(
			w,
			http.StatusBadRequest,
			"invalid login request",
		)
	}
}

// writeAuthLoginFailure maps authentication and
// session issuance failures to public responses.
//
// Internal database and credential errors must
// never be exposed to the client.
func writeAuthLoginFailure(
	w http.ResponseWriter,
	err error,
) {
	switch {
	case errors.Is(
		err,
		errInvalidAdministratorCredentials,
	),
		errors.Is(
			err,
			session.ErrCredentialsChanged,
		):

		writeDeviceError(
			w,
			http.StatusUnauthorized,
			"invalid administrator credentials",
		)

	case errors.Is(err, auth.ErrPasswordHashBusy):
		writeLoginRateLimit(
			w,
			loginBusyRetry,
		)

	case errors.Is(err, session.ErrSessionLimit):
		writeDeviceError(
			w,
			http.StatusConflict,
			"administrator session limit reached",
		)

	default:
		writeAuthenticationUnavailable(w)
	}
}
