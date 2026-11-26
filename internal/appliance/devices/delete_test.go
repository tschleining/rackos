package devices_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/credentials"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
)

const (
	deleteDeviceID = "11111111-1111-4111-8111-111111111111"
	keepDeviceID   = "22222222-2222-4222-8222-222222222222"
)

func newDeletionTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := database.Open(
		context.Background(),
		t.TempDir(),
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return db
}

func createDeletionTestDevice(
	t *testing.T,
	db *sql.DB,
	id string,
) string {
	t.Helper()

	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	repository := devices.NewRepository(db).WithTx(tx)

	_, err = repository.Create(
		ctx,
		id,
		devices.DeviceInfo{
			Hostname:     "test-device.local",
			OS:           "linux",
			Architecture: "amd64",
			AgentVersion: "0.1.0-test",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	token, err := credentials.Issue(
		ctx,
		tx,
		id,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	return token
}

func TestDeleteRegisteredDevice(t *testing.T) {
	ctx := context.Background()
	db := newDeletionTestDB(t)
	repository := devices.NewRepository(db)

	createDeletionTestDevice(t, db, deleteDeviceID)

	if err := repository.Delete(
		ctx,
		deleteDeviceID,
	); err != nil {
		t.Fatal(err)
	}

	_, err := repository.Get(
		ctx,
		deleteDeviceID,
	)

	if !errors.Is(err, devices.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	records, err := repository.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 0 {
		t.Fatal("deleted device remains registered")
	}
}

func TestDeleteDeviceCredentialCascade(t *testing.T) {
	ctx := context.Background()
	db := newDeletionTestDB(t)
	repository := devices.NewRepository(db)

	token := createDeletionTestDevice(
		t,
		db,
		deleteDeviceID,
	)

	// Verify the credential works before deletion.
	err := credentials.Authenticate(
		ctx,
		db,
		deleteDeviceID,
		token,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := repository.Delete(
		ctx,
		deleteDeviceID,
	); err != nil {
		t.Fatal(err)
	}

	// Deleted credentials must no longer authenticate.
	err = credentials.Authenticate(
		ctx,
		db,
		deleteDeviceID,
		token,
	)

	if !errors.Is(
		err,
		credentials.ErrUnauthenticated,
	) {
		t.Fatalf(
			"expected authentication failure, got %v",
			err,
		)
	}

	// Verify SQLite physically removed the credential.
	var count int

	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		 FROM agent_credentials
		 WHERE device_id = ?`,
		deleteDeviceID,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatal("orphaned agent credential remains")
	}
}

func TestDeleteDevicePreservesOtherDevices(t *testing.T) {
	ctx := context.Background()
	db := newDeletionTestDB(t)
	repository := devices.NewRepository(db)

	createDeletionTestDevice(t, db, deleteDeviceID)

	keepToken := createDeletionTestDevice(
		t,
		db,
		keepDeviceID,
	)

	if err := repository.Delete(
		ctx,
		deleteDeviceID,
	); err != nil {
		t.Fatal(err)
	}

	records, err := repository.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(records) != 1 {
		t.Fatalf(
			"expected one remaining device, got %d",
			len(records),
		)
	}

	if records[0].ID != keepDeviceID {
		t.Fatal("incorrect remaining device")
	}

	err = credentials.Authenticate(
		ctx,
		db,
		keepDeviceID,
		keepToken,
	)
	if err != nil {
		t.Fatalf(
			"remaining device credential invalid: %v",
			err,
		)
	}
}

func TestDeleteUnknownDevice(t *testing.T) {
	db := newDeletionTestDB(t)
	repository := devices.NewRepository(db)

	err := repository.Delete(
		context.Background(),
		deleteDeviceID,
	)

	if !errors.Is(err, devices.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteEmptyDeviceID(t *testing.T) {
	db := newDeletionTestDB(t)
	repository := devices.NewRepository(db)

	err := repository.Delete(
		context.Background(),
		"",
	)

	if !errors.Is(err, devices.ErrInvalidDevice) {
		t.Fatalf(
			"expected ErrInvalidDevice, got %v",
			err,
		)
	}
}

func TestDeleteDeviceTwice(t *testing.T) {
	ctx := context.Background()
	db := newDeletionTestDB(t)
	repository := devices.NewRepository(db)

	createDeletionTestDevice(t, db, deleteDeviceID)

	if err := repository.Delete(
		ctx,
		deleteDeviceID,
	); err != nil {
		t.Fatal(err)
	}

	err := repository.Delete(ctx, deleteDeviceID)

	if !errors.Is(err, devices.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteDeviceTransactionRollback(t *testing.T) {
	ctx := context.Background()
	db := newDeletionTestDB(t)
	repository := devices.NewRepository(db)

	token := createDeletionTestDevice(
		t,
		db,
		deleteDeviceID,
	)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	err = repository.WithTx(tx).Delete(
		ctx,
		deleteDeviceID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// The device must still exist after rollback.
	if _, err := repository.Get(
		ctx,
		deleteDeviceID,
	); err != nil {
		t.Fatal(err)
	}

	// Its credentials must also have been restored.
	err = credentials.Authenticate(
		ctx,
		db,
		deleteDeviceID,
		token,
	)
	if err != nil {
		t.Fatal(err)
	}
}
