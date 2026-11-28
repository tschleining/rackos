package session

import (
	"errors"
	"time"
)

const (
	TokenSize        = 32
	TokenHashSize    = 32
	SessionIDSize    = 16
	EncodedTokenSize = 43

	MaxActiveSessions = 10

	IdleTimeout      = 30 * time.Minute
	AbsoluteLifetime = 12 * time.Hour
	CleanupInterval  = time.Minute
)

var (
	ErrInvalidToken = errors.New(
		"invalid session token",
	)

	ErrInvalidSession = errors.New(
		"invalid or expired session",
	)

	ErrInvalidAdministrator = errors.New(
		"invalid administrator for session issuance",
	)

	ErrCredentialsChanged = errors.New(
		"administrator credentials changed or account unavailable",
	)

	ErrInvalidClock = errors.New(
		"invalid clock for session operation",
	)

	ErrClockRollback = errors.New(
		"appliance clock moved backward",
	)

	ErrSessionLimit = errors.New(
		"administrator session limit reached",
	)

	ErrTransactionRequired = errors.New(
		"session revocation requires a transaction",
	)
)

// Session contains nonsecret session metadata.
type Session struct {
	ID string `json:"id"`

	AdministratorID string `json:"administrator_id"`

	PasswordChangedAt time.Time `json:"-"`

	CreatedAt time.Time `json:"created_at"`

	LastActivityAt time.Time `json:"last_activity_at"`

	ExpiresAt time.Time `json:"expires_at"`
}

// IssuedSession contains a newly issued session
// and its secret authentication token.
type IssuedSession struct {
	Session Session `json:"session"`

	token string
}

// Token returns the secret authentication token.
//
// Callers must never log this value.
func (s IssuedSession) Token() string {
	return s.token
}

func (s IssuedSession) String() string {
	return "[redacted issued session]"
}

func (s IssuedSession) GoString() string {
	return "[redacted issued session]"
}
