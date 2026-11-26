package grpcserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/appliance/pki"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func TestRejectNonLoopbackAddresses(t *testing.T) {
	addresses := []string{
		"0.0.0.0:50051",
		":50051",
		"192.168.1.100:50051",
		"[::]:50051",
		"localhost:50051",
		"invalid",
		"127.0.0.2:50051",
	}

	for _, addr := range addresses {
		t.Run(addr, func(t *testing.T) {
			listener, err := Listen(addr)

			if err == nil {
				listener.Close()
				t.Fatal("expected address rejection")
			}
		})
	}
}

func TestGRPCServer(t *testing.T) {

	dataDir := t.TempDir()

	tlsConfig, err := pki.LoadOrCreate(
		dataDir,
		"test-appliance",
	)
	if err != nil {
		t.Fatal(err)
	}

	caPEM, err := os.ReadFile(
		filepath.Join(dataDir, "pki", pki.CAFilename),
	)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()

	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("failed to load trusted CA")
	}

	server := New(tlsConfig)

	defer server.server.Stop()

	listener, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverErr := make(chan error, 1)

	go func() {
		serverErr <- server.Serve(listener)
	}()

	conn, err := grpc.NewClient(
		"passthrough:///"+listener.Addr().String(),
		grpc.WithTransportCredentials(
			credentials.NewTLS(&tls.Config{
				MinVersion: tls.VersionTLS13,
				RootCAs:    roots,
				ServerName: "127.0.0.1",
			}),
		),
	)

	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	healthClient := healthpb.NewHealthClient(conn)

	t.Run("server health", func(t *testing.T) {
		response, err := healthClient.Check(
			ctx,
			&healthpb.HealthCheckRequest{},
		)

		if err != nil {
			t.Fatal(err)
		}

		if response.GetStatus() !=
			healthpb.HealthCheckResponse_SERVING {
			t.Fatalf(
				"unexpected health status: %s",
				response.GetStatus(),
			)
		}
	})

	t.Run("agent service not ready", func(t *testing.T) {
		response, err := healthClient.Check(
			ctx,
			&healthpb.HealthCheckRequest{
				Service: rackosv1.AgentService_ServiceDesc.ServiceName,
			},
		)

		if err != nil {
			t.Fatal(err)
		}

		if response.GetStatus() !=
			healthpb.HealthCheckResponse_NOT_SERVING {
			t.Fatalf(
				"unexpected agent service status: %s",
				response.GetStatus(),
			)
		}
	})

	agentClient := rackosv1.NewAgentServiceClient(conn)

	t.Run("enrollment disabled", func(t *testing.T) {
		_, err := agentClient.Enroll(
			ctx,
			&rackosv1.EnrollRequest{
				Device: &rackosv1.DeviceInfo{
					Hostname:     "server-01",
					Os:           "linux",
					Architecture: "amd64",
					AgentVersion: "0.1.0-dev",
				},
			},
		)

		if status.Code(err) != codes.Unimplemented {
			t.Fatalf(
				"expected Unimplemented, got %v",
				err,
			)
		}
	})

	t.Run("heartbeat disabled", func(t *testing.T) {
		_, err := agentClient.Heartbeat(
			ctx,
			&rackosv1.HeartbeatRequest{
				DeviceId: "test-device",
			},
		)

		if status.Code(err) != codes.Unimplemented {
			t.Fatalf(
				"expected Unimplemented, got %v",
				err,
			)
		}
	})

	t.Run("reject insecure connections", func(t *testing.T) {
		tests := []struct {
			name string
			cred credentials.TransportCredentials
		}{
			{
				name: "plaintext",
				cred: insecure.NewCredentials(),
			},
			{
				name: "untrusted CA",
				cred: credentials.NewTLS(&tls.Config{
					MinVersion: tls.VersionTLS13,
					RootCAs:    x509.NewCertPool(),
					ServerName: "127.0.0.1",
				}),
			},
			{
				name: "wrong hostname",
				cred: credentials.NewTLS(&tls.Config{
					MinVersion: tls.VersionTLS13,
					RootCAs:    roots,
					ServerName: "wrong.example",
				}),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				badConn, err := grpc.NewClient(
					"passthrough:///"+listener.Addr().String(),
					grpc.WithTransportCredentials(tt.cred),
				)
				if err != nil {
					t.Fatal(err)
				}
				defer badConn.Close()

				testCtx, cancel := context.WithTimeout(
					context.Background(),
					3*time.Second,
				)
				defer cancel()

				client := healthpb.NewHealthClient(badConn)

				_, err = client.Check(
					testCtx,
					&healthpb.HealthCheckRequest{},
				)

				if err == nil {
					t.Fatal("insecure connection was accepted")
				}
			})
		}
	})

	t.Run("graceful shutdown", func(t *testing.T) {
		shutdownCtx, shutdownCancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			t.Fatal(err)
		}

		select {
		case err := <-serverErr:
			if err != nil {
				t.Fatal(err)
			}

		case <-shutdownCtx.Done():
			t.Fatal("gRPC server did not stop")
		}
	})
}
