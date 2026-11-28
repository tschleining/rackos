package audit

import (
	"context"

	"github.com/tschleining/rackos/internal/appliance/admin"
)

const EventLogout EventType = "auth.logout"

// RecordLogout records successful revocation of an
// authenticated administrator session.
//
// The caller must delete the session and record the
// event within the same database transaction.
func (r *Repository) RecordLogout(
	ctx context.Context,
	administratorID string,
	sessionID string,
) error {
	if !admin.IsValidID(administratorID) ||
		!isSessionID(sessionID) {
		return ErrInvalidEvent
	}

	return r.record(
		ctx,
		EventLogout,
		"administrator",
		administratorID,
		"session",
		sessionID,
	)
}
