package client

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/agent/metrics"
	"github.com/tschleining/rackos/internal/agent/state"
	"github.com/tschleining/rackos/internal/appliance/agentservice"
	"github.com/tschleining/rackos/internal/appliance/credentials"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/enrollment"
	"github.com/tschleining/rackos/internal/appliance/grpcserver"
	"github.com/tschleining/rackos/internal/appliance/identity"
	"github.com/tschleining/rackos/internal/appliance/performance"
	"github.com/tschleining/rackos/internal/appliance/pki"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// performanceE2EService wraps the real appliance
// service and optionally simulates the loss of the
// first successful performance acknowledgement.
type performanceE2EService struct {
	*agentservice.Service

	dropFirstAcknowledgement bool
	dropped                  atomic.Bool
}

func (s *performanceE2EService) ReportPerformance(
	ctx context.Context,
	request *rackosv1.ReportPerformanceRequest,
) (*rackosv1.ReportPerformanceResponse, error) {
	response, err := s.Service.ReportPerformance(
		ctx,
		request,
	)
	if err != nil {
		return nil, err
	}

	// The actual appliance handler has already
	// committed the historical record.
	if s.dropFirstAcknowledgement &&
		s.dropped.CompareAndSwap(false, true) {
		return nil, status.Error(
			codes.Unavailable,
			"simulated acknowledgement loss after commit",
		)
	}

	return response, nil
}

type performanceE2EAppliance struct {
	db             *sql.DB
	address        string
	caPEM          []byte
	installationID string
	service        *performanceE2EService
}

// startPerformanceE2EAppliance starts a real TLS
// appliance server backed by a persistent database.
//
// Cleanup closes the server before the database.
func startPerformanceE2EAppliance(
	t *testing.T,
	ctx context.Context,
	dir string,
	dropAcknowledgement bool,
) *performanceE2EAppliance {
	t.Helper()

	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})

	installationID, err := identity.LoadOrCreate(
		ctx,
		db,
	)
	if err != nil {
		t.Fatal(err)
	}

	tlsConfig, err := pki.LoadOrCreate(
		dir,
		installationID,
		"127.0.0.1",
	)
	if err != nil {
		t.Fatal(err)
	}

	caPEM, err := os.ReadFile(
		filepath.Join(
			dir,
			"pki",
			pki.CAFilename,
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	service := &performanceE2EService{
		Service: agentservice.New(
			db,
			installationID,
		),

		dropFirstAcknowledgement: dropAcknowledgement,
	}

	server := grpcserver.New(
		tlsConfig,
		service,
	)

	listener, err := grpcserver.Listen(
		"127.0.0.1:0",
	)
	if err != nil {
		t.Fatal(err)
	}

	address := listener.Addr().String()

	serveDone := make(chan error, 1)

	go func() {
		serveDone <- server.Serve(listener)
	}()

	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()

		if err := server.Shutdown(
			shutdownCtx,
		); err != nil {
			t.Error(err)
		}

		_ = listener.Close()

		select {
		case err := <-serveDone:
			if err != nil &&
				err != grpc.ErrServerStopped {
				t.Error(err)
			}

		case <-shutdownCtx.Done():
			t.Error("appliance server did not stop")
		}
	})

	return &performanceE2EAppliance{
		db:             db,
		address:        address,
		caPEM:          caPEM,
		installationID: installationID,
		service:        service,
	}
}

func newPerformanceE2EClient(
	t *testing.T,
	address string,
	caPEM []byte,
) *Client {
	t.Helper()

	connection, err := New(address, caPEM)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
	})

	return connection
}

func enrollPerformanceE2EAgent(
	t *testing.T,
	ctx context.Context,
	appliance *performanceE2EAppliance,
	connection *Client,
) state.State {
	t.Helper()

	invitation, err := enrollment.Issue(
		ctx,
		appliance.db,
		enrollment.DefaultTTL,
	)
	if err != nil {
		t.Fatal(err)
	}

	response, err := connection.Enroll(
		ctx,
		invitation.Token,
		"0.1.0-test",
	)
	if err != nil {
		t.Fatal(err)
	}

	saved := state.State{
		ApplianceAddress: appliance.address,
		CAPEM:            string(appliance.caPEM),

		InstallationID: response.GetInstallationId(),
		DeviceID:       response.GetDeviceId(),
		AgentToken:     response.GetAgentToken(),

		HeartbeatIntervalSeconds: response.
			GetHeartbeatIntervalSeconds(),
	}

	if err := saved.Validate(); err != nil {
		t.Fatal(err)
	}

	if saved.InstallationID != appliance.installationID {
		t.Fatal("incorrect appliance identity")
	}

	return saved
}

