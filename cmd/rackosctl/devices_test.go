package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/credentials"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
)

const (
	testAdminDeviceID = "11111111-1111-4111-8111-111111111111"
	testKeepDeviceID  = "22222222-2222-4222-8222-222222222222"
)

func setupDeviceCommandDB(
	t *testing.T,
) (string, *sql.DB) {
	t.Helper()

	dir := t.TempDir()

	db, err := database.Open(
		context.Background(),
		dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		db.Close()
	})

	return dir, db
}

func seedAdminDevice(
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

	token, err := credentials.Issue(ctx, tx, id)
	if err != nil {
		t.Fatal(err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	return token
}

func executeDeviceCLI(
	args ...string,
) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run(args, &stdout, &stderr)

	return stdout.String(), stderr.String(), err
}

func TestDeviceCommandDispatch(t *testing.T) {
	commands := [][]string{
		{"help"},
		{"devices"},
		{"devices", "help"},
		{"devices", "list", "--help"},
		{"devices", "show", "--help"},
		{"devices", "revoke", "--help"},
		{"devices", "remove", "--help"},
		{"enrollment", "create", "--help"},
		{"enrollment", "revoke", "--help"},
	}

	for _, args := range commands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdout, stderr, err := executeDeviceCLI(args...)

			if err != nil {
				t.Fatal(err)
			}

			if strings.TrimSpace(stdout+stderr) == "" {
				t.Fatal("expected help output")
			}
		})
	}
}

