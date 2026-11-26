package pki

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

const testInstallationID = "test-installation"

func TestLoadOrCreate(t *testing.T) {
	dir := t.TempDir()

	cfg, err := LoadOrCreate(dir, testInstallationID)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatal("expected TLS 1.3")
	}

	if cfg.GetCertificate == nil ||
		len(cfg.Certificates) != 0 {
		t.Fatal("expected dynamic certificate configuration")
	}

	privatePath := filepath.Join(
		dir, "pki", privateFilename,
	)

	info, err := os.Stat(privatePath)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0600 {
		t.Fatal("unexpected CA private file permissions")
	}

	caPEM, err := os.ReadFile(
		filepath.Join(dir, "pki", CAFilename),
	)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()

	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("invalid CA certificate")
	}

	pair, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := x509.ParseCertificate(
		pair.Certificate[0],
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, host := range []string{
		"localhost",
		"127.0.0.1",
		"::1",
	} {
		_, err := cert.Verify(x509.VerifyOptions{
			Roots:   roots,
			DNSName: host,
		})

		if err != nil {
			t.Errorf("verify %s: %v", host, err)
		}
	}

	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:   roots,
		DNSName: "wrong.example",
	}); err == nil {
		t.Fatal("expected hostname verification failure")
	}
}

func TestPersistentAuthority(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pki", CAFilename)

	first, err := LoadOrCreate(dir, testInstallationID)
	if err != nil {
		t.Fatal(err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	second, err := LoadOrCreate(dir, testInstallationID)
	if err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(before, after) {
		t.Fatal("CA changed after restart")
	}

	firstCert, err := first.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	secondCert, err := second.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(
		firstCert.Certificate[0],
		secondCert.Certificate[0],
	) {
		t.Fatal("server certificate was not reissued")
	}
}

func TestIndependentAuthorities(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	for _, dir := range []string{dir1, dir2} {
		if _, err := LoadOrCreate(
			dir, testInstallationID,
		); err != nil {
			t.Fatal(err)
		}
	}

	first, err := os.ReadFile(
		filepath.Join(dir1, "pki", CAFilename),
	)
	if err != nil {
		t.Fatal(err)
	}

	second, err := os.ReadFile(
		filepath.Join(dir2, "pki", CAFilename),
	)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(first, second) {
		t.Fatal("independent appliances share a CA")
	}
}

func TestMissingPrivateKey(t *testing.T) {
	dir := t.TempDir()

	if _, err := LoadOrCreate(
		dir, testInstallationID,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(
		filepath.Join(dir, "pki", privateFilename),
	); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadOrCreate(
		dir, testInstallationID,
	); err == nil {
		t.Fatal("expected missing signing key error")
	}
}

func TestCorruptPrivateKey(t *testing.T) {
	dir := t.TempDir()

	if _, err := LoadOrCreate(
		dir, testInstallationID,
	); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "pki", privateFilename)

	if err := os.WriteFile(
		path, []byte("invalid"), 0600,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadOrCreate(
		dir, testInstallationID,
	); err == nil {
		t.Fatal("expected invalid CA error")
	}
}

func TestWrongInstallation(t *testing.T) {
	dir := t.TempDir()

	if _, err := LoadOrCreate(
		dir, testInstallationID,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadOrCreate(
		dir, "different-installation",
	); err == nil {
		t.Fatal("expected installation mismatch error")
	}
}

func TestRestorePublicCertificate(t *testing.T) {
	dir := t.TempDir()

	if _, err := LoadOrCreate(
		dir, testInstallationID,
	); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "pki", CAFilename)

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadOrCreate(
		dir, testInstallationID,
	); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(before, after) {
		t.Fatal("restored CA differs from original")
	}
}

func TestMismatchedPublicCertificate(t *testing.T) {
	dir := t.TempDir()

	if _, err := LoadOrCreate(
		dir, testInstallationID,
	); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "pki", CAFilename)

	if err := os.WriteFile(
		path, []byte("wrong certificate"), 0600,
	); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadOrCreate(
		dir, testInstallationID,
	); err == nil {
		t.Fatal("expected public CA mismatch error")
	}
}
