# Management Authentication

RackOS provides administrator authentication through persistent server-side sessions, secure browser cookies, strict request validation, and an authenticated management API router.

## Implementation Status

The administrator authentication API and authenticated management router are implemented and tested.

The running appliance daemon does not yet use the complete authenticated router.

Frontend authentication integration, live daemon enforcement, and browser-level security testing remain pending.

The existing management console must not be considered an authenticated production management interface.

## Authentication Policy

The authenticated management router requires a valid administrator session by default.

| Method | Endpoint | Authentication | CSRF Protection |
| --- | --- | --- | --- |
| GET, HEAD | `/healthz` | No | No |
| GET, HEAD | `/readyz` | No | No |
| GET, HEAD | `/api/v1/version` | No | No |
| POST | `/api/v1/auth/login` | No | Yes |
| POST | `/api/v1/auth/logout` | Yes | Yes |
| GET, HEAD | `/api/v1/auth/session` | Yes | No |
| POST | `/api/v1/auth/activity` | Yes | Yes |

All other management endpoints require authentication.

Public authentication exceptions match exact HTTP methods and canonical endpoint paths.

Encoded paths, alternate paths, unsupported methods, and newly introduced endpoints do not automatically inherit public access.

All authentication endpoints require HTTPS.

Public health, readiness, and version endpoints may remain accessible over local HTTP.

## Router Construction

The complete authenticated management router is constructed using `NewAuthenticatedManagementHandlerWithAuth`.

Authentication endpoints and protected management routes share the same persistent session repository.

The router owns a shared login rate limiter.

The appliance must retain one router instance throughout its server lifetime.

The session repository must also be shared with daemon session maintenance.

Network-facing servers must retain bounded request read, write, and header timeouts.

## Login

    POST /api/v1/auth/login

Required headers:

    Content-Type: application/json
    X-RackOS-CSRF: 1
    Origin: <exact management origin>

Request body:

```json
{
  "username": "admin",
  "password": "example-password"
}
```

Usernames are validated and normalized using the existing administrator username policy.

Passwords retain their original whitespace and are verified using the existing Argon2id credential verification implementation.

Login does not reapply new-password blocklist requirements.

The request body is limited to 25,600 bytes, allowing JSON escape overhead for passwords up to the supported input limit of 4,096 bytes.

Login requests reject:

- Unknown or duplicate JSON fields.
- Incorrect field capitalization.
- Missing or null credential fields.
- Malformed Unicode and invalid UTF-8.
- Additional JSON documents and trailing non-whitespace content.
- Unsupported content types and encodings.
- Query parameters.
- Password inputs exceeding the supported limit.

Unknown and disabled administrator accounts perform dummy password verification to reduce account enumeration through timing differences.

Invalid credentials receive a generic authentication failure.

Malformed requests may receive validation errors independently of administrator account existence.

Dummy password verification does not guarantee identical response timings.

### Login Transaction

Successful password verification returns the original administrator snapshot associated with the verified credential revision.

Session issuance independently verifies that the administrator remains eligible and the credential revision remains unchanged.

The session, administrator last-login timestamp, and successful login audit event are committed atomically.

The browser receives its authentication cookie only after successful transaction commit.

Failed session issuance does not return an authentication cookie.

A successful login returns HTTP 200 and the public session response.

### Login Rate Limiting

| Setting | Value |
| --- | --- |
| Maximum admitted attempts | 5 |
| Rolling window | 60 seconds |
| Concurrent login operations | 1 |
| Scope | Shared management router |
| Successful login resets limit | No |
| Persistence | In-memory |
| Rejection status | HTTP 429 |

The production appliance must use a single shared router so the rate limiter applies appliance-wide.

Admitted attempts consume capacity regardless of whether authentication succeeds.

Malformed requests that pass local security and CSRF checks also consume capacity.

Requests rejected by local security, CSRF protection, or concurrent admission do not consume additional attempts.

Rate-limited responses include a Retry-After header containing a whole-second retry interval.

Rate limiter state resets when the daemon restarts.

The existing process-wide password hashing concurrency limit provides an additional resource protection boundary.

## Logout

    POST /api/v1/auth/logout

Logout requires an authenticated session, HTTPS, the exact management Origin, and the custom CSRF header.

Request bodies and query parameters are not permitted.

Logout permanently revokes only the current authenticated session.

Session deletion and the logout audit event are committed within the same transaction.

The browser authentication cookie is deleted only after successful transaction commit.

Successful logout returns HTTP 204 without a response body.

Failed revocation does not clear the authentication cookie.

Replaying a revoked token returns HTTP 401.

Other administrator sessions remain unaffected.

## Current Session

    GET /api/v1/auth/session

This endpoint returns the current authenticated session without modifying persistent session state.

HEAD is also supported.

The endpoint requires HTTPS and a valid authentication cookie.

A CSRF header is not required because this operation is read-only.

Request bodies and query parameters are not permitted.

Session inspection does not renew activity, extend expiration, or modify authentication cookies.

Successful session inspection returns HTTP 200 and the public session response.

## Session Activity

    POST /api/v1/auth/activity

This endpoint explicitly records administrator activity and renews the session inactivity window.

It requires HTTPS, an authenticated session, the exact management Origin, and the custom CSRF header.

Request bodies and query parameters are not permitted.

The session repository independently validates the session within the activity write transaction.

Activity renewal cannot:

- Restore an expired or revoked session.
- Extend the absolute session lifetime.
- Change the administrator identity.
- Change the original credential revision.
- Replace the authentication token.
- Renew unrelated administrator sessions.

