
# Historical Performance Storage

RackOS stores historical performance snapshots for
registered devices using SQLite.

Performance measurements include CPU utilization,
memory utilization, filesystem capacity, disk activity,
and network activity.

Agents automatically collect and report performance
measurements over authenticated, verified TLS
connections.

## Architecture

The performance reporting pipeline consists of:

1. Native operating system metrics collection.
2. Protocol Buffers snapshot conversion.
3. Cryptographically random sample identity generation.
4. Authenticated gRPC transmission.
5. Appliance-side validation.
6. Transactional SQLite persistence.
7. Committed sample acknowledgement.

Performance reporting operates independently of
heartbeat monitoring and hardware inventory reporting.

## Database Schema

Migration 006 introduces the device_performance table.

| Column | Type | Description |
| --- | --- | --- |
| device_id | TEXT | Registered device identity |
| sample_id | TEXT | Unique sample identity within the device |
| collected_at | INTEGER | Collection timestamp in Unix milliseconds |
| received_at | INTEGER | Appliance receipt timestamp in Unix milliseconds |
| payload_version | INTEGER | Stored payload format version |
| payload | BLOB | Serialized PerformanceSnapshot message |

The composite primary key consists of device_id and
sample_id.

The device_id column references devices.id with
ON DELETE CASCADE.

Each device may have multiple historical performance
records.

The current payload format version is 1.

## Historical Indexes

The historical query index contains:

    device_id, collected_at, sample_id

This index supports device-specific latest retrieval,
historical time-range filtering, deterministic
ordering, and keyset pagination.

Queries traverse the collection timestamp and sample
identity in descending order.

The index avoids requiring a separate temporary
sorting structure for the supported historical
retrieval queries.

A separate index contains:

    received_at

The receipt timestamp index supports automatic retention
and bounded historical cleanup operations.

Retention uses appliance receipt timestamps rather than
agent collection timestamps.

See [Historical Performance Retention](performance-retention.md)
for configuration and expiration behavior.

## Payload Serialization

Performance snapshots are serialized using Protocol
Buffers and stored as SQLite BLOB values.

This preserves:

- Unsigned 64-bit cumulative counters.
- Floating-point utilization measurements.
- Optional field presence.
- Collection timestamp precision.
- Repeated measurement records.
- Unknown Protocol Buffers fields.

The stored collection timestamp index uses millisecond
precision.

The serialized snapshot preserves the original
collection timestamp at Protocol Buffers precision.

Receipt timestamps are normalized to Unix milliseconds.

## Sample Identity

Each performance report receives a cryptographically
random 128-bit sample identity.

Sample identities are encoded as 32 lowercase
hexadecimal characters.

The agent generates the sample identity once when
constructing a report.

Retries reuse the original identity and measurement
content.

Sample identities are scoped to individual devices.

## Historical Ordering

Performance reporting inserts independent historical
records without overwriting existing measurements.

Historical records may subsequently be removed by
automatic retention or device removal.

Unlike hardware inventory, an older performance
snapshot does not overwrite a newer snapshot.

| Incoming report | Behavior |
| --- | --- |
| New sample identity | Insert historical record |
| Older collection timestamp | Insert historical record |
| Same timestamp with a different sample ID | Insert historical record |
| Identical retry | Accept without modification |
| Existing sample ID with different contents | Reject |

Identical retries preserve the original receipt
timestamp and serialized payload.

Idempotency applies while the original sample remains
stored.

A sample identity may be accepted again after its
original historical record has been removed by
retention.

Decoded Protocol Buffers messages are compared when
resolving duplicate sample identities.

Conflicting samples return the AlreadyExists gRPC
status.

## Historical Retrieval

The performance repository provides three read
operations:

| Operation | Description |
| --- | --- |
| Get | Retrieve a specific performance sample |
| Latest | Retrieve the latest collected sample |
| History | Retrieve a bounded historical page |

Latest and historical retrieval use the indexed
collection timestamp rather than the appliance
receipt timestamp.

When indexed collection timestamps are identical,
sample identities provide deterministic descending
lexicographical ordering.

Original protobuf collection timestamp precision
is preserved in returned records.

Historical time ranges support inclusive lower
bounds and exclusive upper bounds.

Queries are restricted to individual device
identities.

The default historical page size is 10 samples.

The maximum historical page size is 25 samples.

## Historical Pagination

Historical retrieval uses keyset pagination.

A continuation position contains:

- Indexed collection timestamp in Unix milliseconds.
- Sample identity.

Subsequent pages retrieve records ordered strictly
after the continuation position in descending
historical order.

The repository requests one additional record to
determine whether another page exists.

The additional record is not decoded or validated
until its actual page is requested.

Continuation positions do not require the original
cursor record to remain stored.

