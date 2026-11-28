# Management API

The RackOS Management API provides a read-only interface for retrieving appliance information, managed device status, hardware inventory, and historical performance measurements.

A separate administrator authentication API provides login, logout, session inspection, and session activity renewal.

## Connection

Default base URL:

    http://127.0.0.1:8080

The listening address is configured through the
`RACKOS_HTTP_ADDR` environment variable.

Only explicit loopback IP addresses are permitted.

The Management API supports optional management HTTPS.

Administrator authentication middleware, authentication endpoints, CSRF protection, and the complete authenticated management router are implemented but are not yet enabled in the running appliance daemon.

Existing management requests retain their current behavior until live authentication enforcement is introduced.

Remote management access is not supported.

See [Management Authentication](management-authentication.md) for the authentication policy and current implementation status.

Do not expose the API through a reverse proxy or
port forwarding.

## Endpoints

| Method | Endpoint | Description |
| --- | --- | --- |
| GET | `/healthz` | Appliance liveness |
| GET | `/readyz` | Appliance readiness |
| GET | `/api/v1/version` | Appliance version |
| GET | `/api/v1/devices` | List registered devices |
| GET | `/api/v1/devices/{id}` | Retrieve a device |
| GET | `/api/v1/devices/summary` | Device status counts |
| GET | `/api/v1/devices/{id}/performance/latest` | Latest performance snapshot |
| GET | `/api/v1/devices/{id}/performance/history` | Paginated historical performance |

GET routes also support HEAD through the standard
Go HTTP router.

## Administrator Authentication Endpoints

The authenticated management router provides the following endpoints:

| Method | Endpoint | Description |
| --- | --- | --- |
| POST | `/api/v1/auth/login` | Authenticate and create an administrator session |
| POST | `/api/v1/auth/logout` | Revoke the current administrator session |
| GET, HEAD | `/api/v1/auth/session` | Inspect the current authenticated session |
| POST | `/api/v1/auth/activity` | Explicitly renew session activity |

These endpoints require HTTPS.

All authentication mutations require same-origin CSRF protection.

The authentication endpoints are implemented and tested but are not yet exposed by the running appliance daemon.

See [Management Authentication](management-authentication.md) for complete request and response contracts, cookie behavior, rate limiting, and security requirements.

## List Devices

    GET /api/v1/devices

Returns all registered devices.

Example response:

```json
{
  "devices": [
    {
      "id": "12345678-1234-4000-8000-123456789abc",
      "hostname": "workstation.local",
      "os": "darwin",
      "architecture": "arm64",
      "agent_version": "0.1.0-dev",
      "status": "online",
      "created_at": "2026-11-26T12:00:00Z",
      "updated_at": "2026-11-26T12:05:00Z",
      "last_seen_at": "2026-11-26T12:05:00Z"
    }
  ],
  "total": 1
}
```

An appliance without registered devices returns:

```json
{
  "devices": [],
  "total": 0
}
```

## Retrieve Device

    GET /api/v1/devices/{id}

Returns a single device using its registered device ID.

The response uses the same device representation as
the device list endpoint.

An unknown device returns HTTP 404.

## Device Summary

    GET /api/v1/devices/summary

Returns aggregate device status counts.

Example response:

```json
{
  "total": 3,
  "online": 2,
  "offline": 1
}
```

The total always equals the sum of online and offline
devices.

## Device Status

Device status is calculated when a request is processed.

Agents normally send a heartbeat every 30 seconds.

A device is online when its most recent heartbeat is
less than 90 seconds old and is not in the future.

A device is offline when its heartbeat is at least
90 seconds old or has an invalid future timestamp.

Stopping an agent does not remove its device
registration.

A subsequent successful heartbeat can restore online
status.

## Performance Measurements

Performance endpoints expose historical snapshots
previously submitted by authenticated RackOS agents.

Snapshots include:

- System-wide CPU utilization.
- Memory capacity and utilization.
- Mounted filesystem capacity.
- Cumulative disk activity.
- Disk throughput and operation rates.
- Network interface metadata.
- Cumulative network activity.
- Network throughput and packet rates.

Retrieving performance measurements does not initiate
a new collection on the agent.

Stored measurements remain available when an agent
is offline.

Device IDs supplied to performance endpoints must be
canonical lowercase UUID version 4 values.

## Latest Performance

    GET /api/v1/devices/{id}/performance/latest