Successful activity renewal returns HTTP 200 and updated public session metadata.

Background polling must not call the activity endpoint.

The frontend must distinguish genuine administrator interaction from automatic application activity and throttle renewal requests.

The server cannot independently establish that an activity request represents human interaction.

## Public Session Response

Successful login, current session inspection, and activity renewal return the following JSON structure:

```json
{
  "administrator_id": "12345678-1234-4000-8000-123456789abc",
  "created_at": "2026-11-27T12:00:00Z",
  "last_activity_at": "2026-11-27T12:10:00Z",
  "expires_at": "2026-11-28T00:00:00Z",
  "idle_expires_at": "2026-11-27T12:40:00Z"
}
```

All timestamps are represented in UTC.

| Field | Description |
| --- | --- |
| administrator_id | Authenticated administrator identity |
| created_at | Original session creation timestamp |
| last_activity_at | Latest recorded qualifying activity |
| expires_at | Absolute session expiration |
| idle_expires_at | Effective inactivity expiration |

The effective inactivity expiration cannot exceed the absolute session expiration.

Authentication tokens, password verification material, internal session identifiers, and credential revisions are excluded.

## Session Lifetime

| Setting | Value |
| --- | --- |
| Inactivity timeout | 30 minutes |
| Absolute session lifetime | 12 hours |
| Maximum active sessions | 10 |
| Maintenance interval | 1 minute |
| Restart behavior | Require reauthentication |

Ordinary management requests and current session inspection do not renew activity.

Sessions expire at the exact inactivity or absolute expiration boundary.

Server-side expiration remains authoritative regardless of browser cookie persistence.

## Session Cookies

| Attribute | Value |
| --- | --- |
| Name | `__Host-rackos_session` |
| Secure | Enabled |
| HttpOnly | Enabled |
| SameSite | Strict |
| Path | `/` |
| Domain | Not specified |
| Lifetime | Browser session |

Authentication tokens contain 256 bits of cryptographically generated randomness and use canonical unpadded Base64URL encoding.

Only token digests are persisted.

Duplicate, malformed, and noncanonical authentication cookies are rejected.

Authentication tokens must never appear in URLs, application logs, audit records, or JSON responses.

Cookies are scoped by host rather than TCP port. Management hosts must therefore not be treated as isolated from other services on the same host solely because they use different ports.

## CSRF Protection

Authentication mutations require:

1. An actual HTTPS connection.
2. Exactly one matching Origin header.
3. Exactly one `X-RackOS-CSRF: 1` header.
4. Compliance with local management authority and Fetch Metadata validation.

The custom CSRF header is not a secret authentication token.

RackOS does not authorize cross-origin CORS requests.

Cross-origin form submissions cannot provide the required custom header.

Authentication mutations do not accept requests with missing or ambiguous Origin headers.

Non-browser authentication clients must explicitly supply the required Origin and custom CSRF header.

Forwarded protocol headers are not trusted.

CSRF protection does not protect against same-origin cross-site scripting.

## Authentication Responses

| Status | Meaning |
| --- | --- |
| 200 | Successful login, session inspection, or activity renewal |
| 204 | Successful logout |
| 400 | Invalid authentication request |
| 401 | Invalid credentials or invalid session |
| 403 | Local management, HTTPS, or CSRF security violation |
| 405 | Unsupported method after required authentication |
| 409 | Maximum active session count reached |
| 413 | Login request body too large |
| 415 | Unsupported login content type or encoding |
| 429 | Login rate limit or password hashing capacity exhausted |
| 503 | Authentication infrastructure unavailable |

Authentication error responses use generic JSON error messages.

Example:

```json
{
  "error": "invalid administrator credentials"
}
```

Internal database errors and sensitive credential information are not exposed.

Management responses use:

    Cache-Control: no-store

Clients must distinguish invalid authentication from temporary authentication infrastructure failures.

HTTP 503 must not automatically be interpreted as session expiration.

## Security Boundaries

Management access remains restricted to explicit loopback IP addresses.

Remote management, reverse proxy deployment, and port forwarding are not supported.

Authentication establishes administrator identity but does not replace operation-specific authorization.

Sensitive mutations must enforce authorization at their transaction boundary.

A malicious local process may exhaust the shared login attempt budget and temporarily prevent legitimate login attempts.

Repeated successful logins can create additional sessions until the configured capacity limit is reached.

If a connection fails after session issuance commits but before cookie delivery, the unused session may persist until expiration or cleanup.

The existing password blocklist is limited and must be expanded before production browser authentication is enabled.

Password buffer clearing provides best-effort memory hygiene rather than guaranteed secret erasure.

## Testing

Authentication and session security regression tests:

```sh
go test -race -shuffle=on -count=3 \
  ./internal/appliance/httpserver \
  ./internal/appliance/session \
  ./internal/appliance/admin \
  ./internal/appliance/auth
```

Complete persistent HTTPS authentication lifecycle:

```sh
go test -race -count=1 \
  -run '^TestAuthRouterPersistentLifecycle$' \
  ./internal/appliance/httpserver
```

Complete backend regression suite:

```sh
go test -race -count=1 ./...
```

Integration tests exercise real HTTPS connections, persistent SQLite sessions, Argon2id verification, HTTP cookie-jar behavior, and transactional audit logging.

Actual browser authentication and daemon integration require additional end-to-end testing.

## Related Documentation

- [Administrator Sessions](administrator-sessions.md)
- [Management API](management-api.md)