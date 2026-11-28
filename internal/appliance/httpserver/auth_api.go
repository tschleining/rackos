package httpserver

import (
	"context"
	"time"

	"github.com/tschleining/rackos/internal/appliance/admin"
	"github.com/tschleining/rackos/internal/appliance/session"
)

const authOperationTimeout = 5 * time.Second

// AdministratorAuthenticationReader retrieves the
// original account snapshot and password verification
// material required for administrator authentication.
type AdministratorAuthenticationReader interface {
	GetAuthenticationRecord(
		context.Context,
		string,
	) (admin.AuthenticationRecord, error)
}

// AdministratorSessionStore defines the persistent
// session operations required by the authentication API.
type AdministratorSessionStore interface {
	SessionValidator

	Issue(
		context.Context,
		admin.Administrator,
	) (session.IssuedSession, error)

	Revoke(
		context.Context,
		string,
	) (bool, error)

	RecordActivity(
		context.Context,
		string,
	) (session.Session, error)
}

// Verify that the existing repositories implement
// the authentication API dependencies.
var (
	_ AdministratorAuthenticationReader = (*admin.Repository)(nil)
	_ AdministratorSessionStore         = (*session.Repository)(nil)
)

// authAPIHandler contains the dependencies required
// by administrator authentication endpoints.
//
// All session operations must share the same
// repository instance used by session maintenance.
type authAPIHandler struct {
	administrators AdministratorAuthenticationReader
	sessions       AdministratorSessionStore
}

// authSessionResponse defines the public session
// response independently of persistent storage.
//
// Authentication tokens and credential revisions
// must never be included in this response.
type authSessionResponse struct {
	AdministratorID string    `json:"administrator_id"`
	CreatedAt       time.Time `json:"created_at"`
	LastActivityAt  time.Time `json:"last_activity_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	IdleExpiresAt   time.Time `json:"idle_expires_at"`
}

func toAuthSessionResponse(
	current session.Session,
) authSessionResponse {
	idleExpiresAt := current.LastActivityAt.Add(
		session.IdleTimeout,
	)

	// The effective inactivity deadline cannot
	// exceed the absolute session expiration.
	if idleExpiresAt.After(current.ExpiresAt) {
		idleExpiresAt = current.ExpiresAt
	}

	return authSessionResponse{
		AdministratorID: current.AdministratorID,
		CreatedAt:       current.CreatedAt.UTC(),
		LastActivityAt:  current.LastActivityAt.UTC(),
		ExpiresAt:       current.ExpiresAt.UTC(),
		IdleExpiresAt:   idleExpiresAt.UTC(),
	}
}
