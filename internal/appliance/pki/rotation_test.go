package pki

import (
	"bytes"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfiguredServerNames(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadOrCreate(
		dir,
		testInstallationID,
	)
	if err != nil {
		t.Fatal(err)
	}

	caPath := filepath.Join(
		dir, "pki", CAFilename,
	)

	before, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadOrCreate(
		dir,
		testInstallationID,
		"192.168.1.50",
		"rackos.local",
		"rackos.home.arpa",
	)
	if err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(before, after) {
		t.Fatal("CA changed after network reconfiguration")
	}

	pair, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()

	if !roots.AppendCertsFromPEM(after) {
		t.Fatal("invalid CA")
	}

	for _, name := range []string{
		"localhost",
		"127.0.0.1",
		"::1",
		"192.168.1.50",
		"rackos.local",
		"rackos.home.arpa",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := pair.Leaf.Verify(
				x509.VerifyOptions{
					Roots:   roots,
					DNSName: name,
				},
			)
			if err != nil {
				t.Errorf("verify %s: %v", name, err)
			}
		})
	}

	for _, name := range []string{
		"192.168.1.99",
		"wrong.example",
	} {
		if err := pair.Leaf.VerifyHostname(name); err == nil {
			t.Errorf("unexpected valid hostname: %s", name)
		}
	}
}

func TestInvalidServerNames(t *testing.T) {
	for _, name := range []string{
		"",
		"*.example.com",
		"https://rackos.local",
		"rackos.local:50051",
		"bad name",
		"bad_name",
		"-rackos.local",
		"rackos..local",
		"0.0.0.0",
		"::",
		"224.0.0.1",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseServerNames(
				[]string{name},
			)

			if err == nil {
				t.Fatal("invalid TLS name accepted")
			}
		})
	}
}

func TestCertificateRotation(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadOrCreate(
		dir, testInstallationID,
	)
	if err != nil {
		t.Fatal(err)
	}

	authority, err := loadAuthority(
		filepath.Join(
			dir, "pki", privateFilename,
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	ca, err := x509.ParseCertificate(
		authority.Certificate[0],
	)
	if err != nil {
		t.Fatal(err)
	}

	names, err := parseServerNames(
		[]string{"rackos.local"},
	)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()

	cfg, err := newRotatingTLSConfig(
		ca,
		authority,
		names,
		func() time.Time {
			return now
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	first, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	// Before the renewal window, reuse the certificate.
	now = now.Add(30 * 24 * time.Hour)

	second, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(
		first.Certificate[0],
		second.Certificate[0],
	) {
		t.Fatal("certificate rotated too early")
	}

	// Advance into the renewal window.
	now = now.Add(31 * 24 * time.Hour)

	renewed, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(
		first.Certificate[0],
		renewed.Certificate[0],
	) {
		t.Fatal("certificate did not rotate")
	}

	if !renewed.Leaf.NotAfter.After(
		first.Leaf.NotAfter,
	) {
		t.Fatal("renewed certificate did not extend validity")
	}

	roots := x509.NewCertPool()
	roots.AddCert(ca)

	_, err = renewed.Leaf.Verify(
		x509.VerifyOptions{
			Roots:       roots,
			DNSName:     "rackos.local",
			CurrentTime: now,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	// Near CA expiration, shorten the server certificate.
	now = ca.NotAfter.Add(-20 * 24 * time.Hour)

	limited, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	if !limited.Leaf.NotAfter.Equal(ca.NotAfter) {
		t.Fatal("server certificate outlives CA")
	}

	// Do not repeatedly issue certificates whose
	// expiration is already limited by the CA.
	reused, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(
		limited.Certificate[0],
		reused.Certificate[0],
	) {
		t.Fatal("CA-limited certificate was unnecessarily replaced")
	}

	// An expired CA must not serve new connections.
	now = ca.NotAfter

	if _, err := cfg.GetCertificate(nil); err == nil {
		t.Fatal("expired CA accepted")
	}
}
