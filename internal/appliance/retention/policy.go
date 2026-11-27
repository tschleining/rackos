package retention

import (
	"errors"
	"fmt"
	"time"
)

const (
	DefaultRetentionDuration     = 0 * time.Second
	RecommendedRetentionDuration = 30 * 24 * time.Hour

	MinRetentionDuration = 24 * time.Hour
	MaxRetentionDuration = 365 * 24 * time.Hour

	DefaultCleanupInterval = 15 * time.Minute
	MinCleanupInterval     = time.Minute
	MaxCleanupInterval     = 24 * time.Hour

	DefaultBatchSize = 500
	MinBatchSize     = 1
	MaxBatchSize     = 1000

	DefaultMaxBatchesPerCycle = 20
	MinBatchesPerCycle        = 1
	MaxBatchesPerCycle        = 100

	DefaultBatchDelay = 100 * time.Millisecond
	MinBatchDelay     = 10 * time.Millisecond
	MaxBatchDelay     = 5 * time.Second

	DefaultBatchTimeout = 5 * time.Second
	MinBatchTimeout     = time.Second
	MaxBatchTimeout     = 30 * time.Second
)

var (
	ErrInvalidPolicy = errors.New(
		"invalid performance retention policy",
	)

	ErrRetentionDisabled = errors.New(
		"performance retention is disabled",
	)

	ErrInvalidCutoff = errors.New(
		"invalid performance retention cutoff",
	)
)

// Policy defines historical performance retention
// and bounded cleanup behavior.
//
// A zero retention duration disables automatic
// historical deletion.
//
// Retention is calculated using appliance receipt
// timestamps rather than agent collection timestamps.
type Policy struct {
	RetentionDuration time.Duration
	CleanupInterval   time.Duration

	BatchSize          int
	MaxBatchesPerCycle int

	BatchDelay   time.Duration
	BatchTimeout time.Duration
}

// DefaultPolicy returns the default retention policy.
//
// Automatic deletion is disabled until an administrator
// explicitly configures a retention duration.
func DefaultPolicy() Policy {
	return Policy{
		RetentionDuration: DefaultRetentionDuration,
		CleanupInterval:   DefaultCleanupInterval,

		BatchSize:          DefaultBatchSize,
		MaxBatchesPerCycle: DefaultMaxBatchesPerCycle,

		BatchDelay:   DefaultBatchDelay,
		BatchTimeout: DefaultBatchTimeout,
	}
}

// Enabled reports whether automatic historical
// retention has been configured.
//
// Callers must validate the policy before using it.
func (p Policy) Enabled() bool {
	return p.RetentionDuration > 0
}

// Validate verifies the complete retention policy.
//
// Operational settings are validated even when
// automatic retention is disabled.
func (p Policy) Validate() error {
	if p.RetentionDuration != 0 &&
		(p.RetentionDuration < MinRetentionDuration ||
			p.RetentionDuration > MaxRetentionDuration) {
		return invalidPolicy(
			"retention duration must be zero or between %s and %s",
			MinRetentionDuration,
			MaxRetentionDuration,
		)
	}

	if p.CleanupInterval < MinCleanupInterval ||
		p.CleanupInterval > MaxCleanupInterval {
		return invalidPolicy(
			"cleanup interval must be between %s and %s",
			MinCleanupInterval,
			MaxCleanupInterval,
		)
	}

	if p.BatchSize < MinBatchSize ||
		p.BatchSize > MaxBatchSize {
		return invalidPolicy(
			"batch size must be between %d and %d",
			MinBatchSize,
			MaxBatchSize,
		)
	}

	if p.MaxBatchesPerCycle < MinBatchesPerCycle ||
		p.MaxBatchesPerCycle > MaxBatchesPerCycle {
		return invalidPolicy(
			"maximum batches per cycle must be between %d and %d",
			MinBatchesPerCycle,
			MaxBatchesPerCycle,
		)
	}

	if p.BatchDelay < MinBatchDelay ||
		p.BatchDelay > MaxBatchDelay {
		return invalidPolicy(
			"batch delay must be between %s and %s",
			MinBatchDelay,
			MaxBatchDelay,
		)
	}

	if p.BatchTimeout < MinBatchTimeout ||
		p.BatchTimeout > MaxBatchTimeout {
		return invalidPolicy(
			"batch timeout must be between %s and %s",
			MinBatchTimeout,
			MaxBatchTimeout,
		)
	}

	return nil
}

// Cutoff calculates the exclusive expiration boundary
// for a retention cleanup cycle.
//
// Records with receipt timestamps strictly earlier
// than this boundary are eligible for deletion.
//
// The cutoff is normalized to UTC and truncated to
// milliseconds to match SQLite receipt timestamps.
//
// Disabled retention never produces a usable cutoff.
func (p Policy) Cutoff(
	now time.Time,
) (time.Time, error) {
	if err := p.Validate(); err != nil {
		return time.Time{}, err
	}

	if !p.Enabled() {
		return time.Time{}, ErrRetentionDisabled
	}

	now = now.UTC()

	if now.IsZero() ||
		now.Year() < 1970 ||
		now.Year() > 9999 ||
		now.UnixMilli() <= 0 {
		return time.Time{}, fmt.Errorf(
			"%w: invalid appliance time",
			ErrInvalidCutoff,
		)
	}

	cutoff := now.Add(
		-p.RetentionDuration,
	).Truncate(time.Millisecond)

	if cutoff.Year() < 1970 ||
		cutoff.Year() > 9999 ||
		cutoff.UnixMilli() <= 0 {
		return time.Time{}, fmt.Errorf(
			"%w: timestamp outside supported range",
			ErrInvalidCutoff,
		)
	}

	return cutoff, nil
}

func invalidPolicy(
	format string,
	args ...any,
) error {
	return fmt.Errorf(
		"%w: %s",
		ErrInvalidPolicy,
		fmt.Sprintf(format, args...),
	)
}
