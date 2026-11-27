package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tschleining/rackos/internal/appliance/agentservice"
	"github.com/tschleining/rackos/internal/appliance/config"
	"github.com/tschleining/rackos/internal/appliance/database"
	"github.com/tschleining/rackos/internal/appliance/devices"
	"github.com/tschleining/rackos/internal/appliance/grpcserver"
	"github.com/tschleining/rackos/internal/appliance/httpserver"
	"github.com/tschleining/rackos/internal/appliance/identity"
	"github.com/tschleining/rackos/internal/appliance/inventory"
	"github.com/tschleining/rackos/internal/appliance/performance"
	"github.com/tschleining/rackos/internal/appliance/pki"
	"github.com/tschleining/rackos/internal/appliance/retention"

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
	if err := cfg.PerformanceRetention.Validate(); err != nil {
		return fmt.Errorf(
			"validate performance retention configuration: %w",
			err,
		)
	}

	logger.Info("Starting RackOS appliance",
		"version", version,
	)

	logger.Debug("Configuration loaded",
		"http_addr", cfg.HTTPAddr,
		"grpc_addr", cfg.GRPCAddr,
		"data_dir", cfg.DataDir,
		"log_level", cfg.LogLevel.String(),
		"performance_retention_enabled",
		cfg.PerformanceRetention.Enabled(),
		"performance_retention",
		cfg.PerformanceRetention.RetentionDuration.String(),
		"performance_cleanup_interval",
		cfg.PerformanceRetention.CleanupInterval.String(),
		"performance_cleanup_batch_size",
		cfg.PerformanceRetention.BatchSize,
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

	logger.Info("Initializing appliance PKI")

	bindIP, err := grpcserver.BindIP(cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf(
			"validate gRPC address: %w", err,
		)
	}

	tlsNames := []string{bindIP.String()}

	if value, ok := os.LookupEnv(
		"RACKOS_GRPC_TLS_NAMES",
	); ok {
		tlsNames = append(
			tlsNames,
			strings.Split(value, ",")...,
		)
	}

	tlsConfig, err := pki.LoadOrCreate(
		cfg.DataDir,
		installationID,
		tlsNames...,
	)
	if err != nil {
		return fmt.Errorf(
			"initialize appliance PKI: %w", err,
		)
	}

	logger.Info("Appliance PKI initialized",
		"minimum_tls_version", "1.3",
		"automatic_certificate_renewal", true,
	)

	logger.Info(
		"gRPC certificate identities configured",
		"names", tlsNames,
	)

	if !bindIP.IsLoopback() {
		logger.Warn(
			"Remote agent connections enabled",
			"address", cfg.GRPCAddr,
		)
	}

	deviceRepository := devices.NewRepository(db)

	performanceRepository := performance.NewRepository(db)

	inventoryRepository := inventory.NewRepository(db)

	retentionWorker, err := retention.NewWorker(
		cfg.PerformanceRetention,
		performanceRepository,
		logger,
	)
	if err != nil {
		return fmt.Errorf(
			"initialize performance retention worker: %w",
			err,
		)
	}

	httpServer := httpserver.NewWithManagementInventory(
		cfg.HTTPAddr,
		version,
		db.PingContext,
		deviceRepository,
		performanceRepository,
		inventoryRepository,
	)

	consoleEnabled, err :=
		httpserver.EnableWebConsole(httpServer)
	if err != nil {
		return fmt.Errorf(
			"initialize web console: %w",
			err,
		)
	}

	logger.Info(
		"Web console configuration",
		"embedded", consoleEnabled,
	)

	logger.Info("Device management API initialized")

	logger.Info("Performance management API initialized")

	logger.Info("Inventory management API initialized")

	agentService := agentservice.New(
		db,
		installationID,
	)

	grpcServer := grpcserver.New(
		tlsConfig,
		agentService,
	)

	logger.Info("Agent service initialized",
		"heartbeat_interval_seconds",
		agentservice.HeartbeatIntervalSeconds,
	)

	httpListener, err := httpserver.Listen(cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on HTTP address: %w", err)
	}
	defer httpListener.Close()

	grpcListener, err := grpcserver.Listen(cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen on gRPC address: %w", err)
	}
	defer grpcListener.Close()

	// Start retention only after both network
	// listeners have been initialized successfully.
	retentionCtx, cancelRetention := context.WithCancel(ctx)

	retentionDone := make(chan struct{})

	// Always join the worker before the deferred
	// database closure, including unexpected exits.
	defer func() {
		cancelRetention()
		<-retentionDone
	}()

	if cfg.PerformanceRetention.Enabled() {
		go func() {
			defer close(retentionDone)

			if err := retentionWorker.Run(
				retentionCtx,
			); err != nil {
				logger.Error(
					"Performance retention worker exited unexpectedly",
					"error", err,
				)
			}
		}()
	} else {
		close(retentionDone)

		logger.Info(
			"Performance retention disabled",
		)
	}

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
		"tls", true,
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

	// Cancel retention even when shutdown was
	// caused by a network server failure rather
	// than cancellation of the parent context.
	cancelRetention()

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

	// Wait for retention to finish before allowing
	// run to return and close the SQLite database.
	<-retentionDone

	if runErr != nil {
		return runErr
	}

	logger.Info("Network services stopped")

	return nil
}
