package pki

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"
	"time"
)

const renewBefore = 30 * 24 * time.Hour

func newRotatingTLSConfig(
	ca *x509.Certificate,
	authority tls.Certificate,
	names serverNames,
	clock func() time.Time,
) (*tls.Config, error) {
	var mu sync.Mutex
	var current *tls.Certificate

	getCertificate := func(
		_ *tls.ClientHelloInfo,
	) (*tls.Certificate, error) {
		mu.Lock()
		defer mu.Unlock()

		now := clock().UTC()

		if now.Before(ca.NotBefore) ||
			!now.Before(ca.NotAfter) {
			return nil, errors.New(
				"appliance CA is not currently valid",
			)
		}

		if current != nil {
			leaf := current.Leaf

			valid := !now.Before(leaf.NotBefore) &&
				now.Before(leaf.NotAfter)

			renewalDue := !now.Before(
				leaf.NotAfter.Add(-renewBefore),
			)

			limitedByCA := leaf.NotAfter.Equal(
				ca.NotAfter,
			)

			if valid && (!renewalDue || limitedByCA) {
				return current, nil
			}
		}

		replacement, err := issueServerCertificate(
			ca,
			authority,
			names,
			now,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"issue server certificate: %w", err,
			)
		}

		current = &replacement

		return current, nil
	}

	// Issue the initial certificate during startup so
	// certificate generation failures prevent startup.
	if _, err := getCertificate(nil); err != nil {
		return nil, err
	}

	return &tls.Config{
		MinVersion:             tls.VersionTLS13,
		GetCertificate:         getCertificate,
		SessionTicketsDisabled: true,
	}, nil
}
