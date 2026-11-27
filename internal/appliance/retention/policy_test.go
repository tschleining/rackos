package retention

import (
	"errors"
	"testing"
	"time"
)

func TestDefaultPolicy(t *testing.T) {
	policy := DefaultPolicy()

	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}

	if policy.Enabled() {
		t.Fatal("automatic retention must be disabled by default")
	}

	if policy.RetentionDuration != 0 {
		t.Fatal("incorrect default retention duration")
	}

	if policy.CleanupInterval != 15*time.Minute {
		t.Fatal("incorrect default cleanup interval")
	}

	if policy.BatchSize != 500 {
		t.Fatal("incorrect default batch size")
	}

	if policy.MaxBatchesPerCycle != 20 {
		t.Fatal("incorrect default maximum batches")
	}

	if policy.BatchDelay != 100*time.Millisecond {
		t.Fatal("incorrect default batch delay")
	}

	if policy.BatchTimeout != 5*time.Second {
		t.Fatal("incorrect default batch timeout")
	}
}

func TestPolicyEnabled(t *testing.T) {
	policy := DefaultPolicy()

	if policy.Enabled() {
		t.Fatal("default policy unexpectedly enabled")
	}

	policy.RetentionDuration = RecommendedRetentionDuration

	if !policy.Enabled() {
		t.Fatal("configured retention is not enabled")
	}

	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}

	policy.RetentionDuration = 0

	if policy.Enabled() {
		t.Fatal("zero retention did not disable policy")
	}
}

func TestPolicyValidBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Policy)
	}{
		{
			name: "disabled",
			change: func(p *Policy) {
				p.RetentionDuration = 0
			},
		},
		{
			name: "recommended retention",
			change: func(p *Policy) {
				p.RetentionDuration =
					RecommendedRetentionDuration
			},
		},
		{
			name: "minimum settings",
			change: func(p *Policy) {
				p.RetentionDuration = MinRetentionDuration
				p.CleanupInterval = MinCleanupInterval
				p.BatchSize = MinBatchSize
				p.MaxBatchesPerCycle = MinBatchesPerCycle
				p.BatchDelay = MinBatchDelay
				p.BatchTimeout = MinBatchTimeout
			},
		},
		{
			name: "maximum settings",
			change: func(p *Policy) {
				p.RetentionDuration = MaxRetentionDuration
				p.CleanupInterval = MaxCleanupInterval
				p.BatchSize = MaxBatchSize
				p.MaxBatchesPerCycle = MaxBatchesPerCycle
				p.BatchDelay = MaxBatchDelay
				p.BatchTimeout = MaxBatchTimeout
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := DefaultPolicy()

			tt.change(&policy)

			if err := policy.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPolicyInvalidBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Policy)
	}{
		{
			name: "negative retention",
			change: func(p *Policy) {
				p.RetentionDuration = -time.Hour
			},
		},
		{
			name: "retention below minimum",
			change: func(p *Policy) {
				p.RetentionDuration =
					MinRetentionDuration - time.Nanosecond
			},
		},
		{
			name: "retention above maximum",
			change: func(p *Policy) {
				p.RetentionDuration =
					MaxRetentionDuration + time.Nanosecond
			},
		},
		{
			name: "zero cleanup interval",
			change: func(p *Policy) {
				p.CleanupInterval = 0
			},
		},
		{
			name: "cleanup interval below minimum",
			change: func(p *Policy) {
				p.CleanupInterval =
					MinCleanupInterval - time.Nanosecond
			},
		},
		{
			name: "cleanup interval above maximum",
			change: func(p *Policy) {
				p.CleanupInterval =
					MaxCleanupInterval + time.Nanosecond
			},
		},
		{
			name: "zero batch size",
			change: func(p *Policy) {
				p.BatchSize = 0
			},
		},
		{
			name: "negative batch size",
			change: func(p *Policy) {
				p.BatchSize = -1
			},
		},
		{
			name: "batch size above maximum",
			change: func(p *Policy) {
				p.BatchSize = MaxBatchSize + 1
			},
		},
		{
			name: "zero maximum batches",
			change: func(p *Policy) {
				p.MaxBatchesPerCycle = 0
			},
		},
		{
			name: "maximum batches above limit",
			change: func(p *Policy) {
				p.MaxBatchesPerCycle =
					MaxBatchesPerCycle + 1
			},
		},
		{
			name: "batch delay below minimum",
			change: func(p *Policy) {
				p.BatchDelay =
					MinBatchDelay - time.Nanosecond
			},
		},
		{
			name: "batch delay above maximum",
			change: func(p *Policy) {
				p.BatchDelay =
					MaxBatchDelay + time.Nanosecond
			},
		},
		{
			name: "batch timeout below minimum",
			change: func(p *Policy) {
				p.BatchTimeout =
					MinBatchTimeout - time.Nanosecond
			},
		},
		{
			name: "batch timeout above maximum",
			change: func(p *Policy) {
				p.BatchTimeout =
					MaxBatchTimeout + time.Nanosecond
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := DefaultPolicy()

			tt.change(&policy)

			err := policy.Validate()

			if !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf(
					"expected invalid policy, got %v",
					err,
				)
			}
		})
	}
}

func TestPolicyZeroValue(t *testing.T) {
	var policy Policy

	if policy.Enabled() {
		t.Fatal("zero-value policy is enabled")
	}

	if err := policy.Validate(); !errors.Is(
		err,
		ErrInvalidPolicy,
	) {
		t.Fatalf(
			"expected invalid zero-value policy, got %v",
			err,
		)
	}
}

