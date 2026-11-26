package client

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/agentservice"
	"github.com/tschleining/rackos/internal/appliance/credentials"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/enrollment"
	"github.com/tschleining/rackos/internal/appliance/grpcserver"
	"github.com/tschleining/rackos/internal/appliance/pki"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestLocalDeviceInfo(t *testing.T) {
	info, err := LocalDeviceInfo("0.1.0-dev")
	if err != nil {
		t.Fatal(err)
	}

	if info.GetHostname() == "" ||
		info.GetOs() != runtime.GOOS ||
		info.GetArchitecture() != runtime.GOARCH ||
		info.GetAgentVersion() != "0.1.0-dev" {
		t.Fatal("incorrect local device information")
	}
}

func TestBackoff(t *testing.T) {
	current := initialBackoff

	expected := []time.Duration{
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		time.Minute,
		time.Minute,
	}

	for _, want := range expected {
		current = nextBackoff(current)

		if current != want {
			t.Fatalf(
				"expected backoff %v, got %v",
				want, current,
			)
		}
	}
}

func TestCancelledWait(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	if wait(ctx, time.Hour) {
		t.Fatal("cancelled wait unexpectedly completed")
	}
}

func TestAgentClientEnrollment(t *testing.T) {
	if runtime.GOOS != "linux" &&
		runtime.GOOS != "darwin" {
		t.Skip("appliance PKI test platform not supported")
	}

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

	const installationID = "client-test-appliance"

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

	server := grpcserver.New(
		tlsConfig,
		agentservice.New(db, installationID),
	)

	listener, err := grpcserver.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		_ = server.Serve(listener)
	}()

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			t.Error(err)
		}
	}()

	connection, err := New(
		listener.Addr().String(),
		caPEM,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	invitation, err := enrollment.Issue(
		ctx, db, enrollment.DefaultTTL,
	)
	if err != nil {
		t.Fatal(err)
	}

	response, err := connection.Enroll(
		ctx,
		invitation.Token,
		"0.1.0-dev",
	)
	if err != nil {
		t.Fatal(err)
	}

	if response.GetInstallationId() != installationID {
		t.Fatal("incorrect appliance identity")
	}

	deviceID := response.GetDeviceId()
	token := response.GetAgentToken()

	if deviceID == "" || token == "" {
		t.Fatal("missing agent identity")
	}

	repo := devices.NewRepository(db)

	device, err := repo.Get(ctx, deviceID)
	if err != nil {
		t.Fatal(err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}

	if device.Hostname != hostname ||
		device.OS != runtime.GOOS ||
		device.Architecture != runtime.GOARCH {
		t.Fatal("incorrect enrolled device information")
	}

	// Simulate an offline device.
	_, err = db.ExecContext(
		ctx,
		"UPDATE devices SET last_seen_at = 0 WHERE id = ?",
		deviceID,
	)
	if err != nil {
		t.Fatal(err)
	}

	interval, err := connection.Heartbeat(
		ctx, deviceID, token,
	)
	if err != nil {
		t.Fatal(err)
	}

	if interval != 30 {
		t.Fatal("incorrect heartbeat interval")
	}

	updated, err := repo.Get(ctx, deviceID)
	if err != nil {
		t.Fatal(err)
	}

	if updated.LastSeenAt.UnixMilli() <= 0 {
		t.Fatal("heartbeat did not update device")
	}

	// Revoke the credential and verify that the agent
	// can no longer authenticate.
	if err := credentials.Revoke(
		ctx, db, deviceID,
	); err != nil {
		t.Fatal(err)
	}

	_, err = connection.Heartbeat(
		ctx, deviceID, token,
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("revoked credential accepted: %v", err)
	}

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	if err := connection.Run(
		ctx, deviceID, token, logger,
	); err == nil {
		t.Fatal("agent loop did not reject revoked credentials")
	}
}
