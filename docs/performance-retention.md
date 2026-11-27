
# Historical Performance Retention

RackOS supports configurable automatic expiration of
historical performance measurements.

Retention provides age-based historical storage
management while preserving device registrations,
agent credentials, hardware inventory, and appliance
configuration.

Retention is disabled by default.

## Configuration

Performance retention is configured through appliance
environment variables.

| Variable | Default | Supported Values |
| --- | --- | --- |
| RACKOS_PERFORMANCE_RETENTION | 0s | 0s or 24h through 8760h |
| RACKOS_PERFORMANCE_CLEANUP_INTERVAL | 15m | 1m through 24h |
| RACKOS_PERFORMANCE_CLEANUP_BATCH_SIZE | 500 | 1 through 1000 |

Retention durations and cleanup intervals use Go
duration syntax.

Days and months are not supported duration units.

Examples:

| Retention Period | Configuration |
| --- | --- |
| Disabled | 0s |
| 1 day | 24h |
| 7 days | 168h |
| 14 days | 336h |
| 30 days | 720h |
| 90 days | 2160h |
| 365 days | 8760h |

A 30-day retention duration is recommended as an
initial configuration for typical development and
home-lab installations.

Actual storage requirements depend on device count,
reporting frequency, payload size, and database
overhead.

Invalid retention configuration prevents appliance
startup.

Cleanup interval and batch size are validated even
when automatic retention is disabled.

Configuration changes require an appliance restart.

## Enabling Retention

Example:

```bash
RACKOS_PERFORMANCE_RETENTION=720h \
RACKOS_PERFORMANCE_CLEANUP_INTERVAL=15m \
RACKOS_PERFORMANCE_CLEANUP_BATCH_SIZE=500 \
./bin/rackosd
```

This configuration enables automatic expiration of
performance samples received more than 30 days ago.

Retention applies to existing historical records as
well as newly received measurements.

The initial cleanup begins automatically when the
enabled appliance starts.

Back up historical data before enabling retention or
reducing an existing retention duration.

## Disabling Retention

```bash
RACKOS_PERFORMANCE_RETENTION=0s \
./bin/rackosd
```

Disabling retention prevents future automatic cleanup.

Previously deleted measurements are not restored.

Historical performance records continue accumulating
while retention is disabled.

Removing a registered device remains a separate
destructive administrative operation.

## Expiration Semantics

Expiration is based on the appliance receipt timestamp,
not the agent collection timestamp.

At the beginning of each cleanup cycle, the appliance
calculates an expiration cutoff:

    cutoff = appliance time - retention duration

The cutoff is normalized to UTC and truncated to
millisecond precision.

Records are eligible for deletion when:

    received_at < cutoff

The cutoff is exclusive.

| Receipt Timestamp | Behavior |
| --- | --- |
| Earlier than cutoff | Eligible for deletion |
| Equal to cutoff | Preserved |
| Later than cutoff | Preserved |

All batches within one cleanup cycle use the same
cutoff.

A new cutoff is calculated for each subsequent cycle.

### Delayed Measurements

Older collection timestamps do not automatically make
a performance sample eligible for expiration.

A delayed historical report receives a new appliance
receipt timestamp when successfully persisted.

Recently received historical measurements are therefore
preserved even when their original collection timestamps
are older than the retention duration.

### Clock Accuracy

Retention depends on the appliance system clock.

Invalid timestamps prevent cleanup for the affected
cycle.

A valid but incorrect appliance clock may cause
unexpected expiration or delayed cleanup.

Administrators should maintain accurate system time
before enabling automatic retention.

## Cleanup Architecture

The retention worker runs independently of agent
reporting and management API services.

Cleanup begins after successful initialization of
the appliance network listeners.

When retention is disabled, no background cleanup
worker is started.

The worker performs an initial cleanup immediately
when enabled.

Subsequent cleanup cycles begin after the configured
interval following completion of the previous cycle.

Cleanup cycles do not overlap within a worker.

## Bounded Deletion

Expired records are deleted using bounded SQLite
statements.

The existing received_at index identifies eligible
records.

Records are selected in ascending receipt timestamp
order, with SQLite row identity providing deterministic
ordering for identical receipt timestamps.

Each deletion batch commits independently.

The database connection is released between batches.

Internal worker defaults:

| Setting | Default |
| --- | --- |
| Maximum batches per cycle | 20 |
| Delay between full batches | 100 milliseconds |
| Timeout per batch | 5 seconds |

These internal limits are not currently configurable
through environment variables.

With the default batch size of 500 records, one
cleanup cycle can delete at most 10000 records.

A partial or empty batch terminates the current
cleanup cycle.

A full final batch indicates that additional expired
records may remain.

The next scheduled cycle continues cleanup using a
new expiration cutoff.

Retention is therefore an eventual expiration policy,
not a guarantee that every expired record is removed
at the exact moment it becomes eligible.

