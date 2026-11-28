package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/auth"
	"github.com/tschleining/rackos/internal/appliance/config"
	"github.com/tschleining/rackos/internal/appliance/database"

	"golang.org/x/term"
)

const adminCommandTimeout = 10 * time.Second

var (
	errInteractiveTerminal = errors.New(
		"administrator password entry requires an interactive terminal",
	)

	errPasswordsDoNotMatch = errors.New(
		"passwords do not match",
	)
)

type adminStatusResponse struct {
	Initialized   bool                 `json:"initialized"`
	Administrator *admin.Administrator `json:"administrator,omitempty"`
}

type adminPasswordPrompt func(
	io.Writer,
	string,
) ([]byte, error)

func runAdmin(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	return runAdminWithPrompt(
		ctx,
		args,
		stdout,
		stderr,
		readAdminTerminalPassword,
	)
}

// runAdminWithPrompt allows CLI tests to provide
// synthetic passwords without an actual terminal.
func runAdminWithPrompt(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	prompt adminPasswordPrompt,
) error {
	if len(args) == 0 ||
		args[0] == "help" ||
		args[0] == "--help" ||
		args[0] == "-h" {
		printAdminUsage(stdout)
		return nil
	}

	command := args[0]

	switch command {
	case "init", "status", "reset-password":
	default:
		return errors.New(
			"unknown admin command; run rackosctl admin help",
		)
	}

	defaultDir := config.Default().DataDir

	if value, ok := os.LookupEnv(
		"RACKOS_DATA_DIR",
	); ok {
		defaultDir = value
	}

	flags := flag.NewFlagSet(
		"admin "+command,
		flag.ContinueOnError,
	)

	// Do not echo malformed arguments. A user could
	// accidentally attempt to pass a password as
	// an unsupported command-line option.
	flags.SetOutput(io.Discard)

	dataDir := flags.String(
		"data-dir",
		defaultDir,
		"RackOS appliance data directory",
	)

	var username string
	var confirmOffline bool

	if command != "status" {
		flags.StringVar(
			&username,
			"username",
			"",
			"administrator username",
		)

		flags.BoolVar(
			&confirmOffline,
			"confirm-offline",
			false,
			"confirm the appliance is stopped",
		)
	}

	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printAdminUsage(stdout)
			return nil
		}

		return errors.New(
			"invalid admin arguments; run rackosctl admin help",
		)
	}

	if flags.NArg() != 0 {
		return errors.New(
			"unexpected positional arguments",
		)
	}

	if strings.TrimSpace(*dataDir) == "" {
		return errors.New(
			"data directory cannot be empty",
		)
	}

	if command == "status" {
		return runAdminStatus(
			ctx,
			*dataDir,
			stdout,
		)
	}

	canonicalUsername, err :=
		admin.NormalizeUsername(username)

	if err != nil {
		return err
	}

	if !confirmOffline {
		return errors.New(
			"stop the appliance and provide --confirm-offline",
		)
	}

	// Open the existing database before asking for
	// passwords. Never initialize or migrate it.
	openCtx, cancelOpen := context.WithTimeout(
		ctx,
		adminCommandTimeout,
	)

	db, err := database.OpenExisting(
		openCtx,
		*dataDir,
	)

	cancelOpen()

	if err != nil {
		return err
	}
	defer db.Close()

	repository := admin.NewRepository(db)

	checkCtx, cancelCheck := context.WithTimeout(
		ctx,
		adminCommandTimeout,
	)

	var account admin.Administrator

	switch command {
	case "init":
		var exists bool

		exists, err = repository.Exists(checkCtx)

		if err == nil && exists {
			err = admin.ErrAlreadyInitialized
		}

	case "reset-password":
		account, err = repository.GetByUsername(
			checkCtx,
			canonicalUsername,
		)
	}

	cancelCheck()

	if err != nil {
		return err
	}

	if _, err := fmt.Fprintln(
		stderr,
		"RackOS administrator:", canonicalUsername,
	); err != nil {
		return err
	}

	// Do not start the database operation timeout
	// while the administrator is typing.
	password, err := readConfirmedAdminPassword(
		stderr,
		prompt,
	)
	if err != nil {
		return err
	}
	defer clear(password)

	operationCtx, cancelOperation :=
		context.WithTimeout(
			ctx,
			adminCommandTimeout,
		)
	defer cancelOperation()

	digest, err := auth.HashPassword(
		operationCtx,
		canonicalUsername,
		password,
	)

	// The plaintext is no longer needed.
	clear(password)

	if err != nil {
		return err
	}

	defer clear(digest.Salt[:])
	defer clear(digest.Hash[:])

	account, err = commitAdministratorChange(
		operationCtx,
		db,
		command,
		canonicalUsername,
		account,
		digest,
	)
	if err != nil {
		return err
	}

	switch command {
	case "init":
		_, err = fmt.Fprintf(
			stdout,
			"Administrator initialized: %s\n",
			account.Username,
		)

		return err

	case "reset-password":
		_, err = fmt.Fprintf(
			stdout,
			"Administrator password replaced: %s\n",
			account.Username,
		)

		return err
	}

	return errors.New("unsupported admin command")
}

