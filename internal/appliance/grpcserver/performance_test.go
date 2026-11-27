package grpcserver_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/appliance/agentservice"
	agentcredentials "github.com/tschleining/rackos/internal/appliance/credentials"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/enrollment"
	"github.com/tschleining/rackos/internal/appliance/grpcserver"
	"github.com/tschleining/rackos/internal/appliance/identity"
	"github.com/tschleining/rackos/internal/appliance/performance"
	"github.com/tschleining/rackos/internal/appliance/pki"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPerformanceReportingIntegration(t *testing.T) {
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

	service := agentservice.New(db, installationID)

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

	serveDone := make(chan error, 1)

	go func() {
		serveDone <- server.Serve(listener)
	}()

	defer func() {
		shutdownCtx, shutdownCancel :=
			context.WithTimeout(
				context.Background(),
				3*time.Second,
			)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			t.Error(err)
		}

		select {
		case <-serveDone:
		case <-shutdownCtx.Done():
			t.Error("gRPC server failed to shut down")
		}
	}()

	roots := x509.NewCertPool()

	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("failed to load appliance CA")
	}

	connection, err := grpc.NewClient(
		"passthrough:///"+listener.Addr().String(),
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

	rpc := rackosv1.NewAgentServiceClient(
		connection,
	)

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
			inventoryAuthContext(
				ctx,
				issued.Token,
			),
			&rackosv1.EnrollRequest{
				Device: &rackosv1.DeviceInfo{
					Hostname:     hostname,
					Os:           "linux",
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

	first := enrollDevice(
		"performance-test-1.local",
	)

	second := enrollDevice(
		"performance-test-2.local",
	)

	deviceID := first.GetDeviceId()
	token := first.GetAgentToken()

	now := time.Now().UTC()

	repository := performance.NewRepository(db)
	deviceRepository := devices.NewRepository(db)

	newRequest := func(
		deviceID string,
		sampleID string,
	) *rackosv1.ReportPerformanceRequest {
		return testRPCPerformanceRequest(
			deviceID,
			sampleID,
			now,
		)
	}

	report := func(
		request *rackosv1.ReportPerformanceRequest,
		credential string,
	) (*rackosv1.ReportPerformanceResponse, error) {
		return rpc.ReportPerformance(
			inventoryAuthContext(
				ctx,
				credential,
			),
			request,
		)
	}

	requireCount := func(
		t *testing.T,
		expected int,
	) {
		t.Helper()

		var count int

		err := db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM device_performance
		`).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}

		if count != expected {
			t.Fatalf(
				"expected %d performance records, got %d",
				expected,
				count,
			)
		}
	}

	requireAccepted := func(
		t *testing.T,
		request *rackosv1.ReportPerformanceRequest,
		credential string,
	) time.Time {
		t.Helper()

		response, err := report(
			request,
			credential,
		)
		if err != nil {
			t.Fatal(err)
		}

		if !response.GetAccepted() {
			t.Fatal("performance report was not accepted")
		}

		if response.GetSampleId() != request.SampleId {
			t.Fatal("incorrect acknowledged sample ID")
		}

		if response.GetReceivedAt() == nil {
			t.Fatal("missing receipt timestamp")
		}

		if err := response.ReceivedAt.CheckValid(); err != nil {
			t.Fatal(err)
		}

		receivedAt := response.ReceivedAt.AsTime()

		record, err := repository.Get(
			ctx,
			request.DeviceId,
			request.SampleId,
		)
		if err != nil {
			t.Fatal(err)
		}

		if !record.ReceivedAt.Equal(receivedAt) {
			t.Fatal("acknowledgement receipt differs from stored receipt")
		}

		if !proto.Equal(
			record.Snapshot,
			request.Snapshot,
		) {
			t.Fatal("stored performance differs from reported snapshot")
		}

		if record.PayloadVersion != performance.CurrentPayloadVersion {
			t.Fatal("incorrect stored payload version")
		}

		return receivedAt
	}

	requireRejected := func(
		t *testing.T,
		response *rackosv1.ReportPerformanceResponse,
		err error,
		expected codes.Code,
	) {
		t.Helper()

		if status.Code(err) != expected {
			t.Fatalf(
				"expected gRPC status %s, got %s: %v",
				expected,
				status.Code(err),
				err,
			)
		}

		if response != nil {
			t.Fatal("rejected report returned a response")
		}
	}

	original := newRequest(
		deviceID,
		strings.Repeat("a", 32),
	)

	before, err := deviceRepository.Get(
		ctx,
		deviceID,
	)
	if err != nil {
		t.Fatal(err)
	}

	receiptBefore := time.Now().UTC().Truncate(
		time.Millisecond,
	)

	originalReceipt := requireAccepted(
		t,
		original,
		token,
	)

	receiptAfter := time.Now().UTC().Truncate(
		time.Millisecond,
	)

	if originalReceipt.Before(receiptBefore) ||
		originalReceipt.After(receiptAfter) {
		t.Fatal("receipt timestamp is outside request window")
	}

	requireCount(t, 1)

	t.Run("device timestamps unchanged", func(t *testing.T) {
		after, err := deviceRepository.Get(
			ctx,
			deviceID,
		)
		if err != nil {
			t.Fatal(err)
		}

		if !before.LastSeenAt.Equal(after.LastSeenAt) ||
			!before.UpdatedAt.Equal(after.UpdatedAt) {
			t.Fatal("performance reporting modified device timestamps")
		}
	})

	t.Run("missing credentials", func(t *testing.T) {
		response, err := rpc.ReportPerformance(
			ctx,
			original,
		)

		requireRejected(
			t, response, err, codes.Unauthenticated,
		)

		requireCount(t, 1)
	})

	t.Run("invalid credentials", func(t *testing.T) {
		response, err := report(
			original,
			"invalid-credential",
		)

		requireRejected(
			t, response, err, codes.Unauthenticated,
		)

		requireCount(t, 1)
	})

	t.Run("duplicate authorization", func(t *testing.T) {
		authCtx := metadata.NewOutgoingContext(
			ctx,
			metadata.Pairs(
				"authorization",
				"Bearer "+token,
				"authorization",
				"Bearer "+token,
			),
		)

		response, err := rpc.ReportPerformance(
			authCtx,
			original,
		)

		requireRejected(
			t, response, err, codes.Unauthenticated,
		)

		requireCount(t, 1)
	})

	t.Run("cross-device authentication", func(t *testing.T) {
		request := newRequest(
			second.GetDeviceId(),
			strings.Repeat("b", 32),
		)

		response, err := report(
			request,
			token,
		)

		requireRejected(
			t, response, err, codes.Unauthenticated,
		)

		requireCount(t, 1)
	})

	t.Run("missing device identity", func(t *testing.T) {
		request := newRequest(
			"",
			strings.Repeat("b", 32),
		)

		response, err := report(request, token)

		requireRejected(
			t, response, err, codes.Unauthenticated,
		)

		requireCount(t, 1)
	})

	t.Run("authentication before validation", func(t *testing.T) {
		request := &rackosv1.ReportPerformanceRequest{
			DeviceId: deviceID,
		}

		response, err := rpc.ReportPerformance(
			ctx,
			request,
		)

		requireRejected(
			t, response, err, codes.Unauthenticated,
		)

		requireCount(t, 1)
	})

	t.Run("invalid snapshots", func(t *testing.T) {
		tests := []struct {
			name   string
			change func(
				*rackosv1.ReportPerformanceRequest,
			)
		}{
			{
				name: "missing sample ID",
				change: func(r *rackosv1.ReportPerformanceRequest) {
					r.SampleId = ""
				},
			},
			{
				name: "missing snapshot",
				change: func(r *rackosv1.ReportPerformanceRequest) {
					r.Snapshot = nil
				},
			},
			{
				name: "invalid CPU utilization",
				change: func(r *rackosv1.ReportPerformanceRequest) {
					r.Snapshot.Cpu.UsagePercent = math.NaN()
				},
			},
			{
				name: "invalid memory accounting",
				change: func(r *rackosv1.ReportPerformanceRequest) {
					r.Snapshot.Memory.UsedBytes++
				},
			},
			{
				name: "invalid collection timestamp",
				change: func(r *rackosv1.ReportPerformanceRequest) {
					r.Snapshot.CollectedAt =
						timestamppb.New(
							time.Now().Add(time.Hour),
						)
				},
			},
			{
				name: "missing network group",
				change: func(r *rackosv1.ReportPerformanceRequest) {
					r.Snapshot.Network = nil
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				request := newRequest(
					deviceID,
					strings.Repeat("b", 32),
				)

				tt.change(request)

				response, err := report(
					request,
					token,
				)

				requireRejected(
					t,
					response,
					err,
					codes.InvalidArgument,
				)

				requireCount(t, 1)
			})
		}
	})

	t.Run("oversized request", func(t *testing.T) {
		request := newRequest(
			deviceID,
			strings.Repeat("b", 32),
		)

		unknown := protowire.AppendTag(
			nil,
			1000,
			protowire.BytesType,
		)

		unknown = protowire.AppendBytes(
			unknown,
			make(
				[]byte,
				performance.MaxRequestBytes,
			),
		)

		request.Snapshot.
			ProtoReflect().
			SetUnknown(unknown)

		if proto.Size(request) <= performance.MaxRequestBytes {
			t.Fatal("test request is not oversized")
		}

		if proto.Size(request) >= 1<<20 {
			t.Fatal("test request exceeds transport limit")
		}

		response, err := report(request, token)

		requireRejected(
			t, response, err, codes.ResourceExhausted,
		)

		requireCount(t, 1)
	})

	t.Run("identical retry", func(t *testing.T) {
		// Ensure the subsequent request occurs in
		// a different millisecond.
		time.Sleep(5 * time.Millisecond)

		retry := proto.Clone(original).(*rackosv1.ReportPerformanceRequest)

		receipt := requireAccepted(
			t,
			retry,
			token,
		)

		if !receipt.Equal(originalReceipt) {
			t.Fatal("retry changed original receipt timestamp")
		}

		requireCount(t, 1)
	})

	t.Run("conflicting sample", func(t *testing.T) {
		conflicting := proto.Clone(original).(*rackosv1.ReportPerformanceRequest)

		conflicting.Snapshot.Cpu.UsagePercent = 90

		response, err := report(
			conflicting,
			token,
		)

		requireRejected(
			t, response, err, codes.AlreadyExists,
		)

		record, err := repository.Get(
			ctx,
			original.DeviceId,
			original.SampleId,
		)
		if err != nil {
			t.Fatal(err)
		}

		if !proto.Equal(
			record.Snapshot,
			original.Snapshot,
		) {
			t.Fatal("conflicting report modified stored snapshot")
		}

		if !record.ReceivedAt.Equal(originalReceipt) {
			t.Fatal("conflicting report modified receipt timestamp")
		}

		requireCount(t, 1)
	})

	// Store a historical sample older than the
	// original sample.
	older := newRequest(
		deviceID,
		strings.Repeat("b", 32),
	)

	older.Snapshot.CollectedAt = timestamppb.New(
		now.Add(-time.Hour),
	)

	requireAccepted(
		t,
		older,
		token,
	)

	requireCount(t, 2)

	// Store another sample collected at exactly the
	// same time as the original.
	sameTimestamp := newRequest(
		deviceID,
		strings.Repeat("c", 32),
	)

	sameTimestamp.Snapshot.Cpu.UsagePercent = 70

	requireAccepted(
		t,
		sameTimestamp,
		token,
	)

	requireCount(t, 3)

	t.Run("database failure rolls back", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			CREATE TRIGGER performance_test_fail_insert
			AFTER INSERT ON device_performance
			BEGIN
				SELECT RAISE(
					FAIL,
					'simulated performance persistence failure'
				);
			END;
		`)
		if err != nil {
			t.Fatal(err)
		}

		defer func() {
			cleanupCtx, cleanupCancel :=
				context.WithTimeout(
					context.Background(),
					5*time.Second,
				)
			defer cleanupCancel()

			_, err := db.ExecContext(cleanupCtx, `
				DROP TRIGGER IF EXISTS
					performance_test_fail_insert
			`)
			if err != nil {
				t.Error(err)
			}
		}()

		failed := newRequest(
			deviceID,
			strings.Repeat("d", 32),
		)

		response, err := report(
			failed,
			token,
		)

		requireRejected(
			t, response, err, codes.Internal,
		)

		requireCount(t, 3)

		_, err = repository.Get(
			ctx,
			failed.DeviceId,
			failed.SampleId,
		)

		if err != performance.ErrNotFound {
			t.Fatalf(
				"expected failed sample to be absent, got %v",
				err,
			)
		}

		_, err = db.ExecContext(ctx, `
			DROP TRIGGER performance_test_fail_insert
		`)
		if err != nil {
			t.Fatal(err)
		}

		// Retry the same sample after removing
		// the simulated storage failure.
		requireAccepted(
			t,
			failed,
			token,
		)

		requireCount(t, 4)
	})

	// Verify that sample identity is scoped to each
	// authenticated device.
	secondRequest := newRequest(
		second.GetDeviceId(),
		original.SampleId,
	)

	secondRequest.Snapshot.Cpu.UsagePercent = 15

	requireAccepted(
		t,
		secondRequest,
		second.GetAgentToken(),
	)

	requireCount(t, 5)

	t.Run("heartbeat remains functional", func(t *testing.T) {
		response, err := rpc.Heartbeat(
			inventoryAuthContext(ctx, token),
			&rackosv1.HeartbeatRequest{
				DeviceId: deviceID,
			},
		)
		if err != nil {
			t.Fatal(err)
		}

		if response.GetHeartbeatIntervalSeconds() != 30 {
			t.Fatal("unexpected heartbeat interval")
		}

		requireCount(t, 5)
	})

	t.Run("revoked credential rejected", func(t *testing.T) {
		err := agentcredentials.Revoke(
			ctx,
			db,
			deviceID,
		)
		if err != nil {
			t.Fatal(err)
		}

		request := newRequest(
			deviceID,
			strings.Repeat("e", 32),
		)

		response, err := report(
			request,
			token,
		)

		requireRejected(
			t, response, err, codes.Unauthenticated,
		)

		// Even an identical retry requires valid
		// current authentication.
		response, err = report(
			original,
			token,
		)

		requireRejected(
			t, response, err, codes.Unauthenticated,
		)

		// Revocation preserves historical data.
		requireCount(t, 5)
	})

	t.Run("unrelated device remains authorized", func(t *testing.T) {
		request := newRequest(
			second.GetDeviceId(),
			strings.Repeat("e", 32),
		)

		requireAccepted(
			t,
			request,
			second.GetAgentToken(),
		)

		requireCount(t, 6)
	})

	t.Run("removed device rejected", func(t *testing.T) {
		err := deviceRepository.Delete(
			ctx,
			second.GetDeviceId(),
		)
		if err != nil {
			t.Fatal(err)
		}

		// Removing the second device must cascade
		// to both of its historical records.
		requireCount(t, 4)

		response, err := report(
			secondRequest,
			second.GetAgentToken(),
		)

		requireRejected(
			t, response, err, codes.Unauthenticated,
		)

		requireCount(t, 4)

		// The first device's history must survive
		// removal of the second device.
		var count int

		err = db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM device_performance
			WHERE device_id = ?
		`, deviceID).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}

		if count != 4 {
			t.Fatal("unrelated performance history was removed")
		}
	})
}

// testRPCPerformanceRequest constructs a complete
// performance report for authenticated RPC testing.
func testRPCPerformanceRequest(
	deviceID string,
	sampleID string,
	collectedAt time.Time,
) *rackosv1.ReportPerformanceRequest {
	linkSpeed := uint64(1000000000)
	receiveDropped := uint64(0)

	return &rackosv1.ReportPerformanceRequest{
		DeviceId: deviceID,
		SampleId: sampleID,

		Snapshot: &rackosv1.PerformanceSnapshot{
			CollectedAt: timestamppb.New(
				collectedAt,
			),

			Cpu: &rackosv1.CPUPerformance{
				UsagePercent:         25.5,
				SampleIntervalMillis: 1000,
			},

			Memory: &rackosv1.MemoryPerformance{
				TotalBytes:     16384,
				AvailableBytes: 4096,
				UsedBytes:      12288,
				UsagePercent:   75,
			},

			Storage: &rackosv1.StoragePerformance{
				Filesystems: []*rackosv1.FilesystemPerformance{
					{
						Source:         "/dev/sda1",
						MountPoint:     "/",
						FilesystemType: "ext4",

						Capacity: &rackosv1.FilesystemCapacityPerformance{
							TotalBytes:     1000000,
							FreeBytes:      400000,
							AvailableBytes: 350000,
							UsedBytes:      600000,
							ReservedBytes:  50000,
							UsagePercent:   60,
						},
					},
				},

				Disks: []*rackosv1.DiskPerformance{
					{
						Device:          "sda",
						ReadBytes:       math.MaxUint64,
						WriteBytes:      262144000,
						ReadOperations:  42000,
						WriteOperations: 21000,
					},
				},

				DiskRates: []*rackosv1.DiskRate{
					{
						Device:                   "sda",
						ReadBytesPerSecond:       1048576.5,
						WriteBytesPerSecond:      524288.25,
						ReadOperationsPerSecond:  25.5,
						WriteOperationsPerSecond: 12.5,
						SampleIntervalMillis:     1100,
					},
				},
			},

			Network: &rackosv1.NetworkPerformance{
				Interfaces: []*rackosv1.NetworkInterfacePerformance{
					{
						Name:  "eth0",
						Index: 2,
						Up:    true,
						Mtu:   1500,

						LinkSpeedBitsPerSecond: &linkSpeed,

						ReceivedBytes:      math.MaxUint64,
						ReceivedPackets:    420000,
						ReceiveErrors:      2,
						TransmittedBytes:   262144000,
						TransmittedPackets: 210000,
						TransmitErrors:     1,

						ReceiveDropped:  &receiveDropped,
						TransmitDropped: nil,
					},
				},

				InterfaceRates: []*rackosv1.NetworkInterfaceRate{
					{
						Name:                     "eth0",
						Index:                    2,
						ReceiveBytesPerSecond:    6000.5,
						TransmitBytesPerSecond:   3000.25,
						ReceivePacketsPerSecond:  60.5,
						TransmitPacketsPerSecond: 30.25,
						SampleIntervalMillis:     1250,
					},
				},
			},
		},
	}
}
