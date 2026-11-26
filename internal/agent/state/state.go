package state

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const Filename = "agent.json"

var ErrAlreadyEnrolled = errors.New(
	"agent is already enrolled",
)

type State struct {
	ApplianceAddress         string `json:"appliance_address"`
	CAPEM                    string `json:"ca_pem"`
	InstallationID           string `json:"installation_id"`
	DeviceID                 string `json:"device_id"`
	AgentToken               string `json:"agent_token"`
	HeartbeatIntervalSeconds uint32 `json:"heartbeat_interval_seconds"`
}

func DefaultDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "RackOS", "agent"), nil
}

func (s State) Validate() error {
	if s.ApplianceAddress == "" ||
		s.InstallationID == "" ||
		s.DeviceID == "" {
		return errors.New("incomplete agent identity")
	}

	const prefix = "rackos_agent_"

	encoded, ok := strings.CutPrefix(
		s.AgentToken, prefix,
	)
	if !ok || len(encoded) != 43 {
		return errors.New("invalid agent credential")
	}

	decoded, err := base64.RawURLEncoding.
		Strict().DecodeString(encoded)

	if err != nil ||
		len(decoded) != 32 ||
		base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return errors.New("invalid agent credential")
	}

	roots := x509.NewCertPool()

	if !roots.AppendCertsFromPEM([]byte(s.CAPEM)) {
		return errors.New("invalid appliance CA")
	}

	if s.HeartbeatIntervalSeconds == 0 ||
		s.HeartbeatIntervalSeconds > 3600 {
		return errors.New("invalid heartbeat interval")
	}

	return nil
}

// Prepare verifies that a new enrollment can be stored
// without overwriting an existing agent identity.
func Prepare(dir string) error {
	if err := supportedPlatform(); err != nil {
		return err
	}

	if dir == "" {
		return errors.New("agent state directory is required")
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	if err := checkDirectory(dir); err != nil {
		return err
	}

	_, err := os.Lstat(filepath.Join(dir, Filename))

	if err == nil {
		return ErrAlreadyEnrolled
	}

	if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

func Save(dir string, s State) error {
	if err := s.Validate(); err != nil {
		return err
	}

	if err := Prepare(dir); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return errors.New("encode agent state")
	}

	data = append(data, '\n')

	file, err := os.CreateTemp(dir, ".agent-*")
	if err != nil {
		return err
	}

	defer os.Remove(file.Name())
	defer file.Close()

	if err := file.Chmod(0600); err != nil {
		return err
	}

	if _, err := file.Write(data); err != nil {
		return err
	}

	if err := file.Sync(); err != nil {
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	// Publish the completed state file without replacing
	// an existing enrollment.
	err = os.Link(
		file.Name(),
		filepath.Join(dir, Filename),
	)

	if errors.Is(err, os.ErrExist) {
		return ErrAlreadyEnrolled
	}

	return err
}

func Load(dir string) (State, error) {
	if err := supportedPlatform(); err != nil {
		return State{}, err
	}

	if err := checkDirectory(dir); err != nil {
		return State{}, err
	}

	path := filepath.Join(dir, Filename)

	info, err := os.Lstat(path)
	if err != nil {
		return State{}, fmt.Errorf(
			"load agent state: %w", err,
		)
	}

	if !info.Mode().IsRegular() ||
		info.Mode().Perm()&0077 != 0 {
		return State{}, errors.New(
			"agent state file must be private and regular",
		)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, err
	}

	var s State

	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, errors.New("invalid agent state file")
	}

	if err := s.Validate(); err != nil {
		return State{}, err
	}

	return s, nil
}

func checkDirectory(dir string) error {
	if dir == "" {
		return errors.New("agent state directory is required")
	}

	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}

	if !info.IsDir() ||
		info.Mode().Perm()&0077 != 0 {
		return errors.New(
			"agent state directory must be private",
		)
	}

	return nil
}

func supportedPlatform() error {
	switch runtime.GOOS {
	case "darwin", "linux":
		return nil
	default:
		return errors.New(
			"secure agent state storage is not supported on this platform yet",
		)
	}
}
