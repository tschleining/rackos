package database

// Migration 009: Administrative audit events.
//
// Audit records deliberately contain no arbitrary
// messages, password material or authentication tokens.
//
// Actor and target references are historical identifiers,
// not foreign keys. Audit records must survive deletion
// of the associated resources.
const migration009AuditEvents = `
	CREATE TABLE audit_events (
		id TEXT PRIMARY KEY NOT NULL
			CHECK(
				typeof(id) = 'text'
				AND length(id) = 32
				AND length(CAST(id AS BLOB)) = 32
				AND id NOT GLOB '*[^0-9a-f]*'
			),

		occurred_at INTEGER NOT NULL
			CHECK(
				typeof(occurred_at) = 'integer'
				AND occurred_at > 0
			),

		event_type TEXT NOT NULL
			CHECK(
				typeof(event_type) = 'text'
				AND length(event_type) BETWEEN 3 AND 64
				AND length(CAST(event_type AS BLOB)) = length(event_type)
				AND event_type NOT GLOB '*[^a-z0-9.]*'
			),

		actor_kind TEXT NOT NULL
			CHECK(
				actor_kind IN (
					'local_operator',
					'administrator',
					'anonymous',
					'system'
				)
			),

		actor_id TEXT,

		target_kind TEXT NOT NULL
			CHECK(
				target_kind IN (
					'administrator',
					'session',
					'device',
					'enrollment_invitation',
					'none'
				)
			),

		target_id TEXT,

		outcome TEXT NOT NULL
			CHECK(outcome IN ('success', 'failure')),

		CHECK(
			(actor_kind = 'administrator' AND actor_id IS NOT NULL)
			OR
			(actor_kind <> 'administrator' AND actor_id IS NULL)
		),

		CHECK(
			actor_id IS NULL
			OR (
				typeof(actor_id) = 'text'
				AND length(actor_id) = 36
				AND length(CAST(actor_id AS BLOB)) = 36
				AND substr(actor_id, 9, 1) = '-'
				AND substr(actor_id, 14, 1) = '-'
				AND substr(actor_id, 19, 1) = '-'
				AND substr(actor_id, 24, 1) = '-'
				AND substr(actor_id, 15, 1) = '4'
				AND substr(actor_id, 20, 1) IN ('8', '9', 'a', 'b')
				AND length(replace(actor_id, '-', '')) = 32
				AND replace(actor_id, '-', '') NOT GLOB '*[^0-9a-f]*'
			)
		),

		CHECK(
			(target_kind = 'none' AND target_id IS NULL)
			OR
			(
				target_kind IN ('administrator', 'device')
				AND target_id IS NOT NULL
				AND typeof(target_id) = 'text'
				AND length(target_id) = 36
				AND length(CAST(target_id AS BLOB)) = 36
				AND substr(target_id, 9, 1) = '-'
				AND substr(target_id, 14, 1) = '-'
				AND substr(target_id, 19, 1) = '-'
				AND substr(target_id, 24, 1) = '-'
				AND substr(target_id, 15, 1) = '4'
				AND substr(target_id, 20, 1) IN ('8', '9', 'a', 'b')
				AND length(replace(target_id, '-', '')) = 32
				AND replace(target_id, '-', '') NOT GLOB '*[^0-9a-f]*'
			)
			OR
			(
				target_kind IN ('session', 'enrollment_invitation')
				AND target_id IS NOT NULL
				AND typeof(target_id) = 'text'
				AND length(target_id) = 32
				AND length(CAST(target_id AS BLOB)) = 32
				AND target_id NOT GLOB '*[^0-9a-f]*'
			)
		)
	);

	CREATE INDEX idx_audit_events_occurred_at
		ON audit_events(occurred_at, id);

	CREATE INDEX idx_audit_events_target
		ON audit_events(target_kind, target_id, occurred_at, id);
`
