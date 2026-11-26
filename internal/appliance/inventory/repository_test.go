package inventory_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/inventory"
)

const (
	testDeviceID  = "11111111-1111-4111-8111-111111111111"
	otherDeviceID = "22222222-2222-4222-8222-222222222222"
)

func setupRepository(
	t *testing.T,
) (context.Context, *sql.DB, *inventory.Repository) {
	t.Helper()

	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	t.Cleanup(cancel)

	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	repository := devices.NewRepository(db)

	for _, id := range []string{
		testDeviceID,
		otherDeviceID,
	} {
		_, err := repository.Create(
			ctx,
			id,
			devices.DeviceInfo{
				Hostname:     "test-device.local",
				OS:           "darwin",
				Architecture: "arm64",
				AgentVersion: "0.1.0-test",
			},
		)
		if err != nil {
			t.Fatal(err)
		}
	}

	return ctx, db, inventory.NewRepository(db)
}

func testSnapshot() inventory.Inventory {
	collected := time.Date(
		2026, time.November, 26,
		20, 0, 0, 0,
		time.UTC,
	)

	boot := collected.Add(-24 * time.Hour)

	return inventory.Inventory{
		DeviceID:        testDeviceID,
		Hostname:        "test-device.local",
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
		BootTime:        &boot,
		CollectedAt:     collected,
		ReceivedAt:      collected.Add(time.Second),
	}
}

func requireUpsert(
	t *testing.T,
	ctx context.Context,
	repository *inventory.Repository,
	record inventory.Inventory,
) {
	t.Helper()

	if err := repository.Upsert(
		ctx,
		record,
	); err != nil {
		t.Fatal(err)
	}
}

func requireInventory(
	t *testing.T,
	ctx context.Context,
	repository *inventory.Repository,
	expected inventory.Inventory,
) {
	t.Helper()

	actual, err := repository.Get(
		ctx,
		expected.DeviceID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf(
			"inventory mismatch:\nexpected: %+v\nactual:   %+v",
			expected,
			actual,
		)
	}
}

func requireInventoryError(
	t *testing.T,
	err error,
	expected error,
) {
	t.Helper()

	if !errors.Is(err, expected) {
		t.Fatalf(
			"expected %v, got %v",
			expected,
			err,
		)
	}
}

func TestRepositoryInsertAndGet(t *testing.T) {
	ctx, _, repository := setupRepository(t)

	record := testSnapshot()

	requireUpsert(t, ctx, repository, record)
	requireInventory(t, ctx, repository, record)
}

func TestRepositoryGetNotFound(t *testing.T) {
	ctx, _, repository := setupRepository(t)

	for _, id := range []string{
		testDeviceID,
		"33333333-3333-4333-8333-333333333333",
	} {
		_, err := repository.Get(ctx, id)

		requireInventoryError(
			t, err, inventory.ErrNotFound,
		)
	}
}

func TestRepositoryGetInvalidDeviceID(t *testing.T) {
	ctx, _, repository := setupRepository(t)

	for _, id := range []string{"", "   "} {
		_, err := repository.Get(ctx, id)

		requireInventoryError(
			t, err, inventory.ErrInvalidInventory,
		)
	}
}

