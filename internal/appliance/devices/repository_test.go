package devices

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/database"
)

const testDeviceID = "e2a104d8-9c72-4b31-8fa2-63001b24c9e8"
const secondDeviceID = "e2a104d8-9c72-4b31-8fa2-63001b24c9e9"

func testInfo() DeviceInfo {
	return DeviceInfo{
		Hostname:     "server-01",
		OS:           "linux",
		Architecture: "amd64",
		AgentVersion: "0.1.0",
	}
}

func testRepository(t *testing.T) *Repository {
	t.Helper()

	db, err := database.Open(
		context.Background(),
		t.TempDir(),
	)

	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})

	repo := NewRepository(db)

	repo.now = func() time.Time {
		return time.Date(
			2026, 1, 1, 12, 0, 0, 0, time.UTC,
		)
	}

	return repo
}

func TestCreateDevice(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)

	device, err := repo.Create(
		ctx,
		testDeviceID,
		testInfo(),
	)

	if err != nil {
		t.Fatal(err)
	}

	if device.ID != testDeviceID {
		t.Errorf("unexpected ID: %s", device.ID)
	}

	if device.DeviceInfo != testInfo() {
		t.Errorf("unexpected device info: %+v", device)
	}

	if !device.CreatedAt.Equal(repo.now()) {
		t.Errorf("unexpected creation time")
	}

	if !device.LastSeenAt.Equal(repo.now()) {
		t.Errorf("unexpected last seen time")
	}

	stored, err := repo.Get(ctx, testDeviceID)
	if err != nil {
		t.Fatal(err)
	}

	if stored != device {
		t.Errorf("stored device differs: %+v", stored)
	}
}

func TestDuplicateDevice(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)

	if _, err := repo.Create(
		ctx, testDeviceID, testInfo(),
	); err != nil {
		t.Fatal(err)
	}

	info := testInfo()
	info.Hostname = "different-hostname"

	_, err := repo.Create(ctx, testDeviceID, info)

	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected duplicate error, got %v", err)
	}

	stored, err := repo.Get(ctx, testDeviceID)
	if err != nil {
		t.Fatal(err)
	}

	if stored.Hostname != testInfo().Hostname {
		t.Fatal("duplicate registration overwrote device")
	}
}

func TestInvalidDevice(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)

	_, err := repo.Create(ctx, "invalid-id", testInfo())

	if !errors.Is(err, ErrInvalidDevice) {
		t.Fatalf("expected invalid device error, got %v", err)
	}

	info := testInfo()
	info.Hostname = " "

	_, err = repo.Create(ctx, testDeviceID, info)

	if !errors.Is(err, ErrInvalidDevice) {
		t.Fatalf("expected invalid device error, got %v", err)
	}
}

func TestGetUnknownDevice(t *testing.T) {
	repo := testRepository(t)

	_, err := repo.Get(
		context.Background(),
		testDeviceID,
	)

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestListDevices(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(list) != 0 {
		t.Fatal("expected empty device list")
	}

	if _, err := repo.Create(
		ctx, testDeviceID, testInfo(),
	); err != nil {
		t.Fatal(err)
	}

	info := testInfo()
	info.Hostname = "server-02"

	if _, err := repo.Create(
		ctx, secondDeviceID, info,
	); err != nil {
		t.Fatal(err)
	}

	list, err = repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(list) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(list))
	}

	if list[0].ID != testDeviceID ||
		list[1].ID != secondDeviceID {
		t.Fatal("unexpected device list ordering")
	}
}

func TestHeartbeat(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)

	device, err := repo.Create(
		ctx, testDeviceID, testInfo(),
	)
	if err != nil {
		t.Fatal(err)
	}

	later := device.LastSeenAt.Add(30 * time.Second)

	repo.now = func() time.Time {
		return later
	}

	if err := repo.Heartbeat(ctx, testDeviceID); err != nil {
		t.Fatal(err)
	}

	updated, err := repo.Get(ctx, testDeviceID)
	if err != nil {
		t.Fatal(err)
	}

	if !updated.LastSeenAt.Equal(later) {
		t.Fatal("heartbeat did not update last seen")
	}

	if !updated.UpdatedAt.Equal(later) {
		t.Fatal("heartbeat did not update modification time")
	}

	if !updated.CreatedAt.Equal(device.CreatedAt) {
		t.Fatal("heartbeat modified creation time")
	}
}

func TestHeartbeatClockRegression(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)

	device, err := repo.Create(
		ctx, testDeviceID, testInfo(),
	)
	if err != nil {
		t.Fatal(err)
	}

	repo.now = func() time.Time {
		return device.LastSeenAt.Add(-time.Minute)
	}

	if err := repo.Heartbeat(ctx, testDeviceID); err != nil {
		t.Fatal(err)
	}

	stored, err := repo.Get(ctx, testDeviceID)
	if err != nil {
		t.Fatal(err)
	}

	if !stored.LastSeenAt.Equal(device.LastSeenAt) {
		t.Fatal("heartbeat moved last seen backward")
	}
}

func TestHeartbeatUnknownDevice(t *testing.T) {
	repo := testRepository(t)

	err := repo.Heartbeat(
		context.Background(),
		testDeviceID,
	)

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
}

func TestDeviceStatus(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		lastSeen time.Time
		want     Status
	}{
		{
			name:     "online",
			lastSeen: now.Add(-30 * time.Second),
			want:     StatusOnline,
		},
		{
			name:     "threshold",
			lastSeen: now.Add(-90 * time.Second),
			want:     StatusOffline,
		},
		{
			name:     "offline",
			lastSeen: now.Add(-120 * time.Second),
			want:     StatusOffline,
		},
		{
			name: "never seen",
			want: StatusOffline,
		},
		{
			name:     "future timestamp",
			lastSeen: now.Add(time.Second),
			want:     StatusOffline,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device := Device{
				LastSeenAt: tt.lastSeen,
			}

			got := device.Status(
				now, DefaultOfflineTimeout,
			)

			if got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}
