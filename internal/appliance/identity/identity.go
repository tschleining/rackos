package identity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
)

const metadataKey = "installation_id"

var idPattern = regexp.MustCompile(
	`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`,
)

func LoadOrCreate(ctx context.Context, db *sql.DB) (string, error) {
	var id string

	err := db.QueryRowContext(
		ctx,
		"SELECT value FROM appliance_metadata WHERE key = ?",
		metadataKey,
	).Scan(&id)

	if err == nil {
		return validate(id)
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("load installation identity: %w", err)
	}

	id, err = generateID()
	if err != nil {
		return "", fmt.Errorf("generate installation identity: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin identity transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO appliance_metadata (key, value)
		 VALUES (?, ?)
		 ON CONFLICT(key) DO NOTHING`,
		metadataKey,
		id,
	)

	if err != nil {
		return "", fmt.Errorf("store installation identity: %w", err)
	}

	err = tx.QueryRowContext(
		ctx,
		"SELECT value FROM appliance_metadata WHERE key = ?",
		metadataKey,
	).Scan(&id)

	if err != nil {
		return "", fmt.Errorf("read stored installation identity: %w", err)
	}

	id, err = validate(id)
	if err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit installation identity: %w", err)
	}

	return id, nil
}

func generateID() (string, error) {
	var b [16]byte

	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	// Set UUID version 4.
	b[6] = (b[6] & 0x0f) | 0x40

	// Set the RFC UUID variant.
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		b[0:4],
		b[4:6],
		b[6:8],
		b[8:10],
		b[10:16],
	), nil
}

func validate(id string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", errors.New("invalid stored installation identity")
	}

	return id, nil
}
