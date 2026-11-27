package config

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/retention"
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

	if cfg.PerformanceRetention != retention.DefaultPolicy() {
		t.Fatal("incorrect default performance retention policy")
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

	if cfg.PerformanceRetention.Enabled() {
		t.Fatal("existing configuration enabled retention")
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

func loadTestEnvironment(
	env map[string]string,
) (Config, error) {
	return load(func(key string) (string, bool) {
		value, exists := env[key]

		return value, exists
	})
}

func TestRetentionConfigDefaults(t *testing.T) {
	cfg := Default()

	policy := cfg.PerformanceRetention

	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}

	if policy.Enabled() {
		t.Fatal("automatic retention must be disabled by default")
	}

	if policy != retention.DefaultPolicy() {
		t.Fatal("configuration did not preserve policy defaults")
	}
}

func TestRetentionConfigOverrides(t *testing.T) {
	env := map[string]string{
		"RACKOS_PERFORMANCE_RETENTION":          "720h",
		"RACKOS_PERFORMANCE_CLEANUP_INTERVAL":   "5m",
		"RACKOS_PERFORMANCE_CLEANUP_BATCH_SIZE": "250",
	}

	cfg, err := loadTestEnvironment(env)
	if err != nil {
		t.Fatal(err)
	}

	policy := cfg.PerformanceRetention

	if !policy.Enabled() {
		t.Fatal("configured retention was not enabled")
	}

	expected := retention.DefaultPolicy()

	expected.RetentionDuration = 720 * time.Hour
	expected.CleanupInterval = 5 * time.Minute
	expected.BatchSize = 250

	if policy != expected {
		t.Fatalf(
			"expected policy %+v, got %+v",
			expected,
			policy,
		)
	}

	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRetentionConfigPartialOverrides(t *testing.T) {
	tests := []struct {
		name   string
		env    map[string]string
		change func(*retention.Policy)
	}{
		{
			name: "retention duration only",
			env: map[string]string{
				"RACKOS_PERFORMANCE_RETENTION": "48h",
			},
			change: func(p *retention.Policy) {
				p.RetentionDuration = 48 * time.Hour
			},
		},
		{
			name: "cleanup interval only",
			env: map[string]string{
				"RACKOS_PERFORMANCE_CLEANUP_INTERVAL": "10m",
			},
			change: func(p *retention.Policy) {
				p.CleanupInterval = 10 * time.Minute
			},
		},
		{
			name: "batch size only",
			env: map[string]string{
				"RACKOS_PERFORMANCE_CLEANUP_BATCH_SIZE": "100",
			},
			change: func(p *retention.Policy) {
				p.BatchSize = 100
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadTestEnvironment(tt.env)
			if err != nil {
				t.Fatal(err)
			}

			expected := retention.DefaultPolicy()

			tt.change(&expected)

			if cfg.PerformanceRetention != expected {
				t.Fatalf(
					"expected policy %+v, got %+v",
					expected,
					cfg.PerformanceRetention,
				)
			}
		})
	}
}

func TestRetentionConfigExplicitlyDisabled(t *testing.T) {
	for _, value := range []string{
		"0",
		"0s",
		"0h",
	} {
		t.Run(value, func(t *testing.T) {
			cfg, err := loadTestEnvironment(
				map[string]string{
					"RACKOS_PERFORMANCE_RETENTION":          value,
					"RACKOS_PERFORMANCE_CLEANUP_INTERVAL":   "5m",
					"RACKOS_PERFORMANCE_CLEANUP_BATCH_SIZE": "100",
				},
			)
			if err != nil {
				t.Fatal(err)
			}

			if cfg.PerformanceRetention.Enabled() {
				t.Fatal("zero duration enabled retention")
			}

			if cfg.PerformanceRetention.CleanupInterval !=
				5*time.Minute {
				t.Fatal("cleanup interval was not preserved")
			}

			if cfg.PerformanceRetention.BatchSize != 100 {
				t.Fatal("batch size was not preserved")
			}
		})
	}
}

func TestRetentionConfigValidBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		duration time.Duration
		interval time.Duration
		batch    int
	}{
		{
			name: "minimum enabled settings",
			env: map[string]string{
				"RACKOS_PERFORMANCE_RETENTION":          "24h",
				"RACKOS_PERFORMANCE_CLEANUP_INTERVAL":   "1m",
				"RACKOS_PERFORMANCE_CLEANUP_BATCH_SIZE": "1",
			},
			duration: retention.MinRetentionDuration,
			interval: retention.MinCleanupInterval,
			batch:    retention.MinBatchSize,
		},
		{
			name: "maximum enabled settings",
			env: map[string]string{
				"RACKOS_PERFORMANCE_RETENTION":          "8760h",
				"RACKOS_PERFORMANCE_CLEANUP_INTERVAL":   "24h",
				"RACKOS_PERFORMANCE_CLEANUP_BATCH_SIZE": "1000",
			},
			duration: retention.MaxRetentionDuration,
			interval: retention.MaxCleanupInterval,
			batch:    retention.MaxBatchSize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadTestEnvironment(tt.env)
			if err != nil {
				t.Fatal(err)
			}

			policy := cfg.PerformanceRetention

			if policy.RetentionDuration != tt.duration ||
				policy.CleanupInterval != tt.interval ||
				policy.BatchSize != tt.batch {
				t.Fatal("incorrect boundary configuration")
			}

			if err := policy.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRetentionConfigInvalidDuration(t *testing.T) {
	const key = "RACKOS_PERFORMANCE_RETENTION"

	for _, value := range []string{
		"",
		" ",
		"invalid",
		"30d",
		"720",
		"-24h",
		"1ns",
		"23h59m59s",
		"8761h",
		"720h ",
		" 720h",
		"999999999999999999999h",
	} {
		t.Run(value, func(t *testing.T) {
			cfg, err := loadTestEnvironment(
				map[string]string{
					key: value,
				},
			)

			if err == nil {
				t.Fatal("invalid retention duration accepted")
			}

			if !strings.Contains(err.Error(), key) {
				t.Fatalf("missing configuration key: %v", err)
			}

			if cfg != (Config{}) {
				t.Fatal("invalid configuration returned partial state")
			}
		})
	}
}

func TestRetentionConfigInvalidCleanupInterval(t *testing.T) {
	const key = "RACKOS_PERFORMANCE_CLEANUP_INTERVAL"

	for _, value := range []string{
		"",
		" ",
		"invalid",
		"15",
		"0s",
		"-1m",
		"59s",
		"24h1s",
		"15m ",
		" 15m",
		"999999999999999999999h",
	} {
		t.Run(value, func(t *testing.T) {
			cfg, err := loadTestEnvironment(
				map[string]string{
					key: value,
				},
			)

			if err == nil {
				t.Fatal("invalid cleanup interval accepted")
			}

			if !strings.Contains(err.Error(), key) {
				t.Fatalf("missing configuration key: %v", err)
			}

			if cfg != (Config{}) {
				t.Fatal("invalid configuration returned partial state")
			}
		})
	}
}

func TestRetentionConfigInvalidBatchSize(t *testing.T) {
	const key = "RACKOS_PERFORMANCE_CLEANUP_BATCH_SIZE"

	for _, value := range []string{
		"",
		" ",
		"invalid",
		"0",
		"-1",
		"+500",
		"1001",
		"1.5",
		"5e2",
		"0x100",
		"500 ",
		" 500",
		"999999999999999999999",
	} {
		t.Run(value, func(t *testing.T) {
			cfg, err := loadTestEnvironment(
				map[string]string{
					key: value,
				},
			)

			if err == nil {
				t.Fatal("invalid batch size accepted")
			}

			if !strings.Contains(err.Error(), key) {
				t.Fatalf("missing configuration key: %v", err)
			}

			if cfg != (Config{}) {
				t.Fatal("invalid configuration returned partial state")
			}
		})
	}
}

func TestRetentionConfigPolicyErrors(t *testing.T) {
	tests := []struct {
		key   string
		value string
	}{
		{
			"RACKOS_PERFORMANCE_RETENTION",
			"12h",
		},
		{
			"RACKOS_PERFORMANCE_CLEANUP_INTERVAL",
			"30s",
		},
		{
			"RACKOS_PERFORMANCE_CLEANUP_BATCH_SIZE",
			"1001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			_, err := loadTestEnvironment(
				map[string]string{
					tt.key: tt.value,
				},
			)

			if !errors.Is(err, retention.ErrInvalidPolicy) {
				t.Fatalf(
					"expected retention policy error, got %v",
					err,
				)
			}
		})
	}
}

func TestRetentionConfigDisabledStillValidates(t *testing.T) {
	tests := []map[string]string{
		{
			"RACKOS_PERFORMANCE_RETENTION":        "0s",
			"RACKOS_PERFORMANCE_CLEANUP_INTERVAL": "0s",
		},
		{
			"RACKOS_PERFORMANCE_RETENTION":          "0s",
			"RACKOS_PERFORMANCE_CLEANUP_BATCH_SIZE": "0",
		},
	}

	for _, env := range tests {
		_, err := loadTestEnvironment(env)

		if !errors.Is(err, retention.ErrInvalidPolicy) {
			t.Fatalf(
				"disabled retention accepted invalid settings: %v",
				err,
			)
		}
	}
}

func TestRetentionConfigExistingOverrides(t *testing.T) {
	cfg, err := loadTestEnvironment(
		map[string]string{
			"RACKOS_HTTP_ADDR":                      "127.0.0.1:18080",
			"RACKOS_GRPC_ADDR":                      "127.0.0.1:15051",
			"RACKOS_DATA_DIR":                       "/tmp/rackos-retention",
			"RACKOS_LOG_LEVEL":                      "debug",
			"RACKOS_PERFORMANCE_RETENTION":          "720h",
			"RACKOS_PERFORMANCE_CLEANUP_INTERVAL":   "5m",
			"RACKOS_PERFORMANCE_CLEANUP_BATCH_SIZE": "250",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.HTTPAddr != "127.0.0.1:18080" ||
		cfg.GRPCAddr != "127.0.0.1:15051" ||
		cfg.DataDir != "/tmp/rackos-retention" ||
		cfg.LogLevel != slog.LevelDebug {
		t.Fatal("retention changed existing configuration behavior")
	}

	if !cfg.PerformanceRetention.Enabled() {
		t.Fatal("retention was not enabled")
	}
}

func TestRetentionConfigIndependentDefaults(t *testing.T) {
	first := Default()

	first.PerformanceRetention.RetentionDuration =
		retention.MaxRetentionDuration

	first.PerformanceRetention.BatchSize =
		retention.MaxBatchSize

	second := Default()

	if second.PerformanceRetention != retention.DefaultPolicy() {
		t.Fatal("modifying configuration changed future defaults")
	}
}
