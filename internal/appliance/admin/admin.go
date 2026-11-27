package admin

import "time"

type Role string

const (
	RoleAdministrator Role = "administrator"

	PasswordScheme = "argon2id-v1"

	PasswordSaltSize = 16
	PasswordHashSize = 32
)

// Administrator contains account metadata only.
//
// Password verification material is deliberately
// excluded from this type.
type Administrator struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     Role   `json:"role"`

	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	PasswordChangedAt time.Time  `json:"password_changed_at"`
	LastLoginAt       *time.Time `json:"last_login_at,omitempty"`
	DisabledAt        *time.Time `json:"disabled_at,omitempty"`
}

func (a Administrator) IsActive() bool {
	return a.DisabledAt == nil
}

// PasswordDigest contains derived password verification
// material. It must never contain a plaintext password.
type PasswordDigest struct {
	Scheme string                 `json:"-"`
	Salt   [PasswordSaltSize]byte `json:"-"`
	Hash   [PasswordHashSize]byte `json:"-"`
}

// String prevents accidental disclosure through
// ordinary formatted output.
func (PasswordDigest) String() string {
	return "[redacted password digest]"
}

func (PasswordDigest) GoString() string {
	return "[redacted password digest]"
}

// AuthenticationRecord is intended exclusively for
// internal password verification.
type AuthenticationRecord struct {
	Administrator Administrator  `json:"administrator"`
	Password      PasswordDigest `json:"-"`
}
