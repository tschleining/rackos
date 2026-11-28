
# Administrator Sessions

RackOS uses server-side administrator sessions for
management authentication.

Session persistence is implemented in database
Migration 008. Administrative audit storage is
implemented in Migration 009.

## Session Policy

| Setting | Value |
| --- | --- |
| Authentication token entropy | 256 bits |
| Token encoding | Unpadded Base64URL |
| Encoded token length | 43 characters |
| Stored token representation | SHA-256 digest |
| Session identifier | 128-bit random hexadecimal ID |
| Inactivity timeout | 30 minutes |
| Absolute lifetime | 12 hours |
| Maximum concurrent sessions | 10 per administrator |
| Maintenance interval | 1 minute |
| Restart behavior | Require reauthentication |

All persisted timestamps use Unix milliseconds.
Public session timestamps are represented in UTC.

## Token Security

Session authentication tokens are generated using
Go's cryptographic random generator.

Only SHA-256 digests of the original random token
bytes are persisted. Plaintext tokens are never
written to session storage or audit records.

Session identifiers are independent, nonsecret
identifiers. They cannot authenticate requests.

IssuedSession excludes its secret token from JSON
serialization and redacts ordinary formatted output.
Callers must not log the Token accessor's return value.

## Session Issuance

The session repository accepts an administrator
snapshot obtained during successful password
verification.

The original credential revision must be preserved
between verification and session issuance.

Session issuance performs the following operations
within a SQLite write transaction:

1. Acquire the database writer lock.
2. Evaluate the appliance clock.
3. Remove invalid sessions.
4. Verify the administrator is active and its
   credential revision remains unchanged.
5. Enforce the maximum session count.
6. Insert the session and its token digest.
7. Update the administrator's last login timestamp.
8. Record a successful login audit event.
9. Commit the transaction.

The authentication token is returned only after
successful transaction commit.

The repository does not itself verify passwords.

## Session Validation

Validation checks the token digest, administrator
status, credential revision, inactivity deadline
and absolute expiration.

Ordinary validation is read-only.

Background dashboard polling must not renew
session activity.

Qualifying activity uses a separate conditional
database update that cannot revive expired sessions
or extend absolute expiration.

A session expires at the exact inactivity or
absolute expiration boundary.

## Session Revocation

Authenticated logout permanently deletes its
session and records an auth.logout audit event
within the same transaction.

Password recovery atomically replaces password
verification material, advances the credential
revision, deletes all administrator sessions and
records an admin.password.changed audit event.

Failure of any operation rolls back the complete
recovery transaction.

Historical audit records are not deleted when
sessions are revoked.

## Maintenance and Restart

The appliance removes expired, inactive and
otherwise invalid sessions once per minute.

Invalid sessions cannot authenticate even before
physical cleanup occurs.

Persisted sessions are invalidated during daemon
startup before management requests are accepted.

The maintenance worker is canceled and joined
before the database closes.

## Clock Handling

The daemon shares one session repository instance
between session operations and maintenance.

A process-local clock high-water mark rejects
observed backward clock movement.

Session writes acquire the SQLite writer lock
before sampling the clock.

Startup invalidation prevents persisted sessions
from surviving daemon restarts.

These safeguards do not provide complete
protection against arbitrary manipulation of
the operating-system clock.

## Audit Logging

The following events are currently supported:

- admin.created
- admin.password.changed
- auth.login.succeeded
- auth.logout

Audit records contain structured event types,
timestamps, actor identifiers, target identifiers
and outcomes.

Passwords, salts, password hashes, authentication
tokens and arbitrary diagnostic messages are
excluded from the audit schema.

The audit log is application-level append-only.
It is not tamper-proof against direct database
access.

## Current Security Boundary

The session package provides persistent session storage, validation, activity renewal, revocation and lifecycle management.

The HTTP server provides administrator authentication middleware, secure session cookie helpers and an authenticated management router.

HTTP authentication must preserve the original verified credential revision and share the daemon's session repository.

Authenticated management operations must require trusted HTTPS.

Security-sensitive mutations must enforce authorization at their transaction boundary.

Future administrator disablement and password change operations must invalidate sessions atomically.

Browser login, logout endpoints, CSRF protection and live daemon authentication enforcement remain pending.

RackOS does not yet provide a complete production administrator security boundary.

See [Management Authentication](management-authentication.md) for the HTTP authentication policy and implementation status.

## Testing

Session tests:

    go test -race -count=1 ./internal/appliance/session

Administrator CLI integration tests:

    go test -race -count=1 ./cmd/rackosctl

Daemon integration tests:

    go test -race -count=1 ./cmd/rackosd

Complete backend regression suite:

    go test -race -count=1 ./...
