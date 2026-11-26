package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/tschleining/rackos/internal/agent/client"
	"github.com/tschleining/rackos/internal/agent/state"

	"google.golang.org/grpc/status"
)

const version = "0.1.0-dev"

func main() {
	logger := slog.New(
		slog.NewTextHandler(
			os.Stderr,
			&slog.HandlerOptions{
				Level: slog.LevelInfo,
			},
		),
	)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	err := run(
		ctx,
		os.Args[1:],
		os.Stdin,
		os.Stdout,
		logger,
	)

	if err != nil {
		logger.Error(
			"RackOS agent failed",
			"error", err.Error(),
		)
		stop()
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	logger *slog.Logger,
) error {
	if len(args) == 0 ||
		args[0] == "help" ||
		args[0] == "--help" ||
		args[0] == "-h" {
		printUsage(stdout)
		return nil
	}

	if args[0] == "version" ||
		args[0] == "--version" {
		fmt.Fprintln(stdout, version)
		return nil
	}

	command := args[0]

	if command == "inventory" {
		if len(args) == 2 &&
			(args[1] == "--help" || args[1] == "-h") {
			fmt.Fprintln(stdout, "Usage: rackos-agent inventory")
			fmt.Fprintln(stdout, "")
			fmt.Fprintln(stdout, "Collect local system and hardware inventory.")
			return nil
		}

		if len(args) != 1 {
			return errors.New(
				"inventory does not accept arguments",
			)
		}

		return runInventory(ctx, stdout)
	}

	if command != "enroll" && command != "run" {

		return errors.New(
			"unknown command; run rackos-agent help",
		)
	}

	defaultDir, err := state.DefaultDir()
	if err != nil {
		return err
	}

	flags := flag.NewFlagSet(
		command,
		flag.ContinueOnError,
	)

	stateDir := flags.String(
		"state-dir",
		defaultDir,
		"agent state directory",
	)

	var address *string
	var caPath *string
	var debug *bool

	switch command {
	case "enroll":
		address = flags.String(
			"address",
			"127.0.0.1:50051",
			"RackOS appliance address",
		)

		caPath = flags.String(
			"ca",
			"",
			"trusted appliance CA certificate",
		)

	case "run":
		debug = flags.Bool(
			"debug",
			false,
			"enable debug logging",
		)

		address = flags.String(
			"address",
			"",
			"override the stored appliance address",
		)
	}

	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}

	switch command {
	case "enroll":
		return enroll(
			ctx,
			*stateDir,
			*address,
			*caPath,
			stdin,
			logger,
		)

	case "run":
		if *debug {
			logger = slog.New(
				slog.NewTextHandler(
					os.Stderr,
					&slog.HandlerOptions{
						Level: slog.LevelDebug,
					},
				),
			)
		}

		return runAgent(
			ctx,
			*stateDir,
			*address,
			logger,
		)
	}

	return nil
}

func enroll(
	ctx context.Context,
	stateDir string,
	address string,
	caPath string,
	stdin io.Reader,
	logger *slog.Logger,
) error {
	if caPath == "" {
		return errors.New("trusted CA file is required")
	}

	if err := state.Prepare(stateDir); err != nil {
		return err
	}

	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return fmt.Errorf("read trusted CA: %w", err)
	}

	connection, err := client.New(address, caPEM)
	if err != nil {
		return err
	}
	defer connection.Close()

	logger.Info(
		"Reading enrollment token from standard input",
	)

	data, err := io.ReadAll(
		io.LimitReader(stdin, 257),
	)
	if err != nil {
		return errors.New("read enrollment token")
	}

	if len(data) > 256 {
		return errors.New("enrollment token is too long")
	}

	token := strings.TrimSpace(string(data))

	if !strings.HasPrefix(token, "rackos_enroll_") {
		return errors.New("invalid enrollment token format")
	}

	response, err := connection.Enroll(
		ctx,
		token,
		version,
	)
	if err != nil {
		return fmt.Errorf(
			"enrollment RPC failed: %s",
			status.Code(err),
		)
	}

	saved := state.State{
		ApplianceAddress:         address,
		CAPEM:                    string(caPEM),
		InstallationID:           response.GetInstallationId(),
		DeviceID:                 response.GetDeviceId(),
		AgentToken:               response.GetAgentToken(),
		HeartbeatIntervalSeconds: response.GetHeartbeatIntervalSeconds(),
	}

	if err := state.Save(stateDir, saved); err != nil {
		return fmt.Errorf(
			"enrollment succeeded remotely but local state could not be saved; administrator recovery required: %w",
			err,
		)
	}

	logger.Info(
		"Agent enrollment successful",
		"device_id", saved.DeviceID,
		"installation_id", saved.InstallationID,
		"appliance", saved.ApplianceAddress,
		"heartbeat_interval_seconds",
		saved.HeartbeatIntervalSeconds,
	)

	return nil
}

func runAgent(
	ctx context.Context,
	stateDir string,
	addressOverride string,
	logger *slog.Logger,
) error {
	saved, err := state.Load(stateDir)
	if err != nil {
		return err
	}

	address := saved.ApplianceAddress

	if addressOverride != "" {
		address = addressOverride
	}

	connection, err := client.New(
		address,
		[]byte(saved.CAPEM),
	)
	if err != nil {
		return err
	}
	defer connection.Close()

	logger.Info(
		"RackOS agent started",
		"version", version,
		"device_id", saved.DeviceID,
		"installation_id", saved.InstallationID,
		"appliance", address,
	)

	err = connection.Run(
		ctx,
		saved.DeviceID,
		saved.AgentToken,
		logger,
	)

	if err != nil {
		return err
	}

	logger.Info("RackOS agent stopped")

	return nil
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `RackOS monitoring agent

Usage:
  rackos-agent help
  rackos-agent version
  rackos-agent inventory
  rackos-agent enroll --address HOST:PORT --ca FILE [--state-dir DIR]
  rackos-agent run [--state-dir DIR] [--address HOST:PORT] [--debug]

The inventory command collects local system and hardware
information without requiring enrollment or an appliance
connection.

Enrollment tokens are read from standard input.
Agent credentials are stored in the local agent state directory.

The optional run address overrides the stored appliance address
for the current execution without modifying agent state.
`)
}
