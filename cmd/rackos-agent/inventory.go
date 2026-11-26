package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/tschleining/rackos/internal/agent/inventory"
)

const inventoryCommandTimeout = 10 * time.Second

// runInventory collects and prints a local system
// inventory snapshot without contacting the appliance.
func runInventory(
	ctx context.Context,
	stdout io.Writer,
) error {
	ctx, cancel := context.WithTimeout(
		ctx,
		inventoryCommandTimeout,
	)
	defer cancel()

	result, err := inventory.Collect(ctx)
	if err != nil {
		return fmt.Errorf(
			"collect system inventory: %w",
			err,
		)
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf(
			"encode system inventory: %w",
			err,
		)
	}

	return nil
}
