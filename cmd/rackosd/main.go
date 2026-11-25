package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/tschleining/rackos/internal/appliance/config"
)

const version = "0.1.0-dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(
		os.Stdout,
		&slog.HandlerOptions{
			Level: cfg.LogLevel,
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

	logger.Debug("Configuration loaded",
		"http_addr", cfg.HTTPAddr,
		"grpc_addr", cfg.GRPCAddr,
		"data_dir", cfg.DataDir,
		"log_level", cfg.LogLevel.String(),
	)

	logger.Info("RackOS appliance is running")

	<-ctx.Done()

	logger.Info("Shutting down RackOS appliance")
	logger.Info("Shutdown complete")
}
