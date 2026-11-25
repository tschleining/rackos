package config

import (
	"log/slog"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(func(string) (string, bool) {
		return "", false
	})

	if err != nil {
		t.Fatal(err)
	}

	if cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Errorf("unexpected HTTP address: %s", cfg.HTTPAddr)
	}

	if cfg.GRPCAddr != "127.0.0.1:50051" {
		t.Errorf("unexpected gRPC address: %s", cfg.GRPCAddr)
	}

	if cfg.DataDir != "./data" {
		t.Errorf("unexpected data directory: %s", cfg.DataDir)
	}

	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("unexpected log level: %s", cfg.LogLevel)
	}
}

func TestLoadOverrides(t *testing.T) {
	env := map[string]string{
		"RACKOS_HTTP_ADDR": "127.0.0.1:9090",
		"RACKOS_GRPC_ADDR": "127.0.0.1:50052",
		"RACKOS_DATA_DIR":  "/tmp/rackos",
		"RACKOS_LOG_LEVEL": "debug",
	}

	cfg, err := load(func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	})

	if err != nil {
		t.Fatal(err)
	}

	if cfg.HTTPAddr != env["RACKOS_HTTP_ADDR"] ||
		cfg.GRPCAddr != env["RACKOS_GRPC_ADDR"] ||
		cfg.DataDir != env["RACKOS_DATA_DIR"] ||
		cfg.LogLevel != slog.LevelDebug {
		t.Errorf("unexpected configuration: %+v", cfg)
	}
}

func TestLoadInvalidLogLevel(t *testing.T) {
	_, err := load(func(key string) (string, bool) {
		if key == "RACKOS_LOG_LEVEL" {
			return "invalid", true
		}
		return "", false
	})

	if err == nil {
		t.Fatal("expected invalid log level error")
	}
}

func TestLoadEmptyAddress(t *testing.T) {
	_, err := load(func(key string) (string, bool) {
		if key == "RACKOS_HTTP_ADDR" {
			return "", true
		}
		return "", false
	})

	if err == nil {
		t.Fatal("expected empty address error")
	}
}
