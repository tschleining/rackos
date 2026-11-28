package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tschleining/rackos/internal/appliance/session"
)

const sessionCookieName = "__Host-rackos_session"

var (
	errSessionCookieMissing = errors.New(
		"administrator session cookie missing",
	)

	errSessionCookieInvalid = errors.New(
		"invalid administrator session cookie",
	)

	errSessionHTTPSRequired = errors.New(
		"administrator authentication requires HTTPS",
	)
)

// readSessionCookie extracts exactly one canonical
// administrator authentication token.
//
// Cookie extraction does not authenticate a session.
// The caller must validate the token using the
// server-side session repository.
func readSessionCookie(
	r *http.Request,
) (string, error) {
	// Trust only the actual TLS connection.
	// Never trust forwarded protocol headers.
	if r.TLS == nil {
		return "", errSessionHTTPSRequired
	}

	var token string
	found := false

	for _, header := range r.Header.Values("Cookie") {
		// A comma is not permitted in a standard
		// cookie value. Reject ambiguous combined
		// cookie headers rather than selecting a
		// potentially unintended credential.
		if strings.Contains(header, ",") {
			return "", errSessionCookieInvalid
		}

		for _, part := range strings.Split(header, ";") {
			// Permit ordinary whitespace following
			// a cookie separator.
			part = strings.TrimLeft(part, " \t")

			name, value, hasValue := strings.Cut(
				part,
				"=",
			)

			if strings.Trim(name, " \t") !=
				sessionCookieName {
				continue
			}

			// Reject duplicate or malformed
			// authentication cookie occurrences.
			if found ||
				!hasValue ||
				name != sessionCookieName {
				return "", errSessionCookieInvalid
			}

			found = true

			// Our session tokens never require
			// quotes, padding or URL decoding.
			if err := session.ValidateTokenEncoding(
				value,
			); err != nil {
				return "", errSessionCookieInvalid
			}

			token = value
		}
	}

	if !found {
		return "", errSessionCookieMissing
	}

	return token, nil
}

// newSessionCookie defines the security attributes
// shared by cookie issuance and deletion.
//
// Domain is intentionally omitted to satisfy the
// __Host- cookie requirements.
func newSessionCookie(value string) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
}

// setSessionCookie delivers a session token to the
// browser after successful session issuance.
//
// The caller must supply a token from a committed
// server-side session. This helper does not create
// or authenticate sessions.
func setSessionCookie(
	w http.ResponseWriter,
	r *http.Request,
	token string,
) error {
	if r.TLS == nil {
		return errSessionHTTPSRequired
	}

	if err := session.ValidateTokenEncoding(
		token,
	); err != nil {
		return errSessionCookieInvalid
	}

	// Authentication responses must never be
	// stored in browser or intermediary caches.
	w.Header().Set("Cache-Control", "no-store")

	// Expires and MaxAge remain unset.
	// The browser receives a session cookie.
	http.SetCookie(
		w,
		newSessionCookie(token),
	)

	return nil
}

// clearSessionCookie instructs the browser to remove
// the existing administrator authentication cookie.
//
// Browser cookie deletion is not server-side logout.
// The caller must separately revoke the session
// when performing an authenticated logout.
func clearSessionCookie(
	w http.ResponseWriter,
	r *http.Request,
) error {
	if r.TLS == nil {
		return errSessionHTTPSRequired
	}

	cookie := newSessionCookie("")

	// Match the original cookie's name, path
	// and host scope while expiring its value.
	cookie.Expires = time.Unix(1, 0).UTC()
	cookie.MaxAge = -1

	w.Header().Set("Cache-Control", "no-store")

	http.SetCookie(w, cookie)

	return nil
}