This allows historical pagination to remain compatible
with automatic retention.

Expired records are omitted from subsequent pages.
Continuation cursors remain usable after the original
cursor record has been deleted.

Historical pagination operates against live data
rather than a frozen database snapshot.

Newly ingested measurements may become visible
between page requests depending on their position
relative to the continuation cursor.

## Historical Retrieval Integrity

All returned performance records undergo stored
payload decoding and validation.

Validation includes:

- Supported payload format version.
- Stored payload size.
- Protobuf decoding.
- Snapshot measurement validation.
- Collection timestamp consistency.
- Receipt timestamp validity.

Corrupt or unsupported records cause retrieval
to fail.

The repository does not silently skip invalid
records or return partially populated pages.

A corrupt latest sample does not cause fallback
to an older sample.

## Management API

The appliance exposes stored performance records
through two read-only HTTP endpoints:

    GET /api/v1/devices/{id}/performance/latest

    GET /api/v1/devices/{id}/performance/history

The HTTP layer verifies device registration before
requesting historical measurements.

Historical responses support time-range filtering,
bounded page sizes, and opaque continuation cursors.

HTTP continuation cursors are bound to the device
identity and normalized time range.

Historical snapshots are serialized using protobuf
JSON mapping to preserve 64-bit integer precision
and optional measurement availability.

Management access remains restricted to loopback
interfaces.

The Management API does not currently implement
administrator authentication or HTTPS.

See [management-api.md](management-api.md) for the
complete HTTP contract.

## Transactional Persistence

Device authentication and performance persistence occur
within the same database transaction.

The appliance acknowledges successful acceptance only
after the transaction commits.

An identical previously committed sample may also be
acknowledged after successful authentication.

Database failures cause the transaction to roll back.

Invalid or unauthorized reports cannot create
historical records.

Performance reporting does not modify device heartbeat
timestamps.

## Acknowledgement Recovery

A network failure may occur after the appliance commits
a sample but before the acknowledgement reaches the
agent.

The agent retains the original pending report and
retries it using the same sample identity.

The appliance recognizes the existing sample and
returns its original receipt timestamp.

A successful retry does not insert another historical
record.

## Automatic Reporting

The normal agent run command starts heartbeat,
hardware inventory, and performance reporting workers.

| Configuration | Default |
| --- | --- |
| Initial collection | Immediately at startup |
| Reporting interval | 60 seconds after successful acknowledgement |
| Collection timeout | 10 seconds |
| RPC timeout | 10 seconds |
| Initial retry delay | 5 seconds |
| Maximum retry delay | 60 seconds |
| Maximum pending report age | 15 minutes |
| Maximum pending reports | 1 |

Collection and reporting are sequential within the
performance worker.

The normal reporting interval starts after successful
acknowledgement rather than following a fixed
wall-clock schedule.

Actual collection intervals include collection and
transmission overhead.

## Retry Policies

Temporary reporting failures preserve the original
pending report.

Retry delays increase exponentially:

    5s, 10s, 20s, 40s, 60s, 60s, ...

Successful reporting resets the retry delay.

Invalid or conflicting samples are discarded.

Locally oversized reports are discarded.

Server-side resource exhaustion is treated as a
potentially temporary failure.

Invalid acknowledgements preserve the pending sample
because the appliance may already have committed it.

A pending report expires after 15 minutes.

Expired reports are discarded and replaced with a
fresh collection.

Pending age is measured using the local monotonic
clock in production.

Pending reports are retained only in memory.

## Agent Lifecycle

Heartbeat, inventory, and performance reporting run
independently using a shared authenticated client
connection.

Temporary performance reporting failures do not stop
heartbeat or inventory reporting.

Authentication failures terminate authenticated
agent operation.

An Unimplemented performance RPC response disables
performance reporting for the current agent session.

Unsupported native performance collection also
disables the performance worker.

Agent shutdown cancels and joins all monitoring workers
before closing the appliance connection.

Unacknowledged pending performance reports are not
flushed during shutdown.

## Security

- Existing per-device bearer credentials are reused.
- Reports are transmitted over verified TLS connections.
- Authentication and persistence are transactional.
- Cross-device credential usage is rejected.
- Revoked and removed devices cannot authenticate.
- Reports are validated before persistence.
- Database queries use parameterized values.
- Credentials are not stored in performance payloads.
- Raw performance payloads are not logged.
- Receipt timestamps originate from the appliance clock.
- Serialized reporting requests are limited to 512 KiB.
- Individual stored payloads are limited to 512 KiB.

Authentication establishes the reporting device
identity but does not establish the accuracy of
measurements supplied by that device.

Filesystem sources, mount paths, disk names, and
network interface names may contain identifying
information.