func TestRepositoryReplaceNewerInventory(t *testing.T) {
	ctx, db, repository := setupRepository(t)

	original := testSnapshot()

	requireUpsert(t, ctx, repository, original)

	newer := original
	newer.CollectedAt = original.CollectedAt.Add(
		time.Minute,
	)
	newer.ReceivedAt = newer.CollectedAt.Add(
		time.Second,
	)
	newer.MemoryBytes = 32 * 1024 * 1024 * 1024
	newer.OSVersion = "27.0"
	newer.CPUModel = "Updated processor"

	requireUpsert(t, ctx, repository, newer)
	requireInventory(t, ctx, repository, newer)

	var count int

	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM device_inventory
		WHERE device_id = ?
	`, testDeviceID).Scan(&count)

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

func TestRepositoryRejectStaleInventory(t *testing.T) {
	ctx, _, repository := setupRepository(t)

	original := testSnapshot()

	requireUpsert(t, ctx, repository, original)

	older := original
	older.CollectedAt = original.CollectedAt.Add(
		-time.Minute,
	)
	older.ReceivedAt = original.ReceivedAt.Add(
		time.Minute,
	)
	older.MemoryBytes = 16 * 1024 * 1024 * 1024

	err := repository.Upsert(ctx, older)

	requireInventoryError(
		t, err, inventory.ErrStaleInventory,
	)

	requireInventory(t, ctx, repository, original)
}

func TestRepositoryIdenticalRetry(t *testing.T) {
	ctx, _, repository := setupRepository(t)

	original := testSnapshot()

	requireUpsert(t, ctx, repository, original)

	retry := original

	// A separate pointer with the same boot time
	// must not cause a snapshot conflict.
	boot := *original.BootTime
	retry.BootTime = &boot

	retry.ReceivedAt = original.ReceivedAt.Add(
		10 * time.Minute,
	)

	requireUpsert(t, ctx, repository, retry)

	// The original receipt timestamp must survive.
	requireInventory(t, ctx, repository, original)
}

func TestRepositoryRejectConflictingInventory(t *testing.T) {
	ctx, _, repository := setupRepository(t)

	original := testSnapshot()

	requireUpsert(t, ctx, repository, original)

	conflicting := original
	conflicting.MemoryBytes = 64 * 1024 * 1024 * 1024
	conflicting.ReceivedAt = original.ReceivedAt.Add(
		time.Minute,
	)

	err := repository.Upsert(ctx, conflicting)

	requireInventoryError(
		t, err, inventory.ErrInventoryConflict,
	)

	requireInventory(t, ctx, repository, original)
}

func TestRepositoryInvalidInventory(t *testing.T) {
	ctx, _, repository := setupRepository(t)

	original := testSnapshot()

	requireUpsert(t, ctx, repository, original)

	tests := []struct {
		name   string
		mutate func(*inventory.Inventory)
	}{
		{
			name: "missing device ID",
			mutate: func(s *inventory.Inventory) {
				s.DeviceID = ""
			},
		},
		{
			name: "whitespace device ID",
			mutate: func(s *inventory.Inventory) {
				s.DeviceID = " " + s.DeviceID
			},
		},
		{
			name: "missing hostname",
			mutate: func(s *inventory.Inventory) {
				s.Hostname = ""
			},
		},
		{
			name: "zero logical CPUs",
			mutate: func(s *inventory.Inventory) {
				s.LogicalCPUs = 0
			},
		},
		{
			name: "invalid CPU topology",
			mutate: func(s *inventory.Inventory) {
				s.PhysicalCores = 100
			},
		},
		{
			name: "memory overflow",
			mutate: func(s *inventory.Inventory) {
				s.MemoryBytes = ^uint64(0)
			},
		},
		{
			name: "missing collection timestamp",
			mutate: func(s *inventory.Inventory) {
				s.CollectedAt = time.Time{}
			},
		},
		{
			name: "missing receipt timestamp",
			mutate: func(s *inventory.Inventory) {
				s.ReceivedAt = time.Time{}
			},
		},
		{
			name: "future collection timestamp",
			mutate: func(s *inventory.Inventory) {
				s.CollectedAt = s.ReceivedAt.Add(
					time.Hour,
				)
			},
		},
		{
			name: "boot after collection",
			mutate: func(s *inventory.Inventory) {
				boot := s.CollectedAt.Add(time.Hour)
				s.BootTime = &boot
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := original

			// Make the report newer so timestamp ordering
			// cannot mask a missing validation check.
			record.CollectedAt = original.CollectedAt.Add(
				time.Minute,
			)
			record.ReceivedAt = record.CollectedAt.Add(
				time.Second,
			)

			tt.mutate(&record)

			err := repository.Upsert(ctx, record)

			requireInventoryError(
				t, err, inventory.ErrInvalidInventory,
			)

			requireInventory(
				t, ctx, repository, original,
			)
		})
	}
}

func TestRepositoryUnknownDevice(t *testing.T) {
	ctx, _, repository := setupRepository(t)

	record := testSnapshot()
	record.DeviceID =
		"33333333-3333-4333-8333-333333333333"

	if err := repository.Upsert(
		ctx,
		record,
	); err == nil {
		t.Fatal("inventory accepted for unknown device")
	}

	_, err := repository.Get(ctx, record.DeviceID)

	requireInventoryError(
		t, err, inventory.ErrNotFound,
	)
}

func TestRepositoryOptionalFields(t *testing.T) {
	ctx, _, repository := setupRepository(t)

	original := testSnapshot()

	requireUpsert(t, ctx, repository, original)

	minimal := inventory.Inventory{
		DeviceID:     testDeviceID,
		Hostname:     "test-server",
		OS:           "linux",
		Architecture: "amd64",
		LogicalCPUs:  4,
		CollectedAt: original.CollectedAt.Add(
			time.Minute,
		),
		ReceivedAt: original.ReceivedAt.Add(
			time.Minute,
		),
	}

	requireUpsert(t, ctx, repository, minimal)

	// A newer snapshot must clear unavailable fields
	// rather than retaining outdated hardware details.
	requireInventory(t, ctx, repository, minimal)
}

func TestRepositoryTimestampNormalization(t *testing.T) {
	ctx, _, repository := setupRepository(t)

	expected := testSnapshot()

	record := expected

	location := time.FixedZone(
		"test-offset",
		-5*60*60,
	)

	record.CollectedAt = record.CollectedAt.In(
		location,
	).Add(123 * time.Microsecond)

	record.ReceivedAt = record.ReceivedAt.In(
		location,
	).Add(456 * time.Microsecond)

	boot := record.BootTime.In(
		location,
	).Add(789 * time.Microsecond)

	record.BootTime = &boot

	requireUpsert(t, ctx, repository, record)

	requireInventory(t, ctx, repository, expected)
}

func TestRepositoryTransactionRollback(t *testing.T) {
	ctx, db, repository := setupRepository(t)

	record := testSnapshot()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	transactionRepository := repository.WithTx(tx)

	requireUpsert(
		t, ctx, transactionRepository, record,
	)

	requireInventory(
		t, ctx, transactionRepository, record,
	)

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	_, err = repository.Get(ctx, record.DeviceID)

	requireInventoryError(
		t, err, inventory.ErrNotFound,
	)
}

func TestRepositoryTransactionCommit(t *testing.T) {
	ctx, db, repository := setupRepository(t)

	record := testSnapshot()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	requireUpsert(
		t,
		ctx,
		repository.WithTx(tx),
		record,
	)

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	requireInventory(t, ctx, repository, record)
}

func TestRepositoryConcurrentWrites(t *testing.T) {
	ctx, _, repository := setupRepository(t)

	const writers = 16

	original := testSnapshot()

	makeRecord := func(index int) inventory.Inventory {
		record := original

		record.CollectedAt = original.CollectedAt.Add(
			time.Duration(index) * time.Second,
		)

		record.ReceivedAt = record.CollectedAt.Add(
			time.Second,
		)

		record.MemoryBytes = uint64(index+1) *
			1024 * 1024 * 1024

		return record
	}

	start := make(chan struct{})
	results := make(chan error, writers)

	for i := 0; i < writers; i++ {
		go func(index int) {
			<-start

			results <- repository.Upsert(
				ctx,
				makeRecord(index),
			)
		}(i)
	}

	close(start)

	for i := 0; i < writers; i++ {
		err := <-results

		// Older snapshots may be rejected depending
		// on the order in which writes are processed.
		if err != nil &&
			!errors.Is(err, inventory.ErrStaleInventory) {
			t.Errorf("unexpected write error: %v", err)
		}
	}

	requireInventory(
		t,
		ctx,
		repository,
		makeRecord(writers-1),
	)
}

func TestRepositoryCascadeDelete(t *testing.T) {
	ctx, db, repository := setupRepository(t)

	first := testSnapshot()

	second := first
	second.DeviceID = otherDeviceID

	requireUpsert(t, ctx, repository, first)
	requireUpsert(t, ctx, repository, second)

	deviceRepository := devices.NewRepository(db)

	if err := deviceRepository.Delete(
		ctx,
		first.DeviceID,
	); err != nil {
		t.Fatal(err)
	}

	_, err := repository.Get(ctx, first.DeviceID)

	requireInventoryError(
		t, err, inventory.ErrNotFound,
	)

	// Deleting one device must not affect another.
	requireInventory(t, ctx, repository, second)
}

func TestRepositoryCanceledContext(t *testing.T) {
	ctx, _, repository := setupRepository(t)

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	record := testSnapshot()

	err := repository.Upsert(canceled, record)

	requireInventoryError(
		t, err, context.Canceled,
	)

	_, err = repository.Get(
		canceled,
		record.DeviceID,
	)

	requireInventoryError(
		t, err, context.Canceled,
	)

	_, err = repository.Get(ctx, record.DeviceID)

	requireInventoryError(
		t, err, inventory.ErrNotFound,
	)
}
