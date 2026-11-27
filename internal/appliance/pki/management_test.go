package pki

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func readManagementTestCA(
	t *testing.T,
	path string,
) ([]byte, *x509.CertPool) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()

	if !roots.AppendCertsFromPEM(data) {
		t.Fatal("invalid certificate authority")
	}

	return data, roots
}

func TestManagementPKIIndependentAuthority(
	t *testing.T,
) {
	dir := t.TempDir()

	agentConfig, err := LoadOrCreate(
		dir,
		testInstallationID,
	)
	if err != nil {
		t.Fatal(err)
	}

	agentCAPath := filepath.Join(
		dir,
		"pki",
		CAFilename,
	)

	agentCA, agentRoots :=
		readManagementTestCA(t, agentCAPath)

	managementConfig, err := LoadOrCreateManagement(
		dir,
		testInstallationID,
		"127.0.0.1:8080",
	)
	if err != nil {
		t.Fatal(err)
	}

	if managementConfig.MinVersion !=
		tls.VersionTLS13 {
		t.Fatal("management TLS must require TLS 1.3")
	}

	if !managementConfig.SessionTicketsDisabled {
		t.Fatal("management TLS session tickets enabled")
	}

	managementCA, managementRoots :=
		readManagementTestCA(
			t,
			ManagementCAPath(dir),
		)

	if bytes.Equal(agentCA, managementCA) {
		t.Fatal("agent and management CAs are identical")
	}

	agentCertificate, err :=
		agentConfig.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	managementCertificate, err :=
		managementConfig.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	if managementCertificate.Leaf.Subject.CommonName !=
		"RackOS Management HTTPS" {
		t.Fatal("incorrect management certificate identity")
	}

	if agentCertificate.Leaf.Subject.CommonName !=
		"RackOS gRPC" {
		t.Fatal("agent certificate identity changed")
	}

	// Management certificate validates against
	// the management CA.
	_, err = managementCertificate.Leaf.Verify(
		x509.VerifyOptions{
			Roots:   managementRoots,
			DNSName: "127.0.0.1",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	// Management certificate must not validate
	// against the agent CA.
	_, err = managementCertificate.Leaf.Verify(
		x509.VerifyOptions{
			Roots:   agentRoots,
			DNSName: "127.0.0.1",
		},
	)
	if err == nil {
		t.Fatal("agent CA accepted management certificate")
	}

	// Agent certificates must not validate
	// against the management CA.
	_, err = agentCertificate.Leaf.Verify(
		x509.VerifyOptions{
			Roots:   managementRoots,
			DNSName: "127.0.0.1",
		},
	)
	if err == nil {
		t.Fatal("management CA accepted agent certificate")
	}

	for _, invalidName := range []string{
		"localhost",
		"::1",
		"127.0.0.2",
		"192.168.1.50",
		"rackos.local",
	} {
		if err := managementCertificate.Leaf.VerifyHostname(
			invalidName,
		); err == nil {
			t.Errorf(
				"unexpected management identity: %s",
				invalidName,
			)
		}
	}

	agentCAAfter, _ := readManagementTestCA(
		t,
		agentCAPath,
	)

	if !bytes.Equal(agentCA, agentCAAfter) {
		t.Fatal("management initialization modified agent CA")
	}
}

func TestManagementPKIPersistentAuthority(
	t *testing.T,
) {
	dir := t.TempDir()

	_, err := LoadOrCreateManagement(
		dir,
		testInstallationID,
		"127.0.0.1:8080",
	)
	if err != nil {
		t.Fatal(err)
	}

	before, _ := readManagementTestCA(
		t,
		ManagementCAPath(dir),
	)

	reopened, err := LoadOrCreateManagement(
		dir,
		testInstallationID,
		"127.0.0.1:8443",
	)
	if err != nil {
		t.Fatal(err)
	}

	after, roots := readManagementTestCA(
		t,
		ManagementCAPath(dir),
	)

	if !bytes.Equal(before, after) {
		t.Fatal("management CA changed after reopening")
	}

	certificate, err := reopened.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = certificate.Leaf.Verify(
		x509.VerifyOptions{
			Roots:   roots,
			DNSName: "127.0.0.1",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestManagementPKIIPv6(t *testing.T) {
	dir := t.TempDir()

	cfg, err := LoadOrCreateManagement(
		dir,
		testInstallationID,
		"[::1]:8080",
	)
	if err != nil {
		t.Fatal(err)
	}

	certificate, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}

	_, roots := readManagementTestCA(
		t,
		ManagementCAPath(dir),
	)

	_, err = certificate.Leaf.Verify(
		x509.VerifyOptions{
			Roots:   roots,
			DNSName: "::1",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestManagementPKIRejectsInvalidAddresses(
	t *testing.T,
) {
	for _, address := range []string{
		"",
		"localhost:8080",
		"0.0.0.0:8080",
		"[::]:8080",
		"192.168.1.50:8080",
		"[::ffff:127.0.0.1]:8080",
		"[::1%lo0]:8080",
		"[127.0.0.1]:8080",
		"127.0.0.1",
		"127.0.0.1:abc",
		"127.0.0.1:-1",
		"127.0.0.1:08080",
		"127.0.0.1:65536",
	} {
		t.Run(address, func(t *testing.T) {
			dir := t.TempDir()

			_, err := LoadOrCreateManagement(
				dir,
				testInstallationID,
				address,
			)
			if err == nil {
				t.Fatal("invalid management address accepted")
			}

			if _, err := os.Stat(
				ManagementCAPath(dir),
			); !os.IsNotExist(err) {
				t.Fatal("invalid address created a CA")
			}
		})
	}
}

func TestManagementPKIMissingSigningKey(
	t *testing.T,
) {
	dir := t.TempDir()

	_, err := LoadOrCreateManagement(
		dir,
		testInstallationID,
		"127.0.0.1:8080",
	)
	if err != nil {
		t.Fatal(err)
	}

	before, _ := readManagementTestCA(
		t,
		ManagementCAPath(dir),
	)

	keyPath := filepath.Join(
		dir,
		ManagementDirectory,
		privateFilename,
	)

	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}

	_, err = LoadOrCreateManagement(
		dir,
		testInstallationID,
		"127.0.0.1:8080",
	)
	if err == nil {
		t.Fatal("missing signing key was accepted")
	}

	after, _ := readManagementTestCA(
		t,
		ManagementCAPath(dir),
	)

	if !bytes.Equal(before, after) {
		t.Fatal("public CA certificate was replaced")
	}
}