Appliance databases and backups must be protected
against unauthorized access.

## Credential Revocation and Device Removal

Revoking a device credential prevents future
authenticated reporting.

Existing historical performance records remain stored
subject to the configured retention policy.

Removing a registered device deletes its associated
performance history through SQLite foreign key
cascading deletion.

Device removal is therefore a destructive operation.

## Upgrading Existing Installations

Migration 006 is applied automatically when the
appliance opens its database.

The migration preserves existing:

- Appliance installation identity.
- Registered devices.
- Enrollment tokens.
- Agent credentials.
- Hardware inventory snapshots.

Existing agents do not need to enroll again.

Older agents can continue using the existing
enrollment, heartbeat, and inventory protocol.

## Restart Persistence

Historical performance records remain stored across
normal appliance restarts.

The persistent appliance certificate authority and
installation identity are reused.

Existing agent credentials remain valid unless
revoked or removed.

Agents can reconnect and continue reporting without
requiring another enrollment.

Agent restarts preserve previously committed history
but discard unacknowledged reports held in memory.


## Inspecting Historical Records

The administrative storage command provides a read-only
summary of historical performance storage and SQLite
file and page utilization.

```bash
./bin/rackosctl storage stats --data-dir ./data
```

The command reports:

- Historical sample counts.
- Distinct reporting device counts.
- Serialized performance payload bytes.
- Collection and receipt timestamp ranges.
- SQLite database, WAL, and SHM file sizes.
- Logical SQLite page counts and reusable capacity.

The command requires an existing database with the
current appliance schema.

It does not initialize a database or apply migrations.

See [Historical Performance Retention](performance-retention.md)
for configuration, cleanup behavior, and storage report
interpretation.

Direct database inspection remains available for local
development and troubleshooting:

```bash
sqlite3 -readonly -header -column data/rackos.db '
SELECT
    device_id,
    sample_id,
    datetime(collected_at / 1000, "unixepoch")
        AS collected_utc,
    datetime(received_at / 1000, "unixepoch")
        AS received_utc,
    payload_version,
    length(payload) AS payload_bytes
FROM device_performance
ORDER BY collected_at, sample_id;
'
```

The database path must match the configured appliance
data directory.

SQLite WAL-mode databases may require access to
auxiliary journal files during read-only inspection.

Applications should retrieve historical measurements
through the documented Management API rather than
accessing the appliance database directly.

## Testing

Performance reporting and storage are covered by:

- Protocol definition and compatibility tests.
- Native snapshot conversion tests.
- Appliance-side validation tests.
- Database migration and rollback tests.
- Historical repository tests.
- Transaction commit and rollback tests.
- Idempotent insertion and conflict tests.
- Concurrent retry tests.
- Corrupt payload detection tests.
- Payload version validation tests.
- Authenticated TLS reporting tests.
- Database failure injection tests.
- Client acknowledgement validation tests.
- Reporting scheduler tests.
- Pending report expiration tests.
- Monitoring lifecycle tests.
- Native collection end-to-end tests.
- Lost acknowledgement recovery tests.
- Database restart persistence tests.
- Credential revocation and cascading deletion tests.
- Latest performance retrieval tests.
- Historical time-range filtering tests.
- Millisecond timestamp boundary tests.
- Deterministic historical ordering tests.
- Keyset pagination tests.
- Live pagination consistency tests.
- Deleted continuation record tests.
- Historical retrieval transaction tests.
- Historical query index verification.
- Historical cursor restart persistence tests.
- Performance HTTP query validation tests.
- Performance JSON serialization tests.
- Database-backed HTTP integration tests.

Run the complete repository validation suite:

```bash
go mod verify
go vet ./...
go test -race -count=1 ./...
go build ./...
```

Run end-to-end performance reporting tests:

```bash
go test -race -count=1 -v \
  -run '^TestPerformanceEndToEnd' \
  ./internal/agent/client/
```


## Current Limitations

- No configurable performance reporting interval.
- No configurable retry policy.
- No retry jitter.
- No persistent offline report queue.
- No per-device historical retention policy.
- No historical downsampling or aggregation.
- No strict database size quota.
- No automatic physical database compaction.
- No permanent deduplication of expired samples.
- No management console visualization.
- No performance alerting or threshold evaluation.
- No Windows native performance collection.

Automatic historical retention is available but
disabled by default.

Historical records accumulate while retention is
disabled unless their associated devices are removed.

Disk usage must be monitored during extended
development deployments.

The current implementation is intended for
pre-alpha development and testing.

See [Historical Performance Retention](performance-retention.md)
for expiration policy and storage administration.

See [Performance Metrics](performance-metrics.md) for
collection details and [Agent Protocol](agent-protocol.md)
for reporting protocol details.
