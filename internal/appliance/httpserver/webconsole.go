package httpserver

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/tschleining/rackos/internal/appliance/webconsole"
)

// EnableWebConsole attaches the embedded console
// when the appliance was built with webconsole.
//
// Existing management API routing and security
// middleware remain unchanged.
func EnableWebConsole(
	server *http.Server,
) (bool, error) {
	if server == nil || server.Handler == nil {
		return false, errors.New(
			"management HTTP server is required",
		)
	}

	assets, err := webconsole.Assets()
	if err != nil {
		return false, fmt.Errorf(
			"load embedded web console: %w",
			err,
		)
	}

	if assets == nil {
		return false, nil
	}

	console, err := webconsole.New(assets)
	if err != nil {
		return false, fmt.Errorf(
			"initialize embedded web console: %w",
			err,
		)
	}

	server.Handler = mountWebConsole(
		server.Handler,
		console,
	)

	return true, nil
}

func mountWebConsole(
	management http.Handler,
	console http.Handler,
) http.Handler {
	// Apply the existing loopback Host, Origin
	// and Fetch Metadata checks to the console.
	protectedConsole :=
		protectLocalManagement(console)

	return http.HandlerFunc(
		func(
			w http.ResponseWriter,
			r *http.Request,
		) {
			if isManagementPath(r.URL.Path) {
				management.ServeHTTP(w, r)
				return
			}

			protectedConsole.ServeHTTP(w, r)
		},
	)
}

func isManagementPath(value string) bool {
	for _, prefix := range []string{
		"/api",
		"/healthz",
		"/readyz",
	} {
		if value == prefix ||
			strings.HasPrefix(
				value,
				prefix+"/",
			) {
			return true
		}
	}

	return false
}
