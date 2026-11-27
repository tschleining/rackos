package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const administratorColumns = `
	id,
	username,
	role,
	created_at,
	updated_at,
	password_changed_at,
	last_login_at,
	disabled_at
`

type executor interface {
	ExecContext(
		context.Context,
		string,
		...any,
	) (sql.Result, error)

	QueryRowContext(
		context.Context,
		string,
		...any,
	) *sql.Row
}

type Repository struct {
	db  executor
	now func() time.Time
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db:  db,
		now: time.Now,
	}
}

// WithTx returns a repository scoped to an existing
// transaction. The caller controls commit and rollback.
func (r *Repository) WithTx(
	tx *sql.Tx,
) *Repository {
	return &Repository{
		db:  tx,
		now: r.now,
	}
}

// Exists reports whether an administrator account
// has been initialized, including disabled accounts.
func (r *Repository) Exists(
	ctx context.Context,
) (bool, error) {
	var exists bool

	err := r.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(
			SELECT 1 FROM administrators
		)`,
	).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf(
			"check administrator initialization: %w",
			err,
		)
	}

	return exists, nil
}

// CreateInitial creates the appliance's first
// administrator.
//
// The database enforces single-administrator operation.
// The supplied digest must already have been produced
// by a trusted password hashing implementation.
func (r *Repository) CreateInitial(
	ctx context.Context,
	username string,
	digest PasswordDigest,
) (Administrator, error) {
	username, err := NormalizeUsername(username)
	if err != nil {
		return Administrator{}, err
	}

	if err := validatePasswordDigest(digest); err != nil {
		return Administrator{}, err
	}

	id, err := newAdministratorID()
	if err != nil {
		return Administrator{}, fmt.Errorf(
			"generate administrator identity: %w",
			err,
		)
	}

	now := r.now().UnixMilli()

	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO administrators (
			id,
			username,
			password_scheme,
			password_salt,
			password_hash,
			role,
			created_at,
			updated_at,
			password_changed_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`,
		id,
		username,
		digest.Scheme,
		digest.Salt[:],
		digest.Hash[:],
		string(RoleAdministrator),
		now,
		now,
		now,
	)

	if err != nil {
		return Administrator{}, fmt.Errorf(
			"create initial administrator: %w",
			err,
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return Administrator{}, fmt.Errorf(
			"read administrator insert result: %w",
			err,
		)
	}

	if affected == 0 {
		return Administrator{}, ErrAlreadyInitialized
	}

	timestamp := time.UnixMilli(now).UTC()

	return Administrator{
		ID:                id,
		Username:          username,
		Role:              RoleAdministrator,
		CreatedAt:         timestamp,
		UpdatedAt:         timestamp,
		PasswordChangedAt: timestamp,
	}, nil
}

// Get retrieves account metadata by identity.
func (r *Repository) Get(
	ctx context.Context,
	id string,
) (Administrator, error) {
	if !IsValidID(id) {
		return Administrator{}, ErrInvalidID
	}

	row := r.db.QueryRowContext(
		ctx,
		"SELECT "+administratorColumns+
			" FROM administrators WHERE id = ?",
		id,
	)

	return readAdministrator(row)
}

// GetByUsername retrieves account metadata without
// selecting any password verification material.
func (r *Repository) GetByUsername(
	ctx context.Context,
	username string,
) (Administrator, error) {
	username, err := NormalizeUsername(username)
	if err != nil {
		return Administrator{}, err
	}

	row := r.db.QueryRowContext(
		ctx,
		"SELECT "+administratorColumns+
			" FROM administrators WHERE username = ?",
		username,
	)

	return readAdministrator(row)
}

// GetAuthenticationRecord retrieves metadata and
// password verification material in one database read.
//
// This method is reserved for internal authentication
// and must not be exposed through the management API.
func (r *Repository) GetAuthenticationRecord(
	ctx context.Context,
	username string,
) (AuthenticationRecord, error) {
	username, err := NormalizeUsername(username)
	if err != nil {
		return AuthenticationRecord{}, err
	}

	row := r.db.QueryRowContext(
		ctx,
		"SELECT "+administratorColumns+
			`, password_scheme,
			   password_salt,
			   password_hash
			 FROM administrators
			 WHERE username = ?`,
		username,
	)

	var scheme string
	var salt []byte
	var hash []byte

	account, err := readAdministrator(
		row,
		&scheme,
		&salt,
		&hash,
	)
	if err != nil {
		return AuthenticationRecord{}, err
	}

	if scheme != PasswordScheme ||
		len(salt) != PasswordSaltSize ||
		len(hash) != PasswordHashSize {
		return AuthenticationRecord{},
			ErrInvalidPasswordDigest
	}

	digest := PasswordDigest{
		Scheme: scheme,
	}

	copy(digest.Salt[:], salt)
	copy(digest.Hash[:], hash)

	return AuthenticationRecord{
		Administrator: account,
		Password:      digest,
	}, nil
}

type scanner interface {
	Scan(...any) error
}

func readAdministrator(
	row scanner,
	additionalDestinations ...any,
) (Administrator, error) {
	var account Administrator

	var createdAt int64
	var updatedAt int64
	var passwordChangedAt int64

	var lastLoginAt sql.NullInt64
	var disabledAt sql.NullInt64

	destinations := []any{
		&account.ID,
		&account.Username,
		&account.Role,
		&createdAt,
		&updatedAt,
		&passwordChangedAt,
		&lastLoginAt,
		&disabledAt,
	}

	destinations = append(
		destinations,
		additionalDestinations...,
	)

	err := row.Scan(destinations...)

	if errors.Is(err, sql.ErrNoRows) {
		return Administrator{}, ErrNotFound
	}

	if err != nil {
		return Administrator{}, fmt.Errorf(
			"read administrator: %w",
			err,
		)
	}

	account.CreatedAt =
		time.UnixMilli(createdAt).UTC()

	account.UpdatedAt =
		time.UnixMilli(updatedAt).UTC()

	account.PasswordChangedAt =
		time.UnixMilli(passwordChangedAt).UTC()

	if lastLoginAt.Valid {
		timestamp :=
			time.UnixMilli(lastLoginAt.Int64).UTC()

		account.LastLoginAt = &timestamp
	}

	if disabledAt.Valid {
		timestamp :=
			time.UnixMilli(disabledAt.Int64).UTC()

		account.DisabledAt = &timestamp
	}

	return account, nil
}
