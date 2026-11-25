package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	HTTPAddr string
	GRPCAddr string
	DataDir  string
	LogLevel slog.Level
}

func Default() Config {
	return Config{
		HTTPAddr: "127.0.0.1:8080",
		GRPCAddr: "127.0.0.1:50051",
		DataDir:  "./data",
		LogLevel: slog.LevelInfo,
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

	return cfg, nil
}
