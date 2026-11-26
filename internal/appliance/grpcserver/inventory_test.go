package grpcserver_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	agentclient "github.com/tschleining/rackos/internal/agent/client"
	agentinventory "github.com/tschleining/rackos/internal/agent/inventory"
	"github.com/tschleining/rackos/internal/appliance/agentservice"
	agentcredentials "github.com/tschleining/rackos/internal/appliance/credentials"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/enrollment"
	"github.com/tschleining/rackos/internal/appliance/grpcserver"
	"github.com/tschleining/rackos/internal/appliance/identity"
	"github.com/tschleining/rackos/internal/appliance/pki"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestInventoryReportingIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	dir := t.TempDir()

	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	installationID, err := identity.LoadOrCreate(ctx, db)
	if err != nil {
		t.Fatal(err)
	}

	tlsConfig, err := pki.LoadOrCreate(
		dir,
		installationID,
	)
	if err != nil {
		t.Fatal(err)
	}

	caPEM, err := os.ReadFile(
		filepath.Join(dir, "pki", pki.CAFilename),
	)
	if err != nil {
		t.Fatal(err)
	}

	service := agentservice.New(db, installationID)

	server := grpcserver.New(tlsConfig, service)

	listener, err := grpcserver.Listen(
		"127.0.0.1:0",
	)
	if err != nil {
		t.Fatal(err)
	}

	serveDone := make(chan error, 1)

	go func() {
		serveDone <- server.Serve(listener)
	}()

	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(
			context.Background(),
			3*time.Second,
		)
		defer shutdownCancel()

		server.Shutdown(shutdownCtx)

		select {
		case <-serveDone:
		case <-shutdownCtx.Done():
			t.Error("gRPC server failed to shut down")
		}
	}()

	address := listener.Addr().String()

	roots := x509.NewCertPool()

	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("failed to load appliance CA")
	}

	connection, err := grpc.NewClient(
		"passthrough:///"+address,
		grpc.WithTransportCredentials(
			credentials.NewTLS(
				&tls.Config{
					RootCAs:    roots,
					ServerName: "127.0.0.1",
					MinVersion: tls.VersionTLS13,
				},
			),
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	rpc := rackosv1.NewAgentServiceClient(connection)

	client, err := agentclient.New(address, caPEM)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Enroll two independent test devices.
	enrollDevice := func(
		hostname string,
	) *rackosv1.EnrollResponse {
		t.Helper()

		issued, err := enrollment.Issue(
			ctx,
			db,
			15*time.Minute,
		)
		if err != nil {
			t.Fatal(err)
		}

		response, err := rpc.Enroll(
			inventoryAuthContext(ctx, issued.Token),
			&rackosv1.EnrollRequest{
				Device: &rackosv1.DeviceInfo{
					Hostname:     hostname,
					Os:           "darwin",
					Architecture: "arm64",
					AgentVersion: "0.1.0-test",
				},
			},
		)
		if err != nil {
			t.Fatal(err)
		}

		if response.GetDeviceId() == "" ||
			response.GetAgentToken() == "" {
			t.Fatal("incomplete enrollment response")
		}

		return response
	}

	first := enrollDevice("inventory-test-1.local")
	second := enrollDevice("inventory-test-2.local")

	now := time.Now().UTC()
	bootTime := now.Add(-24 * time.Hour)

	snapshot := agentinventory.Inventory{
		Hostname:        "inventory-test-1.local",
		OS:              "darwin",
		Architecture:    "arm64",
		LogicalCPUs:     14,
		HostLogicalCPUs: 14,
		PhysicalCores:   14,
		CPUModel:        "Apple M4 Pro",
		CPUManufacturer: "Apple",
		MemoryBytes:     25769803776,
		HardwareModel:   "Mac16,8",
		OSVersion:       "26.6.2",
		KernelVersion:   "25.6.0",
		BootTime:        &bootTime,
		CollectedAt:     now,
	}

	newRequest := func(
		deviceID string,
	) *rackosv1.ReportInventoryRequest {
		return &rackosv1.ReportInventoryRequest{
			DeviceId: deviceID,
			Inventory: &rackosv1.HardwareInventory{
				Hostname:              "test-device.local",
				Os:                    "darwin",
				Architecture:          "arm64",
				LogicalCpus:           14,
				HostLogicalCpus:       14,
				PhysicalCores:         14,
				MemoryBytes:           25769803776,
				BootTimeUnixMillis:    bootTime.UnixMilli(),
				CollectedAtUnixMillis: now.UnixMilli(),
			},
		}
	}

	repository := devices.NewRepository(db)

	t.Run("authenticated agent reporting", func(t *testing.T) {
		before, err := repository.Get(
			ctx,
			first.GetDeviceId(),
		)
		if err != nil {
			t.Fatal(err)
		}

		err = client.ReportInventory(
			ctx,
			first.GetDeviceId(),
			first.GetAgentToken(),
			snapshot,
		)
		if err != nil {
			t.Fatal(err)
		}

		after, err := repository.Get(
			ctx,
			first.GetDeviceId(),
		)
		if err != nil {
			t.Fatal(err)
		}

		if !before.LastSeenAt.Equal(after.LastSeenAt) ||
			!before.UpdatedAt.Equal(after.UpdatedAt) {
			t.Fatal("inventory reporting modified device timestamps")
		}
	})

	t.Run("direct inventory acceptance", func(t *testing.T) {
		response, err := rpc.ReportInventory(
			inventoryAuthContext(
				ctx,
				first.GetAgentToken(),
			),
			newRequest(first.GetDeviceId()),
		)
		if err != nil {
			t.Fatal(err)
		}

		if !response.GetAccepted() {
			t.Fatal("valid inventory was not accepted")
		}
	})

	t.Run("missing credentials", func(t *testing.T) {
		_, err := rpc.ReportInventory(
			ctx,
			newRequest(first.GetDeviceId()),
		)

		requireInventoryCode(
			t, err, codes.Unauthenticated,
		)
	})

	t.Run("invalid credentials", func(t *testing.T) {
		_, err := rpc.ReportInventory(
			inventoryAuthContext(
				ctx,
				"invalid-credential",
			),
			newRequest(first.GetDeviceId()),
		)

		requireInventoryCode(
			t, err, codes.Unauthenticated,
		)
	})

	t.Run("duplicate authorization", func(t *testing.T) {
		authCtx := metadata.NewOutgoingContext(
			ctx,
			metadata.Pairs(
				"authorization",
				"Bearer "+first.GetAgentToken(),
				"authorization",
				"Bearer "+first.GetAgentToken(),
			),
		)

		_, err := rpc.ReportInventory(
			authCtx,
			newRequest(first.GetDeviceId()),
		)

		requireInventoryCode(
			t, err, codes.Unauthenticated,
		)
	})

	t.Run("cross-device authentication", func(t *testing.T) {
		_, err := rpc.ReportInventory(
			inventoryAuthContext(
				ctx,
				first.GetAgentToken(),
			),
			newRequest(second.GetDeviceId()),
		)

		requireInventoryCode(
			t, err, codes.Unauthenticated,
		)
	})

	t.Run("authentication before validation", func(t *testing.T) {
		request := newRequest(first.GetDeviceId())
		request.Inventory = nil

		_, err := rpc.ReportInventory(ctx, request)

		requireInventoryCode(
			t, err, codes.Unauthenticated,
		)
	})

	t.Run("missing inventory", func(t *testing.T) {
		request := newRequest(first.GetDeviceId())
		request.Inventory = nil

		_, err := rpc.ReportInventory(
			inventoryAuthContext(
				ctx,
				first.GetAgentToken(),
			),
			request,
		)

		requireInventoryCode(
			t, err, codes.InvalidArgument,
		)
	})

	t.Run("invalid CPU topology", func(t *testing.T) {
		request := newRequest(first.GetDeviceId())
		request.Inventory.PhysicalCores = 100

		_, err := rpc.ReportInventory(
			inventoryAuthContext(
				ctx,
				first.GetAgentToken(),
			),
			request,
		)

		requireInventoryCode(
			t, err, codes.InvalidArgument,
		)
	})

	t.Run("future inventory timestamp", func(t *testing.T) {
		request := newRequest(first.GetDeviceId())

		request.Inventory.CollectedAtUnixMillis =
			time.Now().Add(time.Hour).UnixMilli()

		_, err := rpc.ReportInventory(
			inventoryAuthContext(
				ctx,
				first.GetAgentToken(),
			),
			request,
		)

		requireInventoryCode(
			t, err, codes.InvalidArgument,
		)
	})

	t.Run("heartbeat remains functional", func(t *testing.T) {
		response, err := rpc.Heartbeat(
			inventoryAuthContext(
				ctx,
				first.GetAgentToken(),
			),
			&rackosv1.HeartbeatRequest{
				DeviceId: first.GetDeviceId(),
			},
		)
		if err != nil {
			t.Fatal(err)
		}

		if response.GetHeartbeatIntervalSeconds() != 30 {
			t.Fatal("unexpected heartbeat interval")
		}
	})

	t.Run("revoked credential rejected", func(t *testing.T) {
		err := agentcredentials.Revoke(
			ctx,
			db,
			first.GetDeviceId(),
		)
		if err != nil {
			t.Fatal(err)
		}

		err = client.ReportInventory(
			ctx,
			first.GetDeviceId(),
			first.GetAgentToken(),
			snapshot,
		)

		requireInventoryCode(
			t, err, codes.Unauthenticated,
		)
	})

	t.Run("unrelated device remains authorized", func(t *testing.T) {
		response, err := rpc.ReportInventory(
			inventoryAuthContext(
				ctx,
				second.GetAgentToken(),
			),
			newRequest(second.GetDeviceId()),
		)
		if err != nil {
			t.Fatal(err)
		}

		if !response.GetAccepted() {
			t.Fatal("unrelated device rejected")
		}
	})

	t.Run("removed device rejected", func(t *testing.T) {
		err := repository.Delete(
			ctx,
			second.GetDeviceId(),
		)
		if err != nil {
			t.Fatal(err)
		}

		_, err = rpc.ReportInventory(
			inventoryAuthContext(
				ctx,
				second.GetAgentToken(),
			),
			newRequest(second.GetDeviceId()),
		)

		requireInventoryCode(
			t, err, codes.Unauthenticated,
		)
	})
}

func inventoryAuthContext(
	ctx context.Context,
	token string,
) context.Context {
	return metadata.NewOutgoingContext(
		ctx,
		metadata.Pairs(
			"authorization",
			"Bearer "+token,
		),
	)
}

func requireInventoryCode(
	t *testing.T,
	err error,
	expected codes.Code,
) {
	t.Helper()

	if actual := status.Code(err); actual != expected {
		t.Fatalf(
			"expected gRPC status %s, got %s: %v",
			expected,
			actual,
			err,
		)
	}
}
