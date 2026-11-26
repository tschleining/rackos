package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tschleining/rackos/internal/appliance/database"
)

func TestHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if err := run(
		[]string{"help"},
		&stdout,
		&stderr,
	); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(stdout.String(), "RackOS") {
		t.Fatal("missing CLI help output")
	}
}

func TestCreateEnrollmentToken(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer

	err = run(
		[]string{
			"enrollment", "create",
			"--data-dir", dir,
			"--ttl", "15m",
		},
		&stdout,
		&stderr,
	)
	if err != nil {
		t.Fatal(err)
	}

	token := strings.TrimSpace(stdout.String())

	if !strings.HasPrefix(token, "rackos_enroll_") {
		t.Fatal("unexpected token format")
	}

	if strings.Contains(stderr.String(), token) {
		t.Fatal("secret leaked into informational output")
	}
}

func TestMissingDatabase(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer

	err := run(
		[]string{
			"enrollment", "create",
			"--data-dir", dir,
		},
		&stdout,
		&stderr,
	)

	if err == nil {
		t.Fatal("expected missing database error")
	}

	path := filepath.Join(dir, database.Filename)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("CLI unexpectedly created a database")
	}
}
