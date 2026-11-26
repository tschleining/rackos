package grpcserver_test

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	rackosv1 "github.com/tschleining/rackos/api/proto/rackos/v1"
	"github.com/tschleining/rackos/internal/appliance/inventory"

	"google.golang.org/grpc/codes"
)

func testPersistentInventoryReporting(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	rpc rackosv1.AgentServiceClient,
	first *rackosv1.EnrollResponse,
	second *rackosv1.EnrollResponse,
	newRequest func(string) *rackosv1.ReportInventoryRequest,
) {
	t.Helper()

	repository := inventory.NewRepository(db)

	deviceID := first.GetDeviceId()
	token := first.GetAgentToken()

	report := func(
		req *rackosv1.ReportInventoryRequest,
		credential string,
	) (*rackosv1.ReportInventoryResponse, error) {
		return rpc.ReportInventory(
			inventoryAuthContext(ctx, credential),
			req,
		)
	}

	requireAccepted := func(
		req *rackosv1.ReportInventoryRequest,
	) {
		t.Helper()

		response, err := report(req, token)
		if err != nil {
			t.Fatal(err)
		}

		if !response.GetAccepted() {
			t.Fatal("inventory report was not accepted")
		}
	}

	loadInventory := func() inventory.Inventory {
		t.Helper()

		record, err := repository.Get(ctx, deviceID)
		if err != nil {
			t.Fatal(err)
		}

		return record
	}

	requireUnchanged := func(expected inventory.Inventory) {
		t.Helper()

		actual := loadInventory()

		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf(
				"stored inventory changed unexpectedly:\n"+
					"expected: %+v\nactual: %+v",
				expected,
				actual,
			)
		}
	}

	// Verify that the earlier successful report
	// was actually persisted.
	originalRequest := newRequest(deviceID)
	original := loadInventory()

	requirePersistedSnapshot(
		t,
		original,
		originalRequest,
	)

	if original.ReceivedAt.Before(original.CollectedAt) {
		t.Fatal("receipt timestamp precedes collection")
	}

	// An identical retry must not modify the original
	// inventory or receipt timestamp.
	time.Sleep(5 * time.Millisecond)

	requireAccepted(newRequest(deviceID))
	requireUnchanged(original)

	// Submit a newer inventory snapshot.
	newer := newRequest(deviceID)

	newer.Inventory.CollectedAtUnixMillis += 1000
	newer.Inventory.MemoryBytes = 32 * 1024 * 1024 * 1024
	newer.Inventory.OsVersion = "27.0"
	newer.Inventory.CpuModel = "Updated test processor"

	// A newer snapshot can also clear optional fields.
	newer.Inventory.BootTimeUnixMillis = 0
	newer.Inventory.HostLogicalCpus = 0
	newer.Inventory.PhysicalCores = 0

	receivedBefore := time.Now().UTC().Truncate(
		time.Millisecond,
	)

	requireAccepted(newer)

	updated := loadInventory()

	receivedAfter := time.Now().UTC().Truncate(
		time.Millisecond,
	)

	requirePersistedSnapshot(t, updated, newer)

	if updated.ReceivedAt.Before(receivedBefore) ||
		updated.ReceivedAt.After(receivedAfter) {
		t.Fatal("receipt timestamp is outside request window")
	}

	// A delayed report cannot overwrite newer inventory.
	_, err := report(
		newRequest(deviceID),
		token,
	)

	requireInventoryCode(
		t, err, codes.FailedPrecondition,
	)

	requireUnchanged(updated)

	// Different contents with the same collection
	// timestamp must be rejected.
	conflicting := newRequest(deviceID)

	conflicting.Inventory.CollectedAtUnixMillis =
		newer.Inventory.CollectedAtUnixMillis

	conflicting.Inventory.MemoryBytes =
		64 * 1024 * 1024 * 1024

	_, err = report(conflicting, token)

	requireInventoryCode(
		t, err, codes.AlreadyExists,
	)

	requireUnchanged(updated)

	// Another enrolled device cannot update this
	// device's inventory.
	unauthorized := newRequest(deviceID)

	unauthorized.Inventory.CollectedAtUnixMillis =
		newer.Inventory.CollectedAtUnixMillis + 1000

	unauthorized.Inventory.MemoryBytes =
		128 * 1024 * 1024 * 1024

	_, err = report(
		unauthorized,
		second.GetAgentToken(),
	)

	requireInventoryCode(
		t, err, codes.Unauthenticated,
	)

	requireUnchanged(updated)

	// Install a temporary test trigger that fails
	// AFTER SQLite attempts to update the inventory.
	//
	// RAISE(FAIL) does not necessarily undo changes
	// already made by the failing statement.
	// The RPC transaction must roll them back.
	_, err = db.ExecContext(ctx, `
		CREATE TRIGGER inventory_test_fail_update
		AFTER UPDATE ON device_inventory
		BEGIN
			SELECT RAISE(
				FAIL,
				'simulated inventory persistence failure'
			);
		END;
	`)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()

		_, err := db.ExecContext(cleanupCtx, `
			DROP TRIGGER IF EXISTS inventory_test_fail_update
		`)
		if err != nil {
			t.Error(err)
		}
	})

	failed := newRequest(deviceID)

	failed.Inventory.CollectedAtUnixMillis =
		newer.Inventory.CollectedAtUnixMillis + 2000

	failed.Inventory.MemoryBytes =
		96 * 1024 * 1024 * 1024

	response, err := report(failed, token)

	requireInventoryCode(
		t, err, codes.Internal,
	)

	if response != nil && response.GetAccepted() {
		t.Fatal("failed database write was acknowledged")
	}

	// The failed transaction must leave the existing
	// inventory completely unchanged.
	requireUnchanged(updated)

	// Remove the simulated failure.
	_, err = db.ExecContext(ctx, `
		DROP TRIGGER inventory_test_fail_update
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Retrying after recovery must succeed.
	requireAccepted(failed)

	recovered := loadInventory()

	requirePersistedSnapshot(
		t,
		recovered,
		failed,
	)

	// Each registered device must have at most
	// one inventory record.
	var count int

	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM device_inventory
		WHERE device_id = ?
	`, deviceID).Scan(&count)

	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf(
			"expected one inventory record, got %d",
			count,
		)
	}
}