Returns the most recently collected performance
snapshot for a registered device.

The latest record is determined by the indexed
collection timestamp, not the appliance receipt
timestamp or insertion order.

Samples with identical indexed collection timestamps
are ordered by sample ID in descending lexicographical
order.

Delayed historical reports do not replace newer
collected measurements.

This endpoint does not accept query parameters.

An unknown device returns HTTP 404.

A registered device without performance measurements
also returns HTTP 404, with a distinct error message.

Example response:

```json
{
  "device_id": "12345678-1234-4000-8000-123456789abc",
  "sample_id": "0123456789abcdef0123456789abcdef",
  "collected_at": "2026-11-26T12:05:00.123456789Z",
  "received_at": "2026-11-26T12:05:00.125Z",
  "payload_version": 1,
  "snapshot": {
    "collected_at": "2026-11-26T12:05:00.123456789Z",
    "cpu": {
      "usage_percent": 25.5,
      "sample_interval_millis": "1000"
    },
    "memory": {
      "total_bytes": "17179869184",
      "available_bytes": "6442450944",
      "used_bytes": "10737418240",
      "usage_percent": 62.5
    },
    "storage": {
      "filesystems": [],
      "disks": [],
      "disk_rates": []
    },
    "network": {
      "interfaces": [],
      "interface_rates": []
    }
  }
}
```

The example measurements are illustrative.

## Historical Performance

    GET /api/v1/devices/{id}/performance/history

Returns a bounded page of historical performance
snapshots for a registered device.

Results are ordered by:

1. Indexed collection timestamp, descending.
2. Sample ID, descending.

The same ordering is used by the latest performance
endpoint.

### Query Parameters

| Parameter | Required | Description |
| --- | --- | --- |
| `from` | No | Inclusive lower collection timestamp |
| `to` | No | Exclusive upper collection timestamp |
| `limit` | No | Maximum number of samples in the page |
| `cursor` | No | Continuation token from a previous page |

The default page size is 10 samples.

The minimum page size is 1 sample.

The maximum page size is 25 samples.

An omitted time bound leaves that side of the
historical range unrestricted.

When both time bounds are supplied, `from` must
precede `to`.

Unknown, duplicate, empty, and malformed query
parameters are rejected.

The maximum raw query string length is 4096 bytes.

### Time Range Semantics

Time bounds use RFC 3339 timestamps.

Both UTC timestamps and explicit timezone offsets
are supported.

Timezone offsets are normalized to UTC.

Time bounds must be aligned to millisecond precision.

Examples:

    2026-11-26T12:00:00Z
    2026-11-26T12:00:00.123Z
    2026-11-26T07:00:00-05:00

Sub-millisecond values are rejected rather than
silently rounded.

Additional fractional digits are accepted only
when they do not introduce sub-millisecond precision.

Time-range filtering applies to indexed Unix
millisecond collection timestamps.

The original collection timestamp remains preserved
in each returned snapshot.

The lower bound is inclusive.

The upper bound is exclusive.

### Historical Response

The response contains:

| Field | Description |
| --- | --- |
| `device_id` | Requested device identity |
| `samples` | Historical performance records |
| `count` | Number of samples in the current page |
| `has_more` | Whether another page was available |
| `next_cursor` | Continuation token or null |

Each sample uses the same representation as the
latest performance response.

The count describes the current page, not the total
number of matching records in the database.

A registered device without matching historical
measurements returns HTTP 200:

```json
{
  "device_id": "12345678-1234-4000-8000-123456789abc",
  "samples": [],
  "count": 0,
  "has_more": false,
  "next_cursor": null
}
```

An unknown device returns HTTP 404.

### Cursor-Based Pagination

Historical retrieval uses keyset pagination.

When additional records exist, the response includes
a continuation cursor identifying the last returned
sample.

Clients request another page by supplying the
returned `next_cursor` value.

Clients must repeat the original time bounds.

Equivalent time bounds expressed using different
timezone offsets are accepted.

The page size may change between requests.

Cursors are bound to the original device identity
and normalized time range.

Invalid, unsupported, malformed, or mismatched
cursors return HTTP 400.

Cursors use a versioned canonical JSON representation
encoded using unpadded Base64URL.

The maximum encoded cursor length is 1024 bytes.

Clients must treat cursors as opaque values and
must not construct or modify them.

Cursors are not encrypted, cryptographically signed,
or used for authentication.

