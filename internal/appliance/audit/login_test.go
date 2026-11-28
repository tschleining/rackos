package audit

import (
	"context"
	"errors"
	"testing"
)

const testLoginSessionID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestAuditLoginSucceeded(t *testing.T) {
	ctx := context.Background()

	db := openAuditTestDatabase(t)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	repository := NewTxRepository(tx)

	if err := repository.RecordLoginSucceeded(
		ctx,
		testAdministratorID,
		testLoginSessionID,
	); err != nil {
		t.Fatal(err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertAuditCount(t, db, 1)

	var count int

	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM audit_events
		WHERE event_type = 'auth.login.succeeded'
			AND actor_kind = 'administrator'
			AND actor_id = ?
			AND target_kind = 'session'
			AND target_id = ?
			AND outcome = 'success'
			AND occurred_at > 0
	`,
		testAdministratorID,
		testLoginSessionID,
	).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatal("incorrect login audit metadata")
	}
}

func TestAuditLoginRejectsInvalidIdentity(
	t *testing.T,
) {
	ctx := context.Background()

	db := openAuditTestDatabase(t)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	repository := NewTxRepository(tx)

	tests := []struct {
		administratorID string
		sessionID       string
	}{
		{"invalid", testLoginSessionID},
		{"", testLoginSessionID},
		{testAdministratorID, ""},
		{testAdministratorID, "invalid"},
		{
			testAdministratorID,
			"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		},
		{
			testAdministratorID,
			"gggggggggggggggggggggggggggggggg",
		},
		{
			testAdministratorID,
			testLoginSessionID + "\x00",
		},
	}

	for _, tc := range tests {
		err := repository.RecordLoginSucceeded(
			ctx,
			tc.administratorID,
			tc.sessionID,
		)

		if !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	assertAuditCount(t, db, 0)
}
