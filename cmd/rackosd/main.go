package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

const version = "0.1.0-dev"

func main() {
	logger := slog.New(slog.NewTextHandler(
		os.Stdout,
		&slog.HandlerOptions{
			Level: slog.LevelInfo,
		},
	))

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	logger.Info("Starting RackOS appliance",
		"version", version,
	)

	logger.Info("RackOS appliance is running")

	<-ctx.Done()

	logger.Info("Shutting down RackOS appliance")
	logger.Info("Shutdown complete")
}