A cursor remains usable after an appliance restart
provided the corresponding query remains valid.

The original cursor record does not need to remain
in the database.

### Pagination Consistency

Pagination operates against live historical storage.

It does not provide a frozen database snapshot
across multiple requests.

New samples ordered before the continuation
position are visible when a fresh query is started.

New samples ordered after the continuation position
may appear in subsequent pages.

Deleted records are omitted from subsequent pages.

An earlier response indicating additional records
does not guarantee that those records will still
exist when the next request is processed.

Clients should deduplicate samples by device ID
and sample ID when combining independent queries.


## Historical Retention

The appliance supports configurable automatic expiration
of historical performance records.

Retention is disabled by default.

Expiration is based on appliance receipt timestamps,
while historical retrieval and time-range filtering
continue to use collection timestamps.

Automatic cleanup does not modify registered device
identities or agent credentials.

The latest performance endpoint returns the latest
remaining collected sample.

If all historical samples for a registered device have
expired, latest retrieval returns HTTP 404 with:

```json
{
  "error": "no performance samples found"
}
```

Historical retrieval returns HTTP 200 with an empty
samples array when no matching measurements remain.

Existing continuation cursors remain valid after
retention removes their corresponding records.

Clients must continue to satisfy the original cursor
device identity and time-range requirements.

Retention may remove records between successive
historical page requests.

A previous response containing has_more=true does not
guarantee that additional records will still exist.

Retention configuration and storage administration
are not exposed through the Management API.

See [Historical Performance Retention](performance-retention.md)
for configuration and expiration semantics.


## Performance JSON Representation

Performance snapshots use the Protocol Buffers JSON
mapping with original protobuf field names.

Response field names use snake_case.

Signed and unsigned 64-bit integer values are
represented as decimal strings to preserve precision.

This includes cumulative counters, byte capacities,
and sampling intervals.

Example:

```json
{
  "total_bytes": "18446744073709551615",
  "available_bytes": "0",
  "used_bytes": "18446744073709551615",
  "usage_percent": 100
}
```

Floating-point utilization measurements and activity
rates are represented as JSON numbers.

Interface indexes, MTUs, and payload versions are
represented as JSON numbers.

Boolean fields use JSON boolean values.

Default-valued non-optional scalar fields are
included in responses.

Empty repeated measurement collections are
represented as empty JSON arrays.

Unavailable optional measurements are omitted.

An explicitly measured zero value is not equivalent
to an unavailable measurement.

For example:

```json
{
  "receive_dropped": "0"
}
```

This represents an available receive-drop counter
with a value of zero.

An absent `transmit_dropped` field indicates that
the measurement is unavailable.

The standalone agent metrics command uses its
existing JSON representation and is not required
to match the management API serialization format.

Unknown protobuf fields remain preserved in
historical storage but are not exposed by the
current JSON representation.

## Timestamps

Device and performance timestamps are represented
in UTC using RFC 3339 with optional fractional
seconds.

| Field | Description |
| --- | --- |
| `created_at` | Device registration time |
| `updated_at` | Last device record update |
| `last_seen_at` | Last recorded device contact |
| `collected_at` | Original performance collection time |
| `received_at` | Appliance receipt time |

Performance collection timestamps preserve their
original protobuf precision.

Receipt timestamps are normalized to milliseconds.

Historical ordering and time-range filtering use
the indexed millisecond collection timestamp.

The envelope and snapshot collection timestamps
represent the same instant.

## Response Headers

Device and performance API JSON responses include:

    Content-Type: application/json; charset=utf-8
    Cache-Control: no-store

Performance responses are serialized before the
HTTP success status is committed.

Serialization failures do not return partially
serialized successful responses.

## Error Responses

| Status | Description |
| --- | --- |
| 200 | Successful request |
| 400 | Invalid request when detected |
| 404 | Device, sample, or endpoint not found |
| 405 | Unsupported HTTP method |
| 500 | Internal server error |

Device and performance handler errors use the
following JSON format:

```json
{
  "error": "device not found"
}
```

Performance handler error messages include:

| Message | Status |
| --- | --- |
| `invalid device ID` | 400 |
| `invalid performance query` | 400 |
| `device not found` | 404 |
| `no performance samples found` | 404 |
| `failed to retrieve device` | 500 |
| `failed to retrieve performance` | 500 |
| `failed to retrieve performance history` | 500 |
| `failed to serialize performance response` | 500 |

