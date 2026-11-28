package httpserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tschleining/rackos/internal/appliance/session"
)

func TestAuthSessionResponse(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)

	current := session.Session{
		ID:                "0123456789abcdef0123456789abcdef",
		AdministratorID:   "12345678-1234-4000-8000-123456789abc",
		PasswordChangedAt: now.Add(-time.Hour),
		CreatedAt:         now,
		LastActivityAt:    now,
		ExpiresAt:         now.Add(session.AbsoluteLifetime),
	}

	response := toAuthSessionResponse(current)

	if response.AdministratorID != current.AdministratorID {
		t.Fatal("incorrect administrator identity")
	}

	if !response.CreatedAt.Equal(current.CreatedAt) ||
		!response.LastActivityAt.Equal(current.LastActivityAt) ||
		!response.ExpiresAt.Equal(current.ExpiresAt) {
		t.Fatal("incorrect session timestamps")
	}

	if !response.IdleExpiresAt.Equal(
		now.Add(session.IdleTimeout),
	) {
		t.Fatal("incorrect inactivity deadline")
	}

	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}

	for _, forbidden := range []string{
		"token",
		"password",
		"credential",
		current.ID,
	} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatal("response exposed internal session metadata")
		}
	}
}

func TestAuthSessionResponseAbsoluteExpiration(t *testing.T) {
	now := time.Now().UTC()

	current := session.Session{
		CreatedAt:      now.Add(-11 * time.Hour),
		LastActivityAt: now,
		ExpiresAt:      now.Add(10 * time.Minute),
	}

	response := toAuthSessionResponse(current)

	if !response.IdleExpiresAt.Equal(current.ExpiresAt) {
		t.Fatal("inactivity deadline exceeded absolute expiration")
	}
}
