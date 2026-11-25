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
	"github.com/tschleining/rackos/internal/appliance/grpcserver"
	"github.com/tschleining/rackos/internal/appliance/httpserver"
	"github.com/tschleining/rackos/internal/appliance/identity"
	"google.golang.org/grpc"
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

	installationID, err := identity.LoadOrCreate(ctx, db)
	if err != nil {
		return fmt.Errorf("initialize appliance identity: %w", err)
	}

	logger.Info("Appliance identity initialized",
		"installation_id", installationID,
	)

	httpServer := httpserver.New(
		cfg.HTTPAddr,
		version,
		db.PingContext,
	)

	grpcServer := grpcserver.New()

	httpListener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on HTTP address: %w", err)
	}
	defer httpListener.Close()

	grpcListener, err := grpcserver.Listen(cfg.GRPCAddr)
	if err != nil {
		return err
	}
	defer grpcListener.Close()

	type serverResult struct {
		name string
		err  error
	}

	serverResults := make(chan serverResult, 2)

	go func() {
		serverResults <- serverResult{
			name: "HTTP",
			err:  httpServer.Serve(httpListener),
		}
	}()

	go func() {
		serverResults <- serverResult{
			name: "gRPC",
			err:  grpcServer.Serve(grpcListener),
		}
	}()

	logger.Info("HTTP server listening",
		"address", httpListener.Addr().String(),
	)

	logger.Info("gRPC server listening",
		"address", grpcListener.Addr().String(),
		"tls", false,
	)

	logger.Info("RackOS appliance is running")

	var runErr error
	remaining := 2

	select {
	case result := <-serverResults:
		remaining--

		if result.err != nil {
			runErr = fmt.Errorf(
				"%s server stopped unexpectedly: %w",
				result.name,
				result.err,
			)
		} else {
			runErr = fmt.Errorf(
				"%s server stopped unexpectedly",
				result.name,
			)
		}

	case <-ctx.Done():
	}

	logger.Info("Shutting down RackOS appliance")

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	grpcShutdown := make(chan error, 1)

	go func() {
		grpcShutdown <- grpcServer.Shutdown(shutdownCtx)
	}()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		_ = httpServer.Close()

		runErr = errors.Join(
			runErr,
			fmt.Errorf("HTTP shutdown failed: %w", err),
		)
	} else {
		logger.Info("HTTP server stopped")
	}

	if err := <-grpcShutdown; err != nil {
		runErr = errors.Join(runErr, err)
	} else {
		logger.Info("gRPC server stopped")
	}

	for i := 0; i < remaining; i++ {
		result := <-serverResults

		if result.err == nil ||
			errors.Is(result.err, http.ErrServerClosed) ||
			errors.Is(result.err, grpc.ErrServerStopped) {
			continue
		}

		runErr = errors.Join(
			runErr,
			fmt.Errorf(
				"%s server failed: %w",
				result.name,
				result.err,
			),
		)
	}

	if runErr != nil {
		return runErr
	}

	logger.Info("Network services stopped")

	return nil
}
