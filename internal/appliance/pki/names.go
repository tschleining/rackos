package pki

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

var dnsLabel = regexp.MustCompile(
	`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`,
)

type serverNames struct {
	dns []string
	ips []net.IP
}

func parseServerNames(extra []string) (serverNames, error) {
	if len(extra) > 32 {
		return serverNames{}, fmt.Errorf(
			"too many TLS certificate names",
		)
	}

	names := serverNames{}

	seen := make(map[string]bool)

	values := append(
		[]string{
			"localhost",
			"127.0.0.1",
			"::1",
		},
		extra...,
	)

	for _, value := range values {
		value = strings.ToLower(
			strings.TrimSpace(value),
		)

		if ip := net.ParseIP(value); ip != nil {
			if !ip.IsLoopback() && !ip.IsGlobalUnicast() {
				return serverNames{}, fmt.Errorf(
					"invalid TLS IP address: %q", value,
				)
			}

			key := ip.String()

			if !seen[key] {
				names.ips = append(names.ips, ip)
				seen[key] = true
			}

			continue
		}

		if !validDNSName(value) {
			return serverNames{}, fmt.Errorf(
				"invalid TLS DNS name: %q", value,
			)
		}

		if !seen[value] {
			names.dns = append(names.dns, value)
			seen[value] = true
		}
	}

	return names, nil
}

func validDNSName(value string) bool {
	if len(value) == 0 || len(value) > 253 {
		return false
	}

	for _, label := range strings.Split(value, ".") {
		if !dnsLabel.MatchString(label) {
			return false
		}
	}

	return true
}
