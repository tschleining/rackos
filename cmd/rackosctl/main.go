package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/tschleining/rackos/internal/appliance/config"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/enrollment"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 ||
		args[0] == "help" ||
		args[0] == "--help" ||
		args[0] == "-h" {
		printUsage(stdout)
		return nil
	}

	// Dispatch administrative command groups before
	// validating enrollment-specific arguments.
	if args[0] == "admin" {
		return runAdmin(
			context.Background(),
			args[1:],
			stdout,
			stderr,
		)
	}

	if args[0] == "devices" {
		return runDevices(
			context.Background(),
			args[1:],
			stdout,
			stderr,
		)
	}

	if args[0] == "storage" {
		return runStorage(
			context.Background(),
			args[1:],
			stdout,
			stderr,
		)
	}

	if len(args) < 2 || args[0] != "enrollment" {
		return errors.New(
			"unknown command; run rackosctl help",
		)
	}

	command := args[1]

	if command != "create" && command != "revoke" {
		return errors.New(
			"unknown command; run rackosctl help",
		)
	}

	flags := flag.NewFlagSet(
		"enrollment "+command,
		flag.ContinueOnError,
	)
	flags.SetOutput(stderr)

	defaultDir := config.Default().DataDir

	if value, ok := os.LookupEnv("RACKOS_DATA_DIR"); ok {
		defaultDir = value
	}

	dataDir := flags.String(
		"data-dir",
		defaultDir,
		"RackOS appliance data directory",
	)

	var ttl *time.Duration
	var tokenID *string

	switch command {
	case "create":
		ttl = flags.Duration(
			"ttl",
			enrollment.DefaultTTL,
			"enrollment token lifetime",
		)

	case "revoke":
		tokenID = flags.String(
			"id",
			"",
			"public enrollment token ID",
		)
	}

	if err := flags.Parse(args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}

	if *dataDir == "" {
		return errors.New("data directory cannot be empty")
	}

	if command == "revoke" && *tokenID == "" {
		return errors.New("token ID is required")
	}

	if command == "create" &&
		(*ttl < time.Second || *ttl > enrollment.MaxTTL) {
		return enrollment.ErrInvalidTTL
	}

	// Administrative commands require an existing
	// appliance database. Do not create one accidentally.
	path := filepath.Join(*dataDir, database.Filename)

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf(
			"cannot find appliance database: %w", err,
		)
	}

	if !info.Mode().IsRegular() {
		return errors.New("invalid appliance database file")
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	db, err := database.Open(ctx, *dataDir)
	if err != nil {
		return err
	}
	defer db.Close()

	switch command {
	case "create":
		issued, err := enrollment.Issue(
			ctx,
			db,
			*ttl,
		)
		if err != nil {
			return err
		}

		fmt.Fprintln(stderr, "Enrollment token created")
		fmt.Fprintln(stderr, "ID:", issued.ID)
		fmt.Fprintln(
			stderr,
			"Expires:",
			issued.ExpiresAt.Format(time.RFC3339),
		)
		fmt.Fprintln(
			stderr,
			"Store this token securely. It cannot be retrieved.",
		)

		_, err = fmt.Fprintln(stdout, issued.Token)
		return err

	case "revoke":
		if err := enrollment.Revoke(
			ctx,
			db,
			*tokenID,
		); err != nil {
			return err
		}

		fmt.Fprintln(stdout, "Enrollment token revoked")
	}

	return nil
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `RackOS administration CLI

Usage:
  rackosctl help
  rackosctl admin status [--data-dir DIR]
  rackosctl admin init --username USERNAME --confirm-offline [--data-dir DIR]
  rackosctl admin reset-password --username USERNAME --confirm-offline [--data-dir DIR]
  rackosctl admin help
  rackosctl enrollment create [--ttl 15m] [--data-dir ./data]
  rackosctl enrollment revoke --id TOKEN_ID [--data-dir ./data]
  rackosctl devices list [--data-dir DIR]
  rackosctl devices show --id ID [--data-dir DIR]
  rackosctl devices revoke --id ID [--data-dir DIR]
  rackosctl devices remove --id ID --confirm [--data-dir DIR]
  rackosctl devices help
  rackosctl storage stats [--data-dir DIR]
  rackosctl storage help

Enrollment tokens are single-use and expire automatically.
Administrator passwords are entered interactively.
`)
}