func TestPolicyDisabledValidation(t *testing.T) {
	policy := DefaultPolicy()

	policy.BatchSize = 0

	if policy.Enabled() {
		t.Fatal("retention unexpectedly enabled")
	}

	if err := policy.Validate(); !errors.Is(
		err,
		ErrInvalidPolicy,
	) {
		t.Fatalf(
			"disabled policy accepted invalid settings: %v",
			err,
		)
	}
}

func TestPolicyCutoff(t *testing.T) {
	policy := DefaultPolicy()

	policy.RetentionDuration = 30 * 24 * time.Hour

	now := time.Date(
		2026, 11, 27,
		12, 0, 0, 123456789,
		time.UTC,
	)

	expected := time.Date(
		2026, 10, 28,
		12, 0, 0, 123000000,
		time.UTC,
	)

	actual, err := policy.Cutoff(now)
	if err != nil {
		t.Fatal(err)
	}

	if !actual.Equal(expected) {
		t.Fatalf(
			"expected cutoff %s, got %s",
			expected,
			actual,
		)
	}

	if actual.Location() != time.UTC {
		t.Fatal("cutoff is not UTC")
	}

	if actual.Nanosecond()%int(time.Millisecond) != 0 {
		t.Fatal("cutoff is not millisecond aligned")
	}
}

func TestPolicyCutoffTimezoneNormalization(t *testing.T) {
	policy := DefaultPolicy()

	policy.RetentionDuration = MinRetentionDuration

	location := time.FixedZone(
		"test-offset",
		-5*60*60,
	)

	now := time.Date(
		2026, 11, 27,
		7, 0, 0, 0,
		location,
	)

	expected := time.Date(
		2026, 11, 26,
		12, 0, 0, 0,
		time.UTC,
	)

	actual, err := policy.Cutoff(now)
	if err != nil {
		t.Fatal(err)
	}

	if !actual.Equal(expected) {
		t.Fatalf(
			"expected cutoff %s, got %s",
			expected,
			actual,
		)
	}

	if actual.Location() != time.UTC {
		t.Fatal("cutoff timezone was not normalized")
	}
}

func TestPolicyCutoffDurationBoundaries(t *testing.T) {
	now := time.Date(
		2026, 11, 27,
		12, 0, 0, 0,
		time.UTC,
	)

	for _, duration := range []time.Duration{
		MinRetentionDuration,
		RecommendedRetentionDuration,
		MaxRetentionDuration,
	} {
		t.Run(duration.String(), func(t *testing.T) {
			policy := DefaultPolicy()

			policy.RetentionDuration = duration

			cutoff, err := policy.Cutoff(now)
			if err != nil {
				t.Fatal(err)
			}

			if now.Sub(cutoff) != duration {
				t.Fatal("incorrect retention duration")
			}
		})
	}
}

func TestPolicyCutoffDisabled(t *testing.T) {
	policy := DefaultPolicy()

	cutoff, err := policy.Cutoff(time.Now())

	if !errors.Is(err, ErrRetentionDisabled) {
		t.Fatalf(
			"expected disabled retention error, got %v",
			err,
		)
	}

	if !cutoff.IsZero() {
		t.Fatal("disabled retention returned a cutoff")
	}
}

func TestPolicyCutoffInvalidPolicy(t *testing.T) {
	policy := DefaultPolicy()

	policy.RetentionDuration = -time.Hour

	cutoff, err := policy.Cutoff(time.Now())

	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf(
			"expected invalid policy, got %v",
			err,
		)
	}

	if !cutoff.IsZero() {
		t.Fatal("invalid policy returned a cutoff")
	}
}

func TestPolicyCutoffInvalidClock(t *testing.T) {
	policy := DefaultPolicy()

	policy.RetentionDuration = MinRetentionDuration

	tests := []struct {
		name string
		now  time.Time
	}{
		{
			name: "zero time",
			now:  time.Time{},
		},
		{
			name: "before unix epoch",
			now:  time.UnixMilli(-1).UTC(),
		},
		{
			name: "unix epoch",
			now:  time.UnixMilli(0).UTC(),
		},
		{
			name: "cutoff before unix epoch",
			now: time.Date(
				1970, 1, 1,
				12, 0, 0, 0,
				time.UTC,
			),
		},
		{
			name: "cutoff at unix epoch",
			now: time.Date(
				1970, 1, 2,
				0, 0, 0, 0,
				time.UTC,
			),
		},
		{
			name: "unsupported future year",
			now: time.Date(
				10000, 1, 1,
				0, 0, 0, 0,
				time.UTC,
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cutoff, err := policy.Cutoff(tt.now)

			if !errors.Is(err, ErrInvalidCutoff) {
				t.Fatalf(
					"expected invalid cutoff, got %v",
					err,
				)
			}

			if !cutoff.IsZero() {
				t.Fatal("invalid clock returned a cutoff")
			}
		})
	}
}

func TestPolicyCutoffEarliestSupportedBoundary(
	t *testing.T,
) {
	policy := DefaultPolicy()

	policy.RetentionDuration = MinRetentionDuration

	now := time.UnixMilli(1).
		Add(MinRetentionDuration).
		UTC()

	cutoff, err := policy.Cutoff(now)
	if err != nil {
		t.Fatal(err)
	}

	if cutoff.UnixMilli() != 1 {
		t.Fatal("incorrect earliest supported cutoff")
	}
}

func TestPolicyIndependentDefaults(t *testing.T) {
	first := DefaultPolicy()

	first.RetentionDuration = MaxRetentionDuration
	first.BatchSize = MaxBatchSize

	second := DefaultPolicy()

	if second.Enabled() {
		t.Fatal("modifying policy changed default retention")
	}

	if second.BatchSize != DefaultBatchSize {
		t.Fatal("modifying policy changed default batch size")
	}
}
