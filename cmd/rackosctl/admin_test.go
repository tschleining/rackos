package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/auth"
	"github.com/tschleining/rackos/internal/appliance/database"
)

const (
	testInitialAdminPassword = "purple clouds above the forest"

	testReplacementAdminPassword = "silver rivers beneath the mountains"
)

func executeAdminCLI(
	prompt adminPasswordPrompt,
	args ...string,
) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runAdminWithPrompt(
		context.Background(),
		args,
		&stdout,
		&stderr,
		prompt,
	)

	return stdout.String(), stderr.String(), err
}

func adminTestPrompt(
	passwords ...string,
) adminPasswordPrompt {
	index := 0

	return func(
		_ io.Writer,
		_ string,
	) ([]byte, error) {
		if index >= len(passwords) {
			return nil, errors.New(
				"unexpected password prompt",
			)
		}

		password := []byte(passwords[index])
		index++

		return password, nil
	}
}

func TestAdminCommandDispatch(t *testing.T) {
	commands := [][]string{
		{"admin"},
		{"admin", "help"},
		{"admin", "--help"},
		{"admin", "init", "--help"},
		{"admin", "status", "--help"},
		{"admin", "reset-password", "--help"},
	}

	for _, args := range commands {
		t.Run(
			strings.Join(args, " "),
			func(t *testing.T) {
				var stdout bytes.Buffer
				var stderr bytes.Buffer

				err := run(
					args,
					&stdout,
					&stderr,
				)
				if err != nil {
					t.Fatal(err)
				}

				if stdout.Len() == 0 {
					t.Fatal("expected help output")
				}
			},
		)
	}
}

