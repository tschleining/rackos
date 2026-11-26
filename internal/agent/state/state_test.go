package state

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/pki"
)

func testState(t *testing.T) State {
	t.Helper()

	if runtime.GOOS != "linux" &&
		runtime.GOOS != "darwin" {
		t.Skip("secure state storage not supported")
	}

	dir := t.TempDir()

	_, err := pki.LoadOrCreate(
		dir, "test-installation",
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

	token := "rackos_agent_" +
		base64.RawURLEncoding.EncodeToString(
			make([]byte, 32),
		)

	return State{
		ApplianceAddress:         "127.0.0.1:50051",
		CAPEM:                    string(caPEM),
		InstallationID:           "test-installation",
		DeviceID:                 "test-device",
		AgentToken:               token,
		HeartbeatIntervalSeconds: 30,
	}
}

func TestSaveAndLoad(t *testing.T) {
	expected := testState(t)
	dir := filepath.Join(t.TempDir(), "agent")

	if err := Save(dir, expected); err != nil {
		t.Fatal(err)
	}

	actual, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(expected, actual) {
		t.Fatal("agent state did not survive reload")
	}

	info, err := os.Stat(
		filepath.Join(dir, Filename),
	)
	if err != nil {
		t.Fatal(err)
	}

	if info.Mode().Perm() != 0600 {
		t.Fatal("unexpected state file permissions")
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}

	if dirInfo.Mode().Perm() != 0700 {
		t.Fatal("unexpected state directory permissions")
	}
}

func TestRejectOverwrite(t *testing.T) {
	original := testState(t)
	dir := filepath.Join(t.TempDir(), "agent")

	if err := Save(dir, original); err != nil {
		t.Fatal(err)
	}

	replacement := original
	replacement.DeviceID = "different-device"

	err := Save(dir, replacement)

	if !errors.Is(err, ErrAlreadyEnrolled) {
		t.Fatalf("expected overwrite rejection: %v", err)
	}

	actual, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if actual.DeviceID != original.DeviceID {
		t.Fatal("existing agent identity was replaced")
	}
}

func TestRejectInsecurePermissions(t *testing.T) {
	saved := testState(t)
	dir := filepath.Join(t.TempDir(), "agent")

	if err := Save(dir, saved); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, Filename)

	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(dir); err == nil {
		t.Fatal("insecure state permissions accepted")
	}

	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(dir); err == nil {
		t.Fatal("insecure directory permissions accepted")
	}
}

func TestRejectCorruptState(t *testing.T) {
	saved := testState(t)
	dir := filepath.Join(t.TempDir(), "agent")

	if err := Save(dir, saved); err != nil {
		t.Fatal(err)
	}

	err := os.WriteFile(
		filepath.Join(dir, Filename),
		[]byte("invalid JSON"),
		0600,
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Load(dir); err == nil {
		t.Fatal("corrupt state accepted")
	}
}

func TestRejectInvalidCredential(t *testing.T) {
	saved := testState(t)
	saved.AgentToken = "invalid-token"

	dir := filepath.Join(t.TempDir(), "agent")

	if err := Save(dir, saved); err == nil {
		t.Fatal("invalid agent credential accepted")
	}
}

func TestRejectInvalidCA(t *testing.T) {
	saved := testState(t)
	saved.CAPEM = "invalid certificate"

	dir := filepath.Join(t.TempDir(), "agent")

	if err := Save(dir, saved); err == nil {
		t.Fatal("invalid CA accepted")
	}
}