func runAdminStatus(
	ctx context.Context,
	dataDir string,
	stdout io.Writer,
) error {
	ctx, cancel := context.WithTimeout(
		ctx,
		adminCommandTimeout,
	)
	defer cancel()

	db, err := database.OpenReadOnly(
		ctx,
		dataDir,
	)
	if err != nil {
		return err
	}
	defer db.Close()

	repository := admin.NewRepository(db)

	account, err := repository.Current(ctx)

	response := adminStatusResponse{}

	switch {
	case errors.Is(err, admin.ErrNotFound):
		response.Initialized = false

	case err != nil:
		return err

	default:
		response.Initialized = true
		response.Administrator = &account
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")

	return encoder.Encode(response)
}

func readConfirmedAdminPassword(
	stderr io.Writer,
	prompt adminPasswordPrompt,
) ([]byte, error) {
	password, err := prompt(
		stderr,
		"Password: ",
	)
	if err != nil {
		clear(password)
		return nil, err
	}

	if len(password) > auth.MaxPasswordInputBytes {
		clear(password)
		return nil, auth.ErrPasswordTooLong
	}

	confirmation, err := prompt(
		stderr,
		"Confirm password: ",
	)
	if err != nil {
		clear(password)
		clear(confirmation)
		return nil, err
	}
	defer clear(confirmation)

	if len(confirmation) > auth.MaxPasswordInputBytes {
		clear(password)
		return nil, auth.ErrPasswordTooLong
	}

	if !bytes.Equal(password, confirmation) {
		clear(password)
		return nil, errPasswordsDoNotMatch
	}

	return password, nil
}

func readAdminTerminalPassword(
	stderr io.Writer,
	message string,
) ([]byte, error) {
	fd := int(os.Stdin.Fd())

	if !term.IsTerminal(fd) {
		return nil, errInteractiveTerminal
	}

	if _, err := fmt.Fprint(
		stderr,
		message,
	); err != nil {
		return nil, err
	}

	password, err := term.ReadPassword(fd)

	_, newlineErr := fmt.Fprintln(stderr)

	if err != nil {
		clear(password)

		return nil, errors.New(
			"unable to read administrator password",
		)
	}

	if newlineErr != nil {
		clear(password)
		return nil, newlineErr
	}

	return password, nil
}

func printAdminUsage(w io.Writer) {
	fmt.Fprint(w, `RackOS administrator management

Usage:
  rackosctl admin status [--data-dir DIR]

  rackosctl admin init
    --username USERNAME
    --confirm-offline
    [--data-dir DIR]

  rackosctl admin reset-password
    --username USERNAME
    --confirm-offline
    [--data-dir DIR]

Commands:
  status          Display administrator setup status.
  init            Initialize the first administrator.
  reset-password  Replace the administrator password.

Passwords are entered interactively without echo.

Stop the appliance before running init or
reset-password. The --confirm-offline flag confirms
that you have done so.

The default data directory is ./data, or the value
of RACKOS_DATA_DIR when configured.

Administrator commands require an existing database
with the current schema version.
`)
}
