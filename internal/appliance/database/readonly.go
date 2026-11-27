package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// OpenReadOnly opens an existing appliance database
// without initializing directories or applying migrations.
//
// The database must already have the current schema.
//
// SQLite may still require WAL and SHM file access
// when inspecting a WAL-mode database.
func OpenReadOnly(
	ctx context.Context,
	dataDir string,
) (*sql.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf(
			"data directory cannot be empty",
		)
	}

	path, err := filepath.Abs(
		filepath.Join(dataDir, Filename),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"resolve database path: %w",
			err,
		)
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf(
			"access appliance database: %w",
			err,
		)
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf(
			"appliance database is not a regular file",
		)
	}

	uri := url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(path),
	}

	params := url.Values{}

	params.Set("mode", "ro")
	params.Add("_pragma", "busy_timeout(5000)")
	params.Add("_pragma", "foreign_keys(1)")
	params.Add("_pragma", "query_only(1)")

	uri.RawQuery = params.Encode()

	db, err := sql.Open(
		"sqlite",
		uri.String(),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"open read-only appliance database: %w",
			err,
		)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	var schemaVersion int

	err = db.QueryRowContext(
		ctx,
		"PRAGMA user_version",
	).Scan(&schemaVersion)
	if err != nil {
		db.Close()

		return nil, fmt.Errorf(
			"read database schema version: %w",
			err,
		)
	}

	if schemaVersion != len(migrations) {
		db.Close()

		return nil, fmt.Errorf(
			"unsupported database schema version: %d (required: %d)",
			schemaVersion,
			len(migrations),
		)
	}

	if err := ctx.Err(); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
