package httpserver

import (
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

// protectLocalManagement restricts management requests
// to explicit loopback authorities and rejects browser
// requests originating from other websites.
//
// This is not administrator authentication.
func protectLocalManagement(
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

		w.Header().Set(
			"X-Content-Type-Options",
			"nosniff",
		)

		w.Header().Set(
			"X-Frame-Options",
			"DENY",
		)

		w.Header().Set(
			"Referrer-Policy",
			"no-referrer",
		)

		w.Header().Set(
			"Cross-Origin-Resource-Policy",
			"same-origin",
		)

		w.Header().Add("Vary", "Origin")
		w.Header().Add("Vary", "Sec-Fetch-Site")

		if !isLocalManagementRequest(r) {
			writeDeviceError(
				w,
				http.StatusForbidden,
				"management request forbidden",
			)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isLocalManagementRequest(
	r *http.Request,
) bool {
	scheme := "http"
	defaultPort := 80

	// Trust the actual TLS connection, not headers
	// supplied by a client or reverse proxy.
	if r.TLS != nil {
		scheme = "https"
		defaultPort = 443
	}

	ip, port, ok := parseLocalManagementHost(
		r.Host,
		defaultPort,
	)
	if !ok {
		return false
	}

	// When running inside an actual HTTP server,
	// require the requested authority to match
	// the local connection address.
	if local, ok := r.Context().Value(
		http.LocalAddrContextKey,
	).(*net.TCPAddr); ok {
		localIP, valid := netip.AddrFromSlice(
			local.IP,
		)

		if !valid ||
			localIP.Unmap() != ip ||
			local.Port != port {
			return false
		}
	}

	expectedOrigin := scheme + "://" +
		net.JoinHostPort(
			ip.String(),
			strconv.Itoa(port),
		)

	// Browsers omit the default port when
	// serializing an origin.
	if port == defaultPort {
		if ip.Is6() {
			expectedOrigin = scheme +
				"://[" + ip.String() + "]"
		} else {
			expectedOrigin = scheme +
				"://" + ip.String()
		}
	}

	origins := r.Header.Values("Origin")

	if len(origins) > 1 {
		return false
	}

	if len(origins) == 1 &&
		origins[0] != expectedOrigin {
		return false
	}

	sites := r.Header.Values("Sec-Fetch-Site")

	if len(sites) > 1 {
		return false
	}

	if len(sites) == 1 {
		switch sites[0] {
		case "same-origin", "none":
			// Same-origin requests and direct
			// user-initiated navigation.

		default:
			return false
		}
	}

	// Non-browser clients may legitimately
	// omit browser metadata.
	return true
}

func parseLocalManagementHost(
	authority string,
	defaultPort int,
) (netip.Addr, int, bool) {
	host, portText, err :=
		net.SplitHostPort(authority)

	explicitPort := err == nil

	if err != nil {
		portText = strconv.Itoa(defaultPort)

		switch {
		case strings.HasPrefix(authority, "[") &&
			strings.HasSuffix(authority, "]"):
			host = authority[1 : len(authority)-1]

			if !strings.Contains(host, ":") {
				return netip.Addr{}, 0, false
			}

		case !strings.Contains(authority, ":"):
			host = authority

		default:
			return netip.Addr{}, 0, false
		}
	}

	ip, err := netip.ParseAddr(host)

	if err != nil ||
		!ip.IsLoopback() ||
		ip.Is4In6() ||
		ip.Zone() != "" {
		return netip.Addr{}, 0, false
	}

	if ip.String() != host {
		return netip.Addr{}, 0, false
	}

	port, err := strconv.ParseUint(
		portText,
		10,
		16,
	)

	if err != nil ||
		port == 0 ||
		strconv.FormatUint(port, 10) != portText {
		return netip.Addr{}, 0, false
	}

	// Reject malformed authorities, including
	// bracketed IPv4 addresses.
	canonical := ip.String()

	if ip.Is6() {
		canonical = "[" + canonical + "]"
	}

	if explicitPort {
		canonical = net.JoinHostPort(
			ip.String(),
			portText,
		)
	}

	if authority != canonical {
		return netip.Addr{}, 0, false
	}

	return ip, int(port), true
}
