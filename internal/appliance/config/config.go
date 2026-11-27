package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tschleining/rackos/internal/appliance/retention"
)

type Config struct {
	HTTPAddr      string
	GRPCAddr      string
	DataDir       string
	LogLevel      slog.Level
	ManagementTLS bool

	PerformanceRetention retention.Policy
}

func Default() Config {
	return Config{
		HTTPAddr:             "127.0.0.1:8080",
		GRPCAddr:             "127.0.0.1:50051",
		DataDir:              "./data",
		LogLevel:             slog.LevelInfo,
		ManagementTLS:        false,
		PerformanceRetention: retention.DefaultPolicy(),
	}
}

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup func(string) (string, bool)) (Config, error) {
	cfg := Default()

	overrides := []struct {
		key    string
		target *string
	}{
		{"RACKOS_HTTP_ADDR", &cfg.HTTPAddr},
		{"RACKOS_GRPC_ADDR", &cfg.GRPCAddr},
		{"RACKOS_DATA_DIR", &cfg.DataDir},
	}

	for _, override := range overrides {
		if value, ok := lookup(override.key); ok {
			if strings.TrimSpace(value) == "" {
				return Config{}, fmt.Errorf(
					"%s cannot be empty", override.key,
				)
			}

			*override.target = value
		}
	}

	if value, ok := lookup("RACKOS_LOG_LEVEL"); ok {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "debug":
			cfg.LogLevel = slog.LevelDebug
		case "info":
			cfg.LogLevel = slog.LevelInfo
		case "warn":
			cfg.LogLevel = slog.LevelWarn
		case "error":
			cfg.LogLevel = slog.LevelError
		default:
			return Config{}, fmt.Errorf(
				"invalid RACKOS_LOG_LEVEL: %q", value,
			)
		}
	}
	if value, ok := lookup("RACKOS_MANAGEMENT_TLS"); ok {
		switch value {
		case "true":
			cfg.ManagementTLS = true
		case "false":
			cfg.ManagementTLS = false
		default:
			return Config{}, fmt.Errorf(
				"RACKOS_MANAGEMENT_TLS must be true or false",
			)
		}
	}

	policy, err := loadRetentionPolicy(lookup)
	if err != nil {
		return Config{}, err
	}

	cfg.PerformanceRetention = policy

	return cfg, nil
}

// loadRetentionPolicy applies environment overrides
// to the default performance retention policy.
//
// Invalid settings prevent appliance startup.
// Automatic deletion remains disabled unless an
// explicit nonzero retention duration is configured.
func loadRetentionPolicy(
	lookup func(string) (string, bool),
) (retention.Policy, error) {
	policy := retention.DefaultPolicy()

	durationOverrides := []struct {
		key    string
		target *time.Duration
	}{
		{
			"RACKOS_PERFORMANCE_RETENTION",
			&policy.RetentionDuration,
		},
		{
			"RACKOS_PERFORMANCE_CLEANUP_INTERVAL",
			&policy.CleanupInterval,
		},
	}

	for _, override := range durationOverrides {
		value, exists := lookup(override.key)
		if !exists {
			continue
		}

		duration, err := time.ParseDuration(value)
		if err != nil {
			return retention.Policy{}, fmt.Errorf(
				"invalid %s: %w",
				override.key,
				err,
			)
		}

		*override.target = duration

		if err := policy.Validate(); err != nil {
			return retention.Policy{}, fmt.Errorf(
				"invalid %s: %w",
				override.key,
				err,
			)
		}
	}

	const batchSizeKey = "RACKOS_PERFORMANCE_CLEANUP_BATCH_SIZE"

	if value, exists := lookup(batchSizeKey); exists {
		batchSize, err := parseRetentionBatchSize(value)
		if err != nil {
			return retention.Policy{}, fmt.Errorf(
				"invalid %s: %w",
				batchSizeKey,
				err,
			)
		}

		policy.BatchSize = batchSize

		if err := policy.Validate(); err != nil {
			return retention.Policy{}, fmt.Errorf(
				"invalid %s: %w",
				batchSizeKey,
				err,
			)
		}
	}

	if err := policy.Validate(); err != nil {
		return retention.Policy{}, err
	}

	return policy, nil
}

// parseRetentionBatchSize accepts only decimal
// integer values without signs or whitespace.
//
// Range validation is performed by the retention
// policy rather than duplicated in configuration.
func parseRetentionBatchSize(value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("batch size cannot be empty")
	}

	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, fmt.Errorf(
				"batch size must be a decimal integer",
			)
		}
	}

	return strconv.Atoi(value)
}
