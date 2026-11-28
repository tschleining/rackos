package database

// Migration 008: Administrator sessions.
//
// Session tokens are never persisted in plaintext.
// The database stores only their SHA-256 digests.
//
// All timestamps are Unix milliseconds.
const migration008AdministratorSessions = `
	CREATE TABLE administrator_sessions (
		id TEXT PRIMARY KEY NOT NULL
			CHECK(
				typeof(id) = 'text'
				AND length(id) = 32
				AND length(CAST(id AS BLOB)) = 32
				AND id NOT GLOB '*[^0-9a-f]*'
			),

		administrator_id TEXT NOT NULL,

		token_hash BLOB UNIQUE NOT NULL
			CHECK(
				typeof(token_hash) = 'blob'
				AND length(token_hash) = 32
			),

		password_changed_at INTEGER NOT NULL
			CHECK(
				typeof(password_changed_at) = 'integer'
				AND password_changed_at > 0
			),

		created_at INTEGER NOT NULL
			CHECK(
				typeof(created_at) = 'integer'
				AND created_at > 0
			),

		last_activity_at INTEGER NOT NULL
			CHECK(
				typeof(last_activity_at) = 'integer'
				AND last_activity_at >= created_at
			),

		expires_at INTEGER NOT NULL
			CHECK(
				typeof(expires_at) = 'integer'
				AND expires_at > created_at
				AND expires_at - created_at <= 43200000
			),

		CHECK(password_changed_at <= created_at),

		CHECK(last_activity_at < expires_at),

		FOREIGN KEY(administrator_id)
			REFERENCES administrators(id)
			ON DELETE CASCADE
	);

	CREATE INDEX idx_administrator_sessions_administrator
		ON administrator_sessions(administrator_id);

	CREATE INDEX idx_administrator_sessions_expires_at
		ON administrator_sessions(expires_at);

	CREATE INDEX idx_administrator_sessions_last_activity_at
		ON administrator_sessions(last_activity_at);
`