func TestAdminStatusBeforeInitialization(
	t *testing.T,
) {
	dir, _ := setupDeviceCommandDB(t)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := run(
		[]string{
			"admin",
			"status",
			"--data-dir",
			dir,
		},
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatal(err)
	}

	var response adminStatusResponse

	if err := json.Unmarshal(
		stdout.Bytes(),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response.Initialized ||
		response.Administrator != nil {
		t.Fatal("fresh appliance is already initialized")
	}
}

func TestAdminInitializationAndRecovery(
	t *testing.T,
) {
	ctx := context.Background()
	dir, db := setupDeviceCommandDB(t)

	repository := admin.NewRepository(db)

	stdout, stderr, err := executeAdminCLI(
		adminTestPrompt(
			testInitialAdminPassword,
			testInitialAdminPassword,
		),
		"init",
		"--username", "Admin",
		"--confirm-offline",
		"--data-dir", dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(
		stdout,
		"Administrator initialized: admin",
	) {
		t.Fatal("missing initialization confirmation")
	}

	if strings.Contains(
		stdout+stderr,
		testInitialAdminPassword,
	) {
		t.Fatal("initial password leaked in CLI output")
	}

	original, err :=
		repository.GetAuthenticationRecord(
			ctx,
			"admin",
		)
	if err != nil {
		t.Fatal(err)
	}

	valid, err := auth.VerifyPassword(
		ctx,
		[]byte(testInitialAdminPassword),
		original.Password,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !valid {
		t.Fatal("initial password does not authenticate")
	}

	stdout, stderr, err = executeAdminCLI(
		adminTestPrompt(
			testReplacementAdminPassword,
			testReplacementAdminPassword,
		),
		"reset-password",
		"--username", "admin",
		"--confirm-offline",
		"--data-dir", dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(
		stdout,
		"Administrator password replaced: admin",
	) {
		t.Fatal("missing recovery confirmation")
	}

	if strings.Contains(
		stdout+stderr,
		testReplacementAdminPassword,
	) {
		t.Fatal("replacement password leaked")
	}

	replacement, err :=
		repository.GetAuthenticationRecord(
			ctx,
			"admin",
		)
	if err != nil {
		t.Fatal(err)
	}

	if replacement.Administrator.ID !=
		original.Administrator.ID {
		t.Fatal("password recovery changed account identity")
	}

	if replacement.Password.Salt ==
		original.Password.Salt {
		t.Fatal("password recovery reused salt")
	}

	if !replacement.Administrator.
		PasswordChangedAt.After(
		original.Administrator.PasswordChangedAt,
	) {
		t.Fatal("password revision did not advance")
	}

	valid, err = auth.VerifyPassword(
		ctx,
		[]byte(testReplacementAdminPassword),
		replacement.Password,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !valid {
		t.Fatal("replacement password does not authenticate")
	}

	valid, err = auth.VerifyPassword(
		ctx,
		[]byte(testInitialAdminPassword),
		replacement.Password,
	)
	if err != nil {
		t.Fatal(err)
	}

	if valid {
		t.Fatal("previous password still authenticates")
	}
}

func TestAdminStatusAfterInitialization(
	t *testing.T,
) {
	dir, db := setupDeviceCommandDB(t)

	_, err := admin.NewRepository(db).CreateInitial(
		context.Background(),
		"admin",
		admin.PasswordDigest{
			Scheme: admin.PasswordScheme,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	stdout, _, err := executeAdminCLI(
		adminTestPrompt(),
		"status",
		"--data-dir", dir,
	)
	if err != nil {
		t.Fatal(err)
	}

	var response adminStatusResponse

	if err := json.Unmarshal(
		[]byte(stdout),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if !response.Initialized ||
		response.Administrator == nil {
		t.Fatal("missing administrator status")
	}

	if response.Administrator.Username != "admin" {
		t.Fatal("incorrect administrator username")
	}

	for _, forbidden := range []string{
		"password_hash",
		"password_salt",
		"password_scheme",
		"argon2id",
	} {
		if strings.Contains(stdout, forbidden) {
			t.Fatal("password material exposed in status")
		}
	}
}

func TestAdminInitializationCannotBeRepeated(
	t *testing.T,
) {
	dir, db := setupDeviceCommandDB(t)

	_, err := admin.NewRepository(db).CreateInitial(
		context.Background(),
		"admin",
		admin.PasswordDigest{
			Scheme: admin.PasswordScheme,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = executeAdminCLI(
		adminTestPrompt(),
		"init",
		"--username", "another-admin",
		"--confirm-offline",
		"--data-dir", dir,
	)

	if !errors.Is(
		err,
		admin.ErrAlreadyInitialized,
	) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAdminRecoveryRequiresExistingAccount(
	t *testing.T,
) {
	dir, _ := setupDeviceCommandDB(t)

	_, _, err := executeAdminCLI(
		adminTestPrompt(),
		"reset-password",
		"--username", "admin",
		"--confirm-offline",
		"--data-dir", dir,
	)

	if !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAdminPasswordConfirmationMismatch(
	t *testing.T,
) {
	dir, db := setupDeviceCommandDB(t)

	_, _, err := executeAdminCLI(
		adminTestPrompt(
			testInitialAdminPassword,
			testReplacementAdminPassword,
		),
		"init",
		"--username", "admin",
		"--confirm-offline",
		"--data-dir", dir,
	)

	if !errors.Is(err, errPasswordsDoNotMatch) {
		t.Fatalf("unexpected error: %v", err)
	}

	exists, err := admin.NewRepository(db).Exists(
		context.Background(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if exists {
		t.Fatal("password mismatch created an account")
	}
}

func TestAdminRejectsWeakPassword(t *testing.T) {
	dir, db := setupDeviceCommandDB(t)

	_, _, err := executeAdminCLI(
		adminTestPrompt(
			"passwordpassword",
			"passwordpassword",
		),
		"init",
		"--username", "admin",
		"--confirm-offline",
		"--data-dir", dir,
	)

	if !errors.Is(err, auth.ErrCommonPassword) {
		t.Fatalf("unexpected error: %v", err)
	}

	exists, err := admin.NewRepository(db).Exists(
		context.Background(),
	)
	if err != nil {
		t.Fatal(err)
	}

	if exists {
		t.Fatal("weak password created an account")
	}
}

func TestAdminCommandsRequireExistingDatabase(
	t *testing.T,
) {
	dir := t.TempDir()

	commands := [][]string{
		{"status", "--data-dir", dir},
		{
			"init",
			"--username", "admin",
			"--confirm-offline",
			"--data-dir", dir,
		},
		{
			"reset-password",
			"--username", "admin",
			"--confirm-offline",
			"--data-dir", dir,
		},
	}

	for _, args := range commands {
		_, _, err := executeAdminCLI(
			adminTestPrompt(),
			args...,
		)
		if err == nil {
			t.Fatal("missing database was accepted")
		}
	}

	if _, err := os.Stat(
		filepath.Join(dir, database.Filename),
	); !os.IsNotExist(err) {
		t.Fatal("administrator command created a database")
	}
}

func TestAdminInvalidArguments(t *testing.T) {
	commands := [][]string{
		{"unknown"},
		{"init"},
		{"reset-password"},
		{"init", "--username", "admin"},
		{"reset-password", "--username", "admin"},
		{"status", "--username", "admin"},
		{"status", "--data-dir", ""},
		{"status", "unexpected"},
		{"status", "--unknown"},
		{
			"init",
			"--username", "invalid username",
			"--confirm-offline",
		},
	}

	for _, args := range commands {
		t.Run(
			strings.Join(args, " "),
			func(t *testing.T) {
				_, _, err := executeAdminCLI(
					adminTestPrompt(),
					args...,
				)

				if err == nil {
					t.Fatal("invalid arguments accepted")
				}
			},
		)
	}
}

func TestAdminRejectsPasswordArgumentWithoutEcho(
	t *testing.T,
) {
	secret := "synthetic-secret-test-value"

	stdout, stderr, err := executeAdminCLI(
		adminTestPrompt(),
		"init",
		"--username", "admin",
		"--password="+secret,
		"--confirm-offline",
	)

	if err == nil {
		t.Fatal("password argument was accepted")
	}

	if strings.Contains(
		stdout+stderr+err.Error(),
		secret,
	) {
		t.Fatal("rejected password argument was echoed")
	}
}

func TestAdminDataDirectoryEnvironment(
	t *testing.T,
) {
	dir, _ := setupDeviceCommandDB(t)

	t.Setenv("RACKOS_DATA_DIR", dir)

	stdout, _, err := executeAdminCLI(
		adminTestPrompt(),
		"status",
	)
	if err != nil {
		t.Fatal(err)
	}

	var response adminStatusResponse

	if err := json.Unmarshal(
		[]byte(stdout),
		&response,
	); err != nil {
		t.Fatal(err)
	}

	if response.Initialized {
		t.Fatal("unexpected initialized administrator")
	}
}

func TestAdminPasswordPromptFailure(t *testing.T) {
	expected := errors.New(
		"simulated terminal read failure",
	)

	prompt := func(
		_ io.Writer,
		_ string,
	) ([]byte, error) {
		return nil, expected
	}

	_, err := readConfirmedAdminPassword(
		io.Discard,
		prompt,
	)

	if !errors.Is(err, expected) {
		t.Fatalf("unexpected error: %v", err)
	}
}