func requirePerformanceE2ECount(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	expected int,
) {
	t.Helper()

	var count int

	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM device_performance
	`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != expected {
		t.Fatalf(
			"expected %d historical samples, got %d",
			expected,
			count,
		)
	}
}

func requirePerformanceE2ERecord(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	request *rackosv1.ReportPerformanceRequest,
	receipt time.Time,
) {
	t.Helper()

	record, err := performance.NewRepository(db).Get(
		ctx,
		request.DeviceId,
		request.SampleId,
	)
	if err != nil {
		t.Fatal(err)
	}

	if record.DeviceID != request.DeviceId ||
		record.SampleID != request.SampleId {
		t.Fatal("incorrect persisted sample identity")
	}

	if !proto.Equal(
		record.Snapshot,
		request.Snapshot,
	) {
		t.Fatal("persisted snapshot differs from reported snapshot")
	}

	if !record.CollectedAt.Equal(
		request.Snapshot.CollectedAt.AsTime(),
	) {
		t.Fatal("collection timestamp changed")
	}

	if !record.ReceivedAt.Equal(receipt) {
		t.Fatal("stored receipt differs from acknowledgement")
	}

	if record.PayloadVersion != performance.CurrentPayloadVersion {
		t.Fatal("incorrect payload version")
	}
}

// performanceE2EStoredRow captures the exact stored
// record, including its serialized Protobuf bytes.
type performanceE2EStoredRow struct {
	DeviceID       string
	SampleID       string
	CollectedAt    int64
	ReceivedAt     int64
	PayloadVersion int
	PayloadHex     string
}

func readPerformanceE2ERow(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	request *rackosv1.ReportPerformanceRequest,
) performanceE2EStoredRow {
	t.Helper()

	var row performanceE2EStoredRow

	err := db.QueryRowContext(ctx, `
		SELECT
			device_id,
			sample_id,
			collected_at,
			received_at,
			payload_version,
			hex(payload)
		FROM device_performance
		WHERE device_id = ?
			AND sample_id = ?
	`,
		request.DeviceId,
		request.SampleId,
	).Scan(
		&row.DeviceID,
		&row.SampleID,
		&row.CollectedAt,
		&row.ReceivedAt,
		&row.PayloadVersion,
		&row.PayloadHex,
	)
	if err != nil {
		t.Fatal(err)
	}

	return row
}

func requirePerformanceE2EPlatform(t *testing.T) {
	t.Helper()

	if runtime.GOOS != "darwin" &&
		runtime.GOOS != "linux" {
		t.Skip("native appliance and agent state platform unsupported")
	}
}

// TestPerformanceEndToEndNativeCollection exercises
// actual operating system metrics collection through
// authenticated historical persistence.
func TestPerformanceEndToEndNativeCollection(
	t *testing.T,
) {
	requirePerformanceE2EPlatform(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()

	appliance := startPerformanceE2EAppliance(
		t,
		ctx,
		t.TempDir(),
		false,
	)

	connection := newPerformanceE2EClient(
		t,
		appliance.address,
		appliance.caPEM,
	)

	saved := enrollPerformanceE2EAgent(
		t,
		ctx,
		appliance,
		connection,
	)

	collectionCtx, collectionCancel :=
		context.WithTimeout(
			ctx,
			performanceCollectionLimit,
		)

	snapshot, err := metrics.Collect(collectionCtx)

	collectionCancel()

	if err != nil {
		t.Fatal(err)
	}

	request, err := NewPerformanceRequest(
		saved.DeviceID,
		snapshot,
	)
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := connection.ReportPerformance(
		ctx,
		saved.AgentToken,
		request,
	)
	if err != nil {
		t.Fatal(err)
	}

	requirePerformanceE2ERecord(
		t,
		ctx,
		appliance.db,
		request,
		receipt,
	)

	requirePerformanceE2ECount(
		t,
		ctx,
		appliance.db,
		1,
	)

	if request.Snapshot.Cpu == nil ||
		request.Snapshot.Memory == nil ||
		request.Snapshot.Storage == nil ||
		request.Snapshot.Network == nil {
		t.Fatal("incomplete native performance snapshot")
	}
}

// TestPerformanceEndToEndRestartAndRetry verifies
// acknowledgement recovery, persistent appliance
// identity, saved agent credentials, historical
// records and device credential revocation.
func TestPerformanceEndToEndRestartAndRetry(
	t *testing.T,
) {
	requirePerformanceE2EPlatform(t)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		60*time.Second,
	)
	defer cancel()

	root := t.TempDir()

	applianceDir := filepath.Join(
		root,
		"appliance",
	)

	agentDir := filepath.Join(
		root,
		"agent",
	)

	var (
		originalRequest *rackosv1.ReportPerformanceRequest
		originalReceipt time.Time
		originalRow     performanceE2EStoredRow
	)

	// The first subtest creates the original
	// appliance, enrolls an agent, and commits
	// a sample with a simulated lost acknowledgement.
	ok := t.Run(
		"commit and acknowledgement recovery",
		func(t *testing.T) {
			appliance := startPerformanceE2EAppliance(
				t,
				ctx,
				applianceDir,
				true,
			)

			connection := newPerformanceE2EClient(
				t,
				appliance.address,
				appliance.caPEM,
			)

			saved := enrollPerformanceE2EAgent(
				t,
				ctx,
				appliance,
				connection,
			)

			if err := state.Save(
				agentDir,
				saved,
			); err != nil {
				t.Fatal(err)
			}

			snapshot := testPerformanceConversionSnapshot()

			snapshot.CollectedAt = time.Now().UTC()

			var err error

			originalRequest, err = NewPerformanceRequest(
				saved.DeviceID,
				snapshot,
			)
			if err != nil {
				t.Fatal(err)
			}

			receipt, err := connection.ReportPerformance(
				ctx,
				saved.AgentToken,
				originalRequest,
			)

			if status.Code(err) != codes.Unavailable {
				t.Fatalf(
					"expected simulated acknowledgement loss, got %v",
					err,
				)
			}

			if !receipt.IsZero() {
				t.Fatal("failed request returned a receipt")
			}

			if !appliance.service.dropped.Load() {
				t.Fatal("acknowledgement loss was not simulated")
			}

			// Although the client received an error,
			// the appliance must have committed the
			// original sample.
			requirePerformanceE2ECount(
				t,
				ctx,
				appliance.db,
				1,
			)

			originalRow = readPerformanceE2ERow(
				t,
				ctx,
				appliance.db,
				originalRequest,
			)

			originalReceipt = time.UnixMilli(
				originalRow.ReceivedAt,
			).UTC()

			// Retry the same request without changing
			// its identity or measurements.
			receipt, err = connection.ReportPerformance(
				ctx,
				saved.AgentToken,
				originalRequest,
			)
			if err != nil {
				t.Fatal(err)
			}

			if !receipt.Equal(originalReceipt) {
				t.Fatal("retry changed original receipt")
			}

			requirePerformanceE2ERecord(
				t,
				ctx,
				appliance.db,
				originalRequest,
				originalReceipt,
			)

			requirePerformanceE2ECount(
				t,
				ctx,
				appliance.db,
				1,
			)

			actual := readPerformanceE2ERow(
				t,
				ctx,
				appliance.db,
				originalRequest,
			)

			if actual != originalRow {
				t.Fatal("retry modified original database record")
			}
		},
	)

	if !ok {
		return
	}

	// Subtest cleanup has closed the original
	// agent connection, server and database.
	t.Run(
		"restart and historical persistence",
		func(t *testing.T) {
			appliance := startPerformanceE2EAppliance(
				t,
				ctx,
				applianceDir,
				false,
			)

			// Load the existing agent identity from
			// disk instead of enrolling another agent.
			saved, err := state.Load(agentDir)
			if err != nil {
				t.Fatal(err)
			}

			if saved.InstallationID != appliance.installationID {
				t.Fatal("appliance identity changed after restart")
			}

			if !bytes.Equal(
				[]byte(saved.CAPEM),
				appliance.caPEM,
			) {
				t.Fatal("appliance CA changed after restart")
			}

			// A new ephemeral port is used for the
			// restarted test appliance. Trust remains
			// anchored to the originally saved CA.
			connection := newPerformanceE2EClient(
				t,
				appliance.address,
				[]byte(saved.CAPEM),
			)

			before := readPerformanceE2ERow(
				t,
				ctx,
				appliance.db,
				originalRequest,
			)

			if before != originalRow {
				t.Fatal("appliance restart modified historical record")
			}

			interval, err := connection.Heartbeat(
				ctx,
				saved.DeviceID,
				saved.AgentToken,
			)
			if err != nil {
				t.Fatal(err)
			}

			if interval != saved.HeartbeatIntervalSeconds {
				t.Fatal("incorrect heartbeat interval after restart")
			}

			// Idempotency must survive closing and
			// reopening the database.
			receipt, err := connection.ReportPerformance(
				ctx,
				saved.AgentToken,
				originalRequest,
			)
			if err != nil {
				t.Fatal(err)
			}

			if !receipt.Equal(originalReceipt) {
				t.Fatal("restart changed original receipt")
			}

			requirePerformanceE2ECount(
				t,
				ctx,
				appliance.db,
				1,
			)

			// Collect a new independent measurement.
			snapshot := testPerformanceConversionSnapshot()

			snapshot.CollectedAt = time.Now().UTC()
			snapshot.CPU.UsagePercent = 75

			newRequest, err := NewPerformanceRequest(
				saved.DeviceID,
				snapshot,
			)
			if err != nil {
				t.Fatal(err)
			}

			if newRequest.SampleId == originalRequest.SampleId {
				t.Fatal("new sample reused an existing identity")
			}

			receipt, err = connection.ReportPerformance(
				ctx,
				saved.AgentToken,
				newRequest,
			)
			if err != nil {
				t.Fatal(err)
			}

			requirePerformanceE2ERecord(
				t,
				ctx,
				appliance.db,
				newRequest,
				receipt,
			)

			requirePerformanceE2ECount(
				t,
				ctx,
				appliance.db,
				2,
			)

			after := readPerformanceE2ERow(
				t,
				ctx,
				appliance.db,
				originalRequest,
			)

			if after != originalRow {
				t.Fatal("subsequent reporting modified historical record")
			}

			// Revoke the existing agent credential.
			if err := credentials.Revoke(
				ctx,
				appliance.db,
				saved.DeviceID,
			); err != nil {
				t.Fatal(err)
			}

			// Even identical historical retries must
			// require current valid authentication.
			receipt, err = connection.ReportPerformance(
				ctx,
				saved.AgentToken,
				originalRequest,
			)

			if status.Code(err) != codes.Unauthenticated {
				t.Fatalf(
					"expected revoked credential rejection, got %v",
					err,
				)
			}

			if !receipt.IsZero() {
				t.Fatal("revoked agent received acknowledgement")
			}

			requirePerformanceE2ECount(
				t,
				ctx,
				appliance.db,
				2,
			)

			// Revocation preserves history, while
			// device removal deletes its history.
			if err := devices.NewRepository(
				appliance.db,
			).Delete(
				ctx,
				saved.DeviceID,
			); err != nil {
				t.Fatal(err)
			}

			requirePerformanceE2ECount(
				t,
				ctx,
				appliance.db,
				0,
			)

			_, err = connection.ReportPerformance(
				ctx,
				saved.AgentToken,
				newRequest,
			)

			if status.Code(err) != codes.Unauthenticated {
				t.Fatal("removed device was allowed to report")
			}

			requirePerformanceE2ECount(
				t,
				ctx,
				appliance.db,
				0,
			)
		},
	)
}
