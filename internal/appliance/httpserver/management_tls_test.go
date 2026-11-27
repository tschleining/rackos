package httpserver

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/pki"
)

func TestManagementHTTPSOriginValidation(
	t *testing.T,
) {
	tests := []struct {
		name   string
		url    string
		origin string
		status int
	}{
		{
			"HTTPS same origin",
			"https://127.0.0.1:8080/healthz",
			"https://127.0.0.1:8080",
			http.StatusOK,
		},
		{
			"HTTPS without origin",
			"https://127.0.0.1:8080/healthz",
			"",
			http.StatusOK,
		},
		{
			"wrong scheme",
			"https://127.0.0.1:8080/healthz",
			"http://127.0.0.1:8080",
			http.StatusForbidden,
		},
		{
			"wrong port",
			"https://127.0.0.1:8080/healthz",
			"https://127.0.0.1:5173",
			http.StatusForbidden,
		},
		{
			"foreign origin",
			"https://127.0.0.1:8080/healthz",
			"https://external.example",
			http.StatusForbidden,
		},
		{
			"implicit HTTPS port",
			"https://127.0.0.1/healthz",
			"https://127.0.0.1",
			http.StatusOK,
		},
		{
			"explicit HTTPS default port",
			"https://127.0.0.1:443/healthz",
			"https://127.0.0.1",
			http.StatusOK,
		},
		{
			"IPv6 HTTPS",
			"https://[::1]:8080/healthz",
			"https://[::1]:8080",
			http.StatusOK,
		},
		{
			"IPv6 default port",
			"https://[::1]/healthz",
			"https://[::1]",
			http.StatusOK,
		},
		{
			"HTTPS origin over HTTP",
			"http://127.0.0.1:8080/healthz",
			"https://127.0.0.1:8080",
			http.StatusForbidden,
		},
	}

	handler := protectLocalManagement(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				tc.url,
				nil,
			)

			if tc.origin != "" {
				request.Header.Set(
					"Origin",
					tc.origin,
				)
			}

			request.Header.Set(
				"Sec-Fetch-Site",
				"same-origin",
			)

			recorder := httptest.NewRecorder()

			handler.ServeHTTP(
				recorder,
				request,
			)

			if recorder.Code != tc.status {
				t.Fatalf(
					"expected %d, got %d",
					tc.status,
					recorder.Code,
				)
			}
		})
	}
}

func TestManagementTLSIgnoresForwardedScheme(
	t *testing.T,
) {
	request := httptest.NewRequest(
		http.MethodGet,
		"http://127.0.0.1:8080/healthz",
		nil,
	)

	request.Header.Set(
		"X-Forwarded-Proto",
		"https",
	)

	request.Header.Set(
		"Forwarded",
		"proto=https",
	)

	request.Header.Set(
		"Origin",
		"https://127.0.0.1:8080",
	)

	recorder := httptest.NewRecorder()

	handler := protectLocalManagement(
		http.HandlerFunc(func(
			w http.ResponseWriter,
			_ *http.Request,
		) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatal("forwarded scheme bypassed origin validation")
	}
}

func TestManagementHTTPSIntegration(
	t *testing.T,
) {
	dir := t.TempDir()

	listener, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	address := listener.Addr().String()

	tlsConfig, err := pki.LoadOrCreateManagement(
		dir,
		"management-integration-test",
		address,
	)
	if err != nil {
		t.Fatal(err)
	}

	server := New(
		address,
		"0.1.0-test",
		alwaysReady,
	)

	server.TLSConfig = tlsConfig

	done := make(chan error, 1)

	go func() {
		done <- server.ServeTLS(
			listener,
			"",
			"",
		)
	}()

	defer func() {
		_ = server.Close()
		_ = listener.Close()

		err := <-done

		if err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			t.Errorf("HTTPS server failed: %v", err)
		}
	}()

	caPEM, err := os.ReadFile(
		pki.ManagementCAPath(dir),
	)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()

	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("invalid management CA")
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    roots,
			MinVersion: tls.VersionTLS13,
		},
	}

	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}

	baseURL := "https://" + address

	for _, path := range []string{
		"/healthz",
		"/readyz",
		"/api/v1/version",
	} {
		request, err := http.NewRequest(
			http.MethodGet,
			baseURL+path,
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}

		request.Header.Set(
			"Origin",
			baseURL,
		)

		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()

		if response.StatusCode != http.StatusOK {
			t.Fatalf(
				"%s returned status %d",
				path,
				response.StatusCode,
			)
		}

		if response.TLS == nil ||
			response.TLS.Version != tls.VersionTLS13 {
			t.Fatal("request did not use TLS 1.3")
		}
	}

	// A valid TLS connection does not authorize
	// requests from an untrusted browser origin.
	request, err := http.NewRequest(
		http.MethodGet,
		baseURL+"/healthz",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set(
		"Origin",
		"https://external.example",
	)

	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatal("foreign HTTPS origin accepted")
	}

	// Explicitly use an empty trust store.
	untrustedTransport := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    x509.NewCertPool(),
			MinVersion: tls.VersionTLS13,
		},
	}
	defer untrustedTransport.CloseIdleConnections()

	untrustedClient := &http.Client{
		Transport: untrustedTransport,
		Timeout:   5 * time.Second,
	}

	response, err = untrustedClient.Get(
		baseURL + "/healthz",
	)

	if err == nil {
		response.Body.Close()
		t.Fatal("untrusted certificate was accepted")
	}

	// Plaintext HTTP must not expose the API
	// through an HTTPS-configured listener.
	response, err = client.Get(
		"http://" + address + "/healthz",
	)

	if err == nil {
		response.Body.Close()

		if response.StatusCode == http.StatusOK {
			t.Fatal("HTTPS listener served plaintext API")
		}
	}
}
