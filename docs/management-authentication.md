# Management Authentication

RackOS provides HTTP authentication middleware backed by persistent administrator sessions.

## Authentication Policy

The authenticated management router requires a valid administrator session by default.

Only the following endpoints permit unauthenticated GET and HEAD requests:

| Endpoint | Purpose |
| --- | --- |
| `/healthz` | Appliance liveness |
| `/readyz` | Appliance readiness |
| `/api/v1/version` | Appliance version |

All other management requests require authentication.

Encoded paths, alternate paths, unsupported methods and newly introduced management endpoints do not automatically inherit public access.

## Session Cookies

Administrator authentication uses a browser session cookie with the following attributes:

| Attribute | Value |
| --- | --- |
| Name | `__Host-rackos_session` |
| Secure | Enabled |
| HttpOnly | Enabled |
| SameSite | Strict |
| Path | `/` |
| Domain | Not specified |
| Lifetime | Browser session |

Server-side session expiration remains authoritative regardless of browser cookie persistence.

Authentication tokens contain 256 bits of cryptographically generated randomness and use canonical unpadded Base64URL encoding.

Duplicate, malformed and noncanonical authentication cookies are rejected.

Authentication tokens must never appear in application logs, URLs, audit records or JSON responses.

## Request Processing

Management authentication operates in the following order:

1. Validate the local management authority.
2. Enforce Origin and Fetch Metadata restrictions.
3. Determine whether the request targets an explicitly public endpoint.
4. Require an actual TLS connection for protected requests.
5. Extract and validate the session cookie.
6. Validate the session against persistent storage.
7. Attach validated session metadata to the request context.
8. Execute the protected management handler.

Forwarded protocol headers are not trusted.

## Authentication Responses

| Status | Meaning |
| --- | --- |
| 401 Unauthorized | Missing, malformed, expired, revoked or otherwise invalid session |
| 403 Forbidden | Local management security rejection or protected request without TLS |
| 503 Service Unavailable | Authentication infrastructure unavailable |

Authentication failures return generic error messages without exposing internal database errors or sensitive credential information.

Management responses use `Cache-Control: no-store`.

## Session Validation

Session validation verifies:

- Authentication token digest.
- Administrator account status and role.
- Credential revision.
- Inactivity expiration.
- Absolute session expiration.

Ordinary authenticated requests are read-only with respect to persistent session state.

Background dashboard polling does not renew session activity or extend session lifetime.

Revoked sessions cannot authenticate subsequent requests.

Session validation uses a five-second operation timeout that does not impose the same deadline on downstream management handlers.

## Security Boundaries

Authentication establishes administrator identity. It does not independently provide operation-specific authorization or CSRF protection.

Sensitive mutations must enforce authorization at their transaction boundary.

Management access remains restricted to explicit loopback IP addresses.

Remote management access, reverse proxy deployment and port forwarding are not supported.

## Current Implementation Status

The authenticated management router, session cookie helpers and authentication middleware are implemented and tested.

The running appliance daemon does not yet use the authenticated router.

Daemon enforcement is deferred until browser authentication endpoints and the associated security controls are implemented.

The current management console must not be considered an authenticated production management interface.

## Testing

Authentication middleware and router tests:

```sh
go test -race -count=1 ./internal/appliance/httpserver
```

Persistent session authentication integration test:

```sh
go test -race -count=1 \
  -run '^TestManagementAuthenticationPersistentSessions$' \
  ./internal/appliance/httpserver
```

Complete backend regression suite:

```sh
go test -race -count=1 ./...
```

## Related Documentation

- [Administrator Sessions](administrator-sessions.md)
- [Management API](management-api.md)