## Failure Handling

A failed deletion statement does not partially commit
its changes.

Previously successful batches remain committed.

A failed batch terminates the current cleanup cycle.

Temporary cleanup failures are logged and retried
during subsequent scheduled cycles.

Cleanup failures do not ordinarily interrupt agent
reporting or management API availability.

The worker uses context-aware database operations
with bounded batch deadlines.

Shutdown cancels active cleanup operations and pending
scheduler delays.

The appliance waits for the retention worker to exit
before closing its SQLite database connection.

## Data Preservation

Automatic retention operates only on historical
device_performance records.

The following records are not deleted by automatic
performance retention:

- Appliance installation identity.
- Registered devices.
- Agent credentials.
- Enrollment tokens.
- Latest hardware inventory snapshots.
- Appliance metadata.
- Certificate authority and TLS files.

A registered device may have no remaining performance
samples after cleanup.

Retention does not preserve the latest sample
indefinitely.

Removing a device remains independent of retention
and deletes its associated historical performance
records through foreign key cascading deletion.

## Reporting and Idempotency

Automatic retention does not modify the agent
reporting protocol.

Existing agents do not need to reenroll.

New reports remain accepted through the existing
authenticated performance reporting RPC.

Identical retries preserve the original receipt
timestamp while the original stored record exists.

Once a historical record has expired and been deleted,
its sample identity is no longer reserved.

A replay of a deleted sample may therefore be accepted
as a new historical record if it otherwise passes
authentication and validation.

The new record receives a new appliance receipt
timestamp.

RackOS does not maintain permanent deduplication
tombstones for expired samples.

The current agent pending report lifetime is
15 minutes, which is shorter than the minimum
supported retention duration of 24 hours.

## Management API Compatibility

Existing performance endpoints remain unchanged.

    GET /api/v1/devices/{id}/performance/latest

    GET /api/v1/devices/{id}/performance/history

Latest performance retrieval returns the most recently
collected remaining sample.

If a registered device has no remaining samples,
latest retrieval returns HTTP 404.

Historical retrieval returns HTTP 200 with an empty
samples array when no matching records remain.

Historical time-range filtering continues to use
collection timestamps.

Retention does not change the ordering or format of
historical responses.

### Historical Cursors

Historical pagination uses keyset continuation
positions.

The original record referenced by a cursor does not
need to remain stored.

Existing cursors remain usable after retention removes
their corresponding records, provided the original
query requirements are satisfied.

Pagination operates against live historical storage.

A subsequent page may contain fewer records than
expected if cleanup occurs between requests.

A previous response indicating additional records
does not guarantee those records will still exist
when the next page is requested.

## Storage Administration

Inspect historical performance storage:

```bash
./bin/rackosctl storage stats --data-dir ./data
```

Use the configured data directory:

```bash
RACKOS_DATA_DIR=/path/to/rackos/data \
./bin/rackosctl storage stats
```

Display command help:

```bash
./bin/rackosctl storage help
```

The data directory is selected in the following order:

1. Explicit --data-dir argument.
2. RACKOS_DATA_DIR environment variable.
3. Default ./data directory.

The command requires an existing appliance database
with the current schema version.

It opens the database using SQLite read-only mode
with query-only protection.

It does not initialize an appliance installation,
apply migrations, or initiate retention cleanup.

The command can inspect an active appliance database
when SQLite and filesystem access requirements are
satisfied.

Read-only WAL-mode access may require access to
existing WAL and SHM files or permission to create
the required auxiliary files.

## Logical Performance Statistics

The storage report includes:

| Measurement | Description |
| --- | --- |
| Historical samples | Total stored performance records |
| Reporting devices | Distinct devices with stored performance records |
| Payload size | Combined serialized protobuf payload bytes |
| Oldest collection | Earliest stored collection timestamp |
| Latest collection | Latest stored collection timestamp |
| Oldest receipt | Earliest stored appliance receipt timestamp |
| Latest receipt | Latest stored appliance receipt timestamp |

Empty historical storage reports zero samples,
zero reporting devices, zero payload bytes, and
missing timestamp ranges.

Reporting device counts do not represent currently
online devices or total registered devices.

Payload statistics include corrupt and unsupported
serialized payloads because those records still
consume storage.

Individual performance payloads are not decoded
when calculating statistics.

Timestamp ranges use indexed millisecond precision.

Statistics are calculated on demand rather than
maintained in separate aggregate tables.

Large historical datasets may require additional
query time.

## SQLite File Statistics

The storage report includes the lengths of:

- Main SQLite database file.
- Write-ahead log file.
- Shared-memory file.

Missing WAL and SHM files are reported as zero bytes.

File lengths do not necessarily equal filesystem
allocated block sizes.

The database files contain more than historical
performance payloads, including other appliance
records, indexes, metadata, and unused pages.