// requirePersistedSnapshot verifies every hardware
// field stored by the appliance.
func requirePersistedSnapshot(
	t *testing.T,
	actual inventory.Inventory,
	request *rackosv1.ReportInventoryRequest,
) {
	t.Helper()

	snapshot := request.GetInventory()

	expected := inventory.Inventory{
		DeviceID:        request.GetDeviceId(),
		Hostname:        snapshot.GetHostname(),
		OS:              snapshot.GetOs(),
		Architecture:    snapshot.GetArchitecture(),
		LogicalCPUs:     snapshot.GetLogicalCpus(),
		HostLogicalCPUs: snapshot.GetHostLogicalCpus(),
		PhysicalCores:   snapshot.GetPhysicalCores(),
		CPUModel:        snapshot.GetCpuModel(),
		CPUManufacturer: snapshot.GetCpuManufacturer(),
		MemoryBytes:     snapshot.GetMemoryBytes(),
		HardwareModel:   snapshot.GetHardwareModel(),
		OSVersion:       snapshot.GetOsVersion(),
		KernelVersion:   snapshot.GetKernelVersion(),

		CollectedAt: time.UnixMilli(
			snapshot.GetCollectedAtUnixMillis(),
		).UTC(),

		ReceivedAt: actual.ReceivedAt,
	}

	if snapshot.GetBootTimeUnixMillis() != 0 {
		boot := time.UnixMilli(
			snapshot.GetBootTimeUnixMillis(),
		).UTC()

		expected.BootTime = &boot
	}

	if actual.ReceivedAt.IsZero() ||
		actual.ReceivedAt.UnixMilli() <= 0 {
		t.Fatal("missing or invalid appliance receipt timestamp")
	}

	if actual.ReceivedAt.Location() != time.UTC {
		t.Fatal("receipt timestamp is not normalized to UTC")
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf(
			"persisted inventory mismatch:\n"+
				"expected: %+v\nactual: %+v",
			expected,
			actual,
		)
	}
}
