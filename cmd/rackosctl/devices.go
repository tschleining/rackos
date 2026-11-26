package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/tschleining/rackos/internal/appliance/credentials"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
)

const deviceCommandTimeout = 10 * time.Second

type cliDeviceResponse struct {
	ID           string    `json:"id"`
	Hostname     string    `json:"hostname"`
	OS           string    `json:"os"`
	Architecture string    `json:"architecture"`
	AgentVersion string    `json:"agent_version"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

func runDevices(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if len(args) == 0 ||
		args[0] == "help" ||
		args[0] == "--help" ||
		args[0] == "-h" {
		printDevicesUsage(stdout)
		return nil
	}

	command := args[0]

	switch command {
	case "list", "show", "revoke", "remove":
	default:
		return fmt.Errorf(
			"unknown devices command: %q",
			command,
		)
	}

	defaultDir := "./data"

	if value, ok := os.LookupEnv("RACKOS_DATA_DIR"); ok {
		defaultDir = value
	}

	flags := flag.NewFlagSet(
		"devices "+command,
		flag.ContinueOnError,
	)

	flags.SetOutput(stderr)

	dataDir := flags.String(
		"data-dir",
		defaultDir,
		"appliance data directory",
	)

	var id string
	var confirm bool

	if command != "list" {
		flags.StringVar(
			&id,
			"id",
			"",
			"registered device ID",
		)
	}

	if command == "remove" {
		flags.BoolVar(
			&confirm,
			"confirm",
			false,
			"confirm permanent device removal",
		)
	}

	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	if flags.NArg() != 0 {
		return errors.New(
			"unexpected positional arguments",
		)
	}

	if *dataDir == "" {
		return errors.New(
			"data directory must not be empty",
		)
	}

	if command != "list" && id == "" {
		return errors.New("--id is required")
	}

	if command == "remove" && !confirm {
		return errors.New(
			"permanent removal requires --confirm",
		)
	}

	// Administrative commands must operate on an existing
	// appliance database, not initialize a new installation.
	dbPath := filepath.Join(
		*dataDir,
		database.Filename,
	)

	info, err := os.Stat(dbPath)
	if err != nil {
		return fmt.Errorf(
			"access appliance database: %w",
			err,
		)
	}

	if !info.Mode().IsRegular() {
		return errors.New(
			"appliance database is not a regular file",
		)
	}

	ctx, cancel := context.WithTimeout(
		ctx,
		deviceCommandTimeout,
	)
	defer cancel()

	db, err := database.Open(ctx, *dataDir)
	if err != nil {
		return fmt.Errorf(
			"open appliance database: %w",
			err,
		)
	}
	defer db.Close()

	repository := devices.NewRepository(db)

	switch command {
	case "list":
		records, err := repository.List(ctx)
		if err != nil {
			return err
		}

		now := time.Now().UTC()

		response := make(
			[]cliDeviceResponse,
			0,
			len(records),
		)

		for _, device := range records {
			response = append(
				response,
				toCLIDeviceResponse(device, now),
			)
		}

		return writeDeviceOutput(stdout, response)

	case "show":
		device, err := repository.Get(ctx, id)
		if err != nil {
			return err
		}

		return writeDeviceOutput(
			stdout,
			toCLIDeviceResponse(
				device,
				time.Now().UTC(),
			),
		)

	case "revoke":
		if err := credentials.Revoke(
			ctx,
			db,
			id,
		); err != nil {
			return fmt.Errorf(
				"revoke device credential: %w",
				err,
			)
		}

		_, err := fmt.Fprintf(
			stdout,
			"Agent credential revoked: %s\n",
			id,
		)

		return err

	case "remove":
		if err := repository.Delete(
			ctx,
			id,
		); err != nil {
			return fmt.Errorf(
				"remove device: %w",
				err,
			)
		}

		_, err := fmt.Fprintf(
			stdout,
			"Device permanently removed: %s\n",
			id,
		)

		return err
	}

	return errors.New("unsupported devices command")
}

func toCLIDeviceResponse(
	device devices.Device,
	now time.Time,
) cliDeviceResponse {
	return cliDeviceResponse{
		ID:           device.ID,
		Hostname:     device.Hostname,
		OS:           device.OS,
		Architecture: device.Architecture,
		AgentVersion: device.AgentVersion,

		Status: string(device.Status(
			now,
			devices.DefaultOfflineTimeout,
		)),

		CreatedAt:  device.CreatedAt.UTC(),
		UpdatedAt:  device.UpdatedAt.UTC(),
		LastSeenAt: device.LastSeenAt.UTC(),
	}
}

func writeDeviceOutput(
	w io.Writer,
	value any,
) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	return encoder.Encode(value)
}

func printDevicesUsage(w io.Writer) {
	fmt.Fprint(w, `RackOS device administration

Usage:
  rackosctl devices list [--data-dir DIR]
  rackosctl devices show --id ID [--data-dir DIR]
  rackosctl devices revoke --id ID [--data-dir DIR]
  rackosctl devices remove --id ID --confirm [--data-dir DIR]

Commands:
  list      List registered devices.
  show      Display a registered device.
  revoke    Revoke a device's agent credential.
  remove    Permanently delete a device and its credential.

Revoking credentials preserves the device registration.
Removing a device permanently deletes its registration.

Stop the appliance before performing revoke or remove
operations.

The default data directory is ./data, or the value of
RACKOS_DATA_DIR when configured.
`)
}
