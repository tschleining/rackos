package pki

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	CAFilename      = "ca.crt"
	privateFilename = "ca-private.pem"
	serverLifetime  = 90 * 24 * time.Hour
)

func LoadOrCreate(
	dataDir string,
	installationID string,
) (*tls.Config, error) {
	dir := filepath.Join(dataDir, "pki")

	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create PKI directory: %w", err)
	}

	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New(
			"PKI directory must be private",
		)
	}

	privatePath := filepath.Join(dir, privateFilename)
	publicPath := filepath.Join(dir, CAFilename)

	commonName := "RackOS CA " + installationID
	now := time.Now().UTC()

	authority, err := loadAuthority(privatePath)

	if errors.Is(err, os.ErrNotExist) {
		// Never silently replace a missing CA signing key
		// if its public certificate still exists.
		if _, statErr := os.Lstat(publicPath); statErr == nil {
			return nil, errors.New(
				"CA signing key is missing",
			)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return nil, statErr
		}

		bundle, createErr := createAuthority(
			commonName, now,
		)
		if createErr != nil {
			return nil, createErr
		}

		if err := writeNew(privatePath, bundle); err != nil {
			return nil, fmt.Errorf("persist CA: %w", err)
		}

		authority, err = loadAuthority(privatePath)
	}

	if err != nil {
		return nil, fmt.Errorf("load CA: %w", err)
	}

	ca, err := x509.ParseCertificate(
		authority.Certificate[0],
	)
	if err != nil {
		return nil, err
	}

	if !ca.IsCA ||
		!ca.BasicConstraintsValid ||
		ca.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, errors.New("invalid CA certificate")
	}

	if err := ca.CheckSignatureFrom(ca); err != nil {
		return nil, fmt.Errorf("verify CA: %w", err)
	}

	if ca.Subject.CommonName != commonName {
		return nil, errors.New(
			"CA belongs to a different installation",
		)
	}

	if now.Before(ca.NotBefore) ||
		!ca.NotAfter.After(now.Add(serverLifetime)) {
		return nil, errors.New(
			"CA is not valid for the server certificate lifetime",
		)
	}

	publicPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ca.Raw,
	})

	if err := ensurePublicCertificate(
		publicPath, publicPEM,
	); err != nil {
		return nil, err
	}

	serverCert, err := issueServerCertificate(
		ca, authority, now,
	)
	if err != nil {
		return nil, fmt.Errorf("issue server certificate: %w", err)
	}

	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{serverCert},
	}, nil
}

func loadAuthority(path string) (tls.Certificate, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return tls.Certificate{}, err
	}

	if !info.Mode().IsRegular() ||
		info.Mode().Perm()&0077 != 0 {
		return tls.Certificate{}, errors.New(
			"CA private key file must be private and regular",
		)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return tls.Certificate{}, err
	}

	pair, err := tls.X509KeyPair(data, data)
	if err != nil {
		return tls.Certificate{}, err
	}

	if len(pair.Certificate) != 1 {
		return tls.Certificate{}, errors.New(
			"invalid CA certificate bundle",
		)
	}

	key, ok := pair.PrivateKey.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return tls.Certificate{}, errors.New(
			"unsupported CA private key",
		)
	}

	return pair, nil
}

func createAuthority(
	commonName string,
	now time.Time,
) ([]byte, error) {
	key, err := ecdsa.GenerateKey(
		elliptic.P256(), rand.Reader,
	)
	if err != nil {
		return nil, err
	}

	serial, err := serialNumber()
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign,
	}

	der, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&key.PublicKey,
		key,
	)
	if err != nil {
		return nil, err
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: der,
	})

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyDER,
	})

	return append(certPEM, keyPEM...), nil
}

func issueServerCertificate(
	ca *x509.Certificate,
	authority tls.Certificate,
	now time.Time,
) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(
		elliptic.P256(), rand.Reader,
	)
	if err != nil {
		return tls.Certificate{}, err
	}

	serial, err := serialNumber()
	if err != nil {
		return tls.Certificate{}, err
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "RackOS gRPC",
		},
		NotBefore: now.Add(-5 * time.Minute),
		NotAfter:  now.Add(serverLifetime),
		KeyUsage:  x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses: []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("::1"),
		},
	}

	der, err := x509.CreateCertificate(
		rand.Reader,
		template,
		ca,
		&key.PublicKey,
		authority.PrivateKey,
	)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
	}, nil
}

func serialNumber() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)

	number, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, err
	}

	return number.Add(number, big.NewInt(1)), nil
}

func ensurePublicCertificate(
	path string,
	expected []byte,
) error {
	stored, err := os.ReadFile(path)

	if errors.Is(err, os.ErrNotExist) {
		return writeNew(path, expected)
	}

	if err != nil {
		return err
	}

	if !bytes.Equal(stored, expected) {
		return errors.New(
			"public CA certificate does not match signing key",
		)
	}

	return nil
}

// writeNew publishes a complete file without overwriting
// an existing identity or certificate.
func writeNew(path string, data []byte) error {
	file, err := os.CreateTemp(
		filepath.Dir(path), ".pki-*",
	)
	if err != nil {
		return err
	}

	defer os.Remove(file.Name())
	defer file.Close()

	if err := file.Chmod(0600); err != nil {
		return err
	}

	if _, err := file.Write(data); err != nil {
		return err
	}

	if err := file.Sync(); err != nil {
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	return os.Link(file.Name(), path)
}