func TestDevicesListCommand(t *testing.T) {
	dir, db := setupDeviceCommandDB(t)

	seedAdminDevice(t, db, testAdminDeviceID)
	seedAdminDevice(t, db, testKeepDeviceID)

	stdout, _, err := executeDeviceCLI(
		"devices", "list",
		"--data-dir", dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	var response []cliDeviceResponse

	if err := json.Unmarshal(
		[]byte(stdout),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if len(response) != 2 {
		t.Fatalf(
			"expected two devices, got %d",
			len(response),
		)
	}

	found := make(map[string]bool)

	for _, device := range response {
		found[device.ID] = true

		if device.Status != "online" {
			t.Fatal("expected online device")
		}
	}

	if !found[testAdminDeviceID] ||
		!found[testKeepDeviceID] {
		t.Fatal("expected both registered devices")
	}
}

func TestDevicesEmptyListCommand(t *testing.T) {
	dir, _ := setupDeviceCommandDB(t)

	stdout, _, err := executeDeviceCLI(
		"devices", "list",
		"--data-dir", dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(stdout) != "[]" {
		t.Fatalf(
			"expected empty JSON array, got %s",
			stdout,
		)
	}
}

func TestDevicesShowCommand(t *testing.T) {
	dir, db := setupDeviceCommandDB(t)

	token := seedAdminDevice(
		t,
		db,
		testAdminDeviceID,
	)

	stdout, _, err := executeDeviceCLI(
		"devices", "show",
		"--id", testAdminDeviceID,
		"--data-dir", dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	var response cliDeviceResponse

	if err := json.Unmarshal(
		[]byte(stdout),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response.ID != testAdminDeviceID ||
		response.Hostname != "test-device.local" ||
		response.OS != "linux" ||
		response.Architecture != "amd64" {
		t.Fatal("incorrect device details")
	}

	if strings.Contains(stdout, token) ||
		strings.Contains(stdout, "token_hash") ||
		strings.Contains(stdout, "agent_token") {
		t.Fatal("credential information leaked")
	}
}

func TestDevicesShowUnknownDevice(t *testing.T) {
	dir, _ := setupDeviceCommandDB(t)

	_, _, err := executeDeviceCLI(
		"devices", "show",
		"--id", testAdminDeviceID,
		"--data-dir", dir,
	)

	if !errors.Is(err, devices.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDevicesRevokeCommand(t *testing.T) {
	ctx := context.Background()
	dir, db := setupDeviceCommandDB(t)

	token := seedAdminDevice(
		t,
		db,
		testAdminDeviceID,
	)

	keepToken := seedAdminDevice(
		t,
		db,
		testKeepDeviceID,
	)

	if err := credentials.Authenticate(
		ctx, db, testAdminDeviceID, token,
	); err != nil {
		t.Fatal(err)
	}

	_, _, err := executeDeviceCLI(
		"devices", "revoke",
		"--id", testAdminDeviceID,
		"--data-dir", dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = credentials.Authenticate(
		ctx, db, testAdminDeviceID, token,
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

	repository := devices.NewRepository(db)

	if _, err := repository.Get(
		ctx,
		testAdminDeviceID,
	); err != nil {
		t.Fatal("revocation removed device registration")
	}

	if err := credentials.Authenticate(
		ctx, db, testKeepDeviceID, keepToken,
	); err != nil {
		t.Fatal("unrelated credential was affected")
	}
}

func TestDevicesRemoveRequiresConfirmation(t *testing.T) {
	ctx := context.Background()
	dir, db := setupDeviceCommandDB(t)

	token := seedAdminDevice(
		t,
		db,
		testAdminDeviceID,
	)

	_, _, err := executeDeviceCLI(
		"devices", "remove",
		"--id", testAdminDeviceID,
		"--data-dir", dir,
	)

	if err == nil ||
		!strings.Contains(err.Error(), "--confirm") {
		t.Fatal("expected confirmation error")
	}

	repository := devices.NewRepository(db)

	if _, err := repository.Get(
		ctx,
		testAdminDeviceID,
	); err != nil {
		t.Fatal("unconfirmed removal deleted device")
	}

	if err := credentials.Authenticate(
		ctx, db, testAdminDeviceID, token,
	); err != nil {
		t.Fatal("unconfirmed removal affected credential")
	}
}

func TestDevicesRemoveCommand(t *testing.T) {
	ctx := context.Background()
	dir, db := setupDeviceCommandDB(t)

	token := seedAdminDevice(
		t,
		db,
		testAdminDeviceID,
	)

	keepToken := seedAdminDevice(
		t,
		db,
		testKeepDeviceID,
	)

	_, _, err := executeDeviceCLI(
		"devices", "remove",
		"--id", testAdminDeviceID,
		"--confirm",
		"--data-dir", dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	repository := devices.NewRepository(db)

	_, err = repository.Get(ctx, testAdminDeviceID)

	if !errors.Is(err, devices.ErrNotFound) {
		t.Fatal("device was not removed")
	}

	err = credentials.Authenticate(
		ctx, db, testAdminDeviceID, token,
	)

	if !errors.Is(
		err,
		credentials.ErrUnauthenticated,
	) {
		t.Fatal("removed credential still authenticates")
	}

	if _, err := repository.Get(
		ctx,
		testKeepDeviceID,
	); err != nil {
		t.Fatal("unrelated device was removed")
	}

	if err := credentials.Authenticate(
		ctx, db, testKeepDeviceID, keepToken,
	); err != nil {
		t.Fatal("unrelated credential was affected")
	}
}

func TestDevicesMissingDatabase(t *testing.T) {
	dir := t.TempDir()

	_, _, err := executeDeviceCLI(
		"devices", "list",
		"--data-dir", dir,
	)

	if err == nil {
		t.Fatal("expected missing database error")
	}

	path := filepath.Join(dir, database.Filename)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("command unexpectedly created a database")
	}
}

func TestDevicesInvalidArguments(t *testing.T) {
	commands := [][]string{
		{"devices", "unknown"},
		{"devices", "show"},
		{"devices", "revoke"},
		{"devices", "remove", "--confirm"},
		{"devices", "list", "--data-dir", ""},
		{"devices", "list", "unexpected"},
		{"devices", "list", "--unknown"},
	}

	for _, args := range commands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, _, err := executeDeviceCLI(args...)

			if err == nil {
				t.Fatal("expected argument validation error")
			}
		})
	}
}

func TestDevicesDataDirectoryEnvironment(t *testing.T) {
	dir, db := setupDeviceCommandDB(t)

	seedAdminDevice(t, db, testAdminDeviceID)

	t.Setenv("RACKOS_DATA_DIR", dir)

	stdout, _, err := executeDeviceCLI(
		"devices", "list",
	)
	if err != nil {
		t.Fatal(err)
	}

	var response []cliDeviceResponse

	if err := json.Unmarshal(
		[]byte(stdout),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if len(response) != 1 ||
		response[0].ID != testAdminDeviceID {
		t.Fatal("environment data directory was not used")
	}
}
