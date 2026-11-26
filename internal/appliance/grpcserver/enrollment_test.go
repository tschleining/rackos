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
	"github.com/tschleining/rackos/internal/appliance/agentservice"
	agentcredentials "github.com/tschleining/rackos/internal/appliance/credentials"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/enrollment"
	"github.com/tschleining/rackos/internal/appliance/pki"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestLiveAgentEnrollment(t *testing.T) {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	dataDir := t.TempDir()
	installationID := "test-appliance"

	db, err := database.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tlsConfig, err := pki.LoadOrCreate(
		dataDir, installationID,
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
		t.Fatal("invalid trusted CA")
	}

	service := agentservice.New(db, installationID)
	server := New(tlsConfig, service)
	defer server.server.Stop()

	listener, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		_ = server.Serve(listener)
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

	client := rackosv1.NewAgentServiceClient(conn)
	repo := devices.NewRepository(db)

	healthClient := healthpb.NewHealthClient(conn)

	health, err := healthClient.Check(
		ctx,
		&healthpb.HealthCheckRequest{
			Service: rackosv1.AgentService_ServiceDesc.ServiceName,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if health.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatal("agent service is not serving")
	}

	request := &rackosv1.EnrollRequest{
		Device: &rackosv1.DeviceInfo{
			Hostname:     "integration-server",
			Os:           "linux",
			Architecture: "amd64",
			AgentVersion: "0.1.0-dev",
		},
	}

	withToken := func(token string) context.Context {
		return metadata.AppendToOutgoingContext(
			ctx,
			"authorization",
			"Bearer "+token,
		)
	}

	// Reject enrollment without credentials.
	_, err = client.Enroll(ctx, request)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected authentication rejection: %v", err)
	}

	// Issue a legitimate enrollment invitation.
	invitation, err := enrollment.Issue(
		ctx, db, enrollment.DefaultTTL,
	)
	if err != nil {
		t.Fatal(err)
	}

	enrollCtx := withToken(invitation.Token)

	// Invalid device information must not consume the token.
	_, err = client.Enroll(
		enrollCtx,
		&rackosv1.EnrollRequest{},
	)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected invalid argument: %v", err)
	}

	// Simulate a failure when issuing the agent credential.
	_, err = db.ExecContext(ctx, `
		CREATE TRIGGER test_credential_failure
		BEFORE INSERT ON agent_credentials
		BEGIN
			SELECT RAISE(ABORT, 'test failure');
		END;
	`)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Enroll(enrollCtx, request)

	if status.Code(err) != codes.Internal {
		t.Fatalf("expected enrollment failure: %v", err)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(list) != 0 {
		t.Fatal("failed enrollment left an orphaned device")
	}

	_, err = db.ExecContext(
		ctx,
		"DROP TRIGGER test_credential_failure",
	)
	if err != nil {
		t.Fatal(err)
	}

	// The same token must still work after rollback.
	response, err := client.Enroll(enrollCtx, request)
	if err != nil {
		t.Fatal(err)
	}

	deviceID := response.GetDeviceId()
	agentToken := response.GetAgentToken()

	if deviceID == "" || agentToken == "" {
		t.Fatal("missing enrollment credentials")
	}

	if response.GetInstallationId() != installationID {
		t.Fatal("incorrect installation ID")
	}

	if response.GetHeartbeatIntervalSeconds() != 30 {
		t.Fatal("incorrect heartbeat interval")
	}

	device, err := repo.Get(ctx, deviceID)
	if err != nil {
		t.Fatal(err)
	}

	if device.Hostname != "integration-server" {
		t.Fatal("incorrect stored hostname")
	}

	// Reject enrollment token replay.
	_, err = client.Enroll(enrollCtx, request)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("consumed token accepted: %v", err)
	}

	heartbeat := &rackosv1.HeartbeatRequest{
		DeviceId: deviceID,
	}

	// Reject unauthenticated heartbeats.
	_, err = client.Heartbeat(ctx, heartbeat)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated heartbeat accepted: %v", err)
	}

	// Enrollment tokens cannot authenticate heartbeats.
	_, err = client.Heartbeat(enrollCtx, heartbeat)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("enrollment token accepted as agent token: %v", err)
	}

	agentCtx := withToken(agentToken)

	// Reject valid credentials used with another device ID.
	_, err = client.Heartbeat(
		agentCtx,
		&rackosv1.HeartbeatRequest{
			DeviceId: "unknown-device",
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("wrong device accepted: %v", err)
	}

	// Simulate a device that has not contacted us recently.
	_, err = db.ExecContext(
		ctx,
		"UPDATE devices SET last_seen_at = 0 WHERE id = ?",
		deviceID,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Accept a legitimate heartbeat.
	ack, err := client.Heartbeat(agentCtx, heartbeat)
	if err != nil {
		t.Fatal(err)
	}

	if ack.GetHeartbeatIntervalSeconds() != 30 {
		t.Fatal("incorrect heartbeat response")
	}

	updated, err := repo.Get(ctx, deviceID)
	if err != nil {
		t.Fatal(err)
	}

	if updated.LastSeenAt.UnixMilli() <= 0 {
		t.Fatal("heartbeat did not update last seen")
	}

	if updated.Status(
		time.Now(),
		devices.DefaultOfflineTimeout,
	) != devices.StatusOnline {
		t.Fatal("device should be online")
	}

	// Revoke the registered agent's credential.
	if err := agentcredentials.Revoke(
		ctx, db, deviceID,
	); err != nil {
		t.Fatal(err)
	}

	_, err = client.Heartbeat(agentCtx, heartbeat)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("revoked credential accepted: %v", err)
	}

	final, err := repo.Get(ctx, deviceID)
	if err != nil {
		t.Fatal(err)
	}

	if !final.LastSeenAt.Equal(updated.LastSeenAt) {
		t.Fatal("unauthorized heartbeat updated device")
	}
}