## SQLite Page Statistics

| Measurement | Description |
| --- | --- |
| Page size | Bytes per SQLite page |
| Total pages | Logical database page count |
| Reusable pages | Pages on the SQLite freelist |
| Allocated capacity | Total pages multiplied by page size |
| Reusable capacity | Reusable pages multiplied by page size |
| Non-freelist capacity | Allocated capacity minus reusable capacity |

Logical page statistics include committed database
state visible through SQLite, including WAL data.

Logical page capacity must not be added to database
and WAL file sizes to calculate total storage usage.

These measurements describe different aspects of the
same database.

Non-freelist capacity does not represent exact
application payload usage.

It also includes indexes, metadata, and unused space
within active pages.

Logical and filesystem statistics are collected
separately and may differ during concurrent writes.

## Storage Reclamation

Deleting historical measurements reduces the number
of stored samples and their combined payload size.

SQLite may make released pages available for future
database writes rather than immediately reducing
the database file size.

Reusable page capacity may increase after retention.

A successful cleanup cycle does not guarantee that
the database or WAL file becomes smaller.

The retention worker does not initiate:

- VACUUM operations.
- WAL checkpoint operations.
- Journal truncation.
- Automatic vacuum mode changes.
- Database compaction.

SQLite's existing automatic journal management
continues to operate normally.

Automatic physical disk space reclamation is not
currently implemented.

## Logging

When enabled, the appliance logs retention worker
startup and shutdown.

Successful cleanup cycles report:

- Expiration cutoff.
- Number of deleted samples.
- Number of completed batches.
- Whether the cycle batch limit was reached.

Example:

```text
INFO Performance retention cleanup completed
cutoff=2026-10-28T12:00:00Z
deleted_samples=1250
completed_batches=3
batch_limit_reached=false
```

Empty cleanup cycles are logged at debug level.

Failed cleanup cycles are logged at warning level
and include completed deletion counts and error
information.

Raw historical performance payloads and agent
credentials are not included in normal retention logs.

## Upgrading Existing Installations

Automatic retention reuses the received_at index
introduced by database migration 006.

No additional database migration is required.

Existing historical measurements remain stored when
upgrading with retention disabled.

Enabling retention on an existing installation may
immediately delete previously accumulated historical
measurements that exceed the configured receipt age.

Back up the appliance database before enabling
retention on installations with important historical
data.

## Testing

Retention coverage includes:

- Policy and configuration validation.
- Inclusive and exclusive timestamp boundaries.
- Receipt-based expiration.
- Cross-device deletion.
- Bounded deletion batches.
- Indexed deletion queries.
- Transaction rollback and commit behavior.
- Failure injection and recovery.
- Corrupt and unsupported payload expiration.
- Concurrent performance ingestion.
- Preservation of unrelated appliance records.
- Historical cursor compatibility.
- Expired sample replay behavior.
- Logical performance storage statistics.
- SQLite physical storage inspection.
- Read-only database access.
- Worker scheduling and cancellation.
- Daemon startup and shutdown integration.
- Administrative storage CLI.
- SQLite-backed retention integration.
- Historical persistence across database restarts.

Run the retention tests:

```bash
go test -race -count=1 ./internal/appliance/retention/
```

Run the repository retention and statistics tests:

```bash
go test -race -count=1 \
  -run '^TestPerformance(Retention|Statistics)' \
  ./internal/appliance/performance/
```

Run the daemon retention tests:

```bash
go test -race -count=1 \
  -run '^TestDaemonRetention' \
  ./cmd/rackosd/
```

Run the retention integration test:

```bash
go test -race -count=1 -v \
  -run '^TestPerformanceRetentionIntegration$' \
  ./internal/appliance/performance/
```

Run the storage CLI tests:

```bash
go test -race -count=1 \
  -run '^Test(StorageCLI|FormatStorage)' \
  ./cmd/rackosctl/
```

## Current Limitations

- Retention is disabled by default.
- Retention duration is global across all devices.
- No per-device retention configuration.
- No per-metric retention configuration.
- No historical aggregation or downsampling.
- No strict database size quota.
- No automatic disk space exhaustion protection.
- No automatic physical database compaction.
- No guaranteed indefinite preservation of latest samples.
- No permanent deduplication history for expired samples.
- No runtime retention configuration API.
- No manual cleanup CLI command.
- No historical storage statistics HTTP endpoint.
- No machine-readable storage CLI output format.
- No persistent retention execution history.
- No management console retention configuration.

Age-based retention does not replace storage monitoring
or database backups.

The current implementation is intended for pre-alpha
development and testing.

## Related Documentation

- [Historical Performance Storage](performance-storage.md)
- [Performance Metrics](performance-metrics.md)
- [Management API](management-api.md)
- [Agent Protocol](agent-protocol.md)
- [Device Administration](device-administration.md)