Corrupt stored records and unsupported payload
versions result in internal server errors.

A corrupt record within a requested historical
page causes the entire page to fail.

A corrupt latest record does not cause silent
fallback to an older sample.

Routing errors generated by the HTTP server may
use plain text.

Internal database errors are not exposed to clients.

## Request Limits

Performance handlers use a shared five-second
request context for device registration checks
and repository retrieval.

Historical queries retrieve at most the requested
page size plus one lookahead record.

The additional record determines whether another
page exists.

Its payload is decoded and validated when its
actual page is requested.

Page limits bound the number of returned samples,
not the exact serialized JSON response size.

## Usage Examples

Set the appliance address and a registered device ID:

```bash
BASE_URL="http://127.0.0.1:8080"

DEVICE_ID="12345678-1234-4000-8000-123456789abc"
```

Retrieve the latest performance snapshot:

```bash
curl -fsS \
  "$BASE_URL/api/v1/devices/$DEVICE_ID/performance/latest"
```

Retrieve historical performance:

```bash
curl -fsS \
  "$BASE_URL/api/v1/devices/$DEVICE_ID/performance/history?limit=10"
```

Retrieve a bounded historical range:

```bash
curl -fsS --get \
  "$BASE_URL/api/v1/devices/$DEVICE_ID/performance/history" \
  --data-urlencode "from=2026-11-26T12:00:00Z" \
  --data-urlencode "to=2026-11-26T13:00:00Z" \
  --data-urlencode "limit=10"
```

For subsequent pages, repeat the original time
bounds and supply the returned continuation token:

```bash
curl -fsS --get \
  "$BASE_URL/api/v1/devices/$DEVICE_ID/performance/history" \
  --data-urlencode "from=2026-11-26T12:00:00Z" \
  --data-urlencode "to=2026-11-26T13:00:00Z" \
  --data-urlencode "limit=10" \
  --data-urlencode "cursor=$CURSOR"
```

Replace the example device identity with an actual
registered device ID.

Set `CURSOR` to the previous response's
`next_cursor` value before requesting another page.

## Security

Device, inventory, and performance management endpoints
are read-only.

Administrator authentication endpoints modify only
the associated authentication and session state.

Agent credentials, enrollment tokens, credential
hashes, and private certificate keys are not
included in responses.

Management traffic is restricted to loopback
interfaces.

Loopback binding is not a substitute for
authentication.

Filesystem paths, storage device names, network
interface names, and device metadata may reveal
sensitive infrastructure information.

Management responses must not be exposed to
untrusted clients.

The Management API is separate from the
authenticated, TLS-protected agent gRPC service.

## Testing

Performance management coverage includes:

- Repository retrieval and ordering.
- Historical time-range filtering.
- Cursor pagination and validation.
- Stored payload integrity.
- Database restart persistence.
- JSON serialization and numeric precision.
- Optional measurement availability.
- HTTP request validation.
- Device isolation.
- Request cancellation.
- HTTP server integration.
- SQLite-backed HTTP integration.

Run the HTTP server test suite:

```bash
go test -race -count=1 ./internal/appliance/httpserver/
```

Run database-backed performance API integration tests:

```bash
go test -race -count=1 -v \
  -run '^TestPerformanceAPIIntegration$' \
  ./internal/appliance/httpserver/
```

## Current Limitations

- No remote management access.
- No device-list pagination or filtering.
- No client-selectable historical sorting.
- No device modification or deletion endpoints.
- No cross-device performance aggregation.
- No historical downsampling.
- No retention configuration API.
- No storage statistics API.
- No per-device historical retention policy.
- No real-time event subscriptions.
- No performance alerting or threshold evaluation.
- No snapshot-isolated pagination across requests.
- Device summaries are calculated from registered records.
- Administrator authentication is not yet enforced by the running appliance daemon.
- The administrator authentication interface is implemented and tested using an authentication-enabled frontend build.
- Complete browser authentication against the running appliance daemon remains pending.
- Management HTTPS remains optional in the current daemon configuration.
- Remote management access remains unsupported.

See [performance-storage.md](performance-storage.md)
for historical persistence and reporting behavior.

See [performance-metrics.md](performance-metrics.md)
for native collection behavior and measurement
definitions.

See [agent-protocol.md](agent-protocol.md) for
authenticated performance reporting details.