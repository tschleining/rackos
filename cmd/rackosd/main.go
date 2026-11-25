package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tschleining/rackos/internal/appliance/config"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/httpserver"
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

	err = run(ctx, cfg, logger)
	stop()

	if err != nil {
		logger.Error("RackOS appliance failed", "error", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	cfg config.Config,
	logger *slog.Logger,
) error {
	logger.Info("Starting RackOS appliance",
		"version", version,
	)

	logger.Debug("Configuration loaded",
		"http_addr", cfg.HTTPAddr,
		"grpc_addr", cfg.GRPCAddr,
		"data_dir", cfg.DataDir,
		"log_level", cfg.LogLevel.String(),
	)

	logger.Info("Initializing database",
		"data_dir", cfg.DataDir,
	)

	db, err := database.Open(ctx, cfg.DataDir)
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}

	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("Failed to close database",
				"error", err,
			)
			return
		}

		logger.Info("Database closed")
	}()

	logger.Info("Database initialized")

	server := httpserver.New(
		cfg.HTTPAddr,
		version,
		db.PingContext,
	)

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on HTTP address: %w", err)
	}
	defer listener.Close()

	serverErr := make(chan error, 1)

	go func() {
		serverErr <- server.Serve(listener)
	}()

	logger.Info("HTTP server listening",
		"address", listener.Addr().String(),
	)

	logger.Info("RackOS appliance is running")

	select {
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server failed: %w", err)
		}
		return nil

	case <-ctx.Done():
		logger.Info("Shutting down RackOS appliance")
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("HTTP server shutdown failed: %w", err)
	}

	if err := <-serverErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("HTTP server failed: %w", err)
	}

	logger.Info("HTTP server stopped")
	logger.Info("HTTP shutdown complete")

	return nil
}
