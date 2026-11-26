package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"testing"

	"github.com/tschleining/rackos/internal/agent/inventory"
)

func executeInventoryCLI(
	ctx context.Context,
	args ...string,
) (string, error) {
	var stdout bytes.Buffer

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	err := run(
		ctx,
		args,
		strings.NewReader(""),
		&stdout,
		logger,
	)

	return stdout.String(), err
}

func TestInventoryCommand(t *testing.T) {
	output, err := executeInventoryCLI(
		context.Background(),
		"inventory",
	)
	if err != nil {
		t.Fatal(err)
	}

	var result inventory.Inventory

	if err := json.Unmarshal(
		[]byte(output),
		&result,
	); err != nil {
		t.Fatal(err)
	}

	if result.Hostname == "" {
		t.Fatal("missing hostname")
	}

	if result.OS != runtime.GOOS {
		t.Fatal("incorrect operating system")
	}

	if result.Architecture != runtime.GOARCH {
		t.Fatal("incorrect architecture")
	}

	if result.LogicalCPUs < 1 {
		t.Fatal("invalid CPU count")
	}

	if result.CollectedAt.IsZero() {
		t.Fatal("missing collection timestamp")
	}

	switch runtime.GOOS {
	case "darwin", "linux":
		if result.MemoryBytes == 0 {
			t.Fatal("missing physical memory")
		}

		if result.HostLogicalCPUs < 1 {
			t.Fatal("missing host CPU count")
		}

		if result.BootTime == nil {
			t.Fatal("missing boot time")
		}
	}
}

func TestInventoryCommandHelp(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		t.Run(flag, func(t *testing.T) {
			output, err := executeInventoryCLI(
				context.Background(),
				"inventory",
				flag,
			)
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(
				output,
				"Usage: rackos-agent inventory",
			) {
				t.Fatal("missing inventory help")
			}
		})
	}
}

func TestInventoryCommandInvalidArguments(t *testing.T) {
	tests := [][]string{
		{"inventory", "unexpected"},
		{"inventory", "--unknown"},
		{"inventory", "--state-dir", "./data/agent"},
		{"inventory", "--help", "unexpected"},
	}

	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := executeInventoryCLI(
				context.Background(),
				args...,
			)

			if err == nil {
				t.Fatal("unexpected arguments accepted")
			}
		})
	}
}

func TestInventoryCommandCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cancel()

	output, err := executeInventoryCLI(
		ctx,
		"inventory",
	)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf(
			"expected context.Canceled, got %v",
			err,
		)
	}

	if output != "" {
		t.Fatal("unexpected output after cancellation")
	}
}

func TestAgentCommandDispatch(t *testing.T) {
	commands := [][]string{
		{"help"},
		{"version"},
		{"inventory", "--help"},
		{"enroll", "--help"},
		{"run", "--help"},
	}

	for _, args := range commands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			_, err := executeInventoryCLI(
				context.Background(),
				args...,
			)

			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
