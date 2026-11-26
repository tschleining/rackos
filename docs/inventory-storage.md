
# Persistent Hardware Inventory

RackOS stores the latest hardware inventory snapshot
for each registered device.

Inventory is collected automatically by the agent
and transmitted using the authenticated ReportInventory
RPC over a verified TLS connection.

## Architecture

The inventory reporting pipeline consists of:

1. Native hardware collection.
2. Protocol Buffers conversion.
3. Authenticated gRPC transmission.
4. Appliance-side validation.
5. Transactional SQLite persistence.

## Database Schema

Migration 005 introduces the device_inventory table.

Each registered device may have one inventory record.

| Column | Type |
| --- | --- |
| device_id | TEXT |
| hostname | TEXT |
| os | TEXT |
| architecture | TEXT |
| logical_cpus | INTEGER |
| host_logical_cpus | INTEGER |
| physical_cores | INTEGER |
| cpu_model | TEXT |
| cpu_manufacturer | TEXT |
| memory_bytes | INTEGER |
| hardware_model | TEXT |
| os_version | TEXT |
| kernel_version | TEXT |
| boot_time | INTEGER |
| collected_at | INTEGER |
| received_at | INTEGER |

The device_id column is both the primary key and a
foreign key referencing devices.id.

Deleting a device automatically deletes its inventory.

Unknown optional numeric values are stored as zero.
Unknown optional text values are stored as empty strings.

Timestamps are stored as Unix milliseconds.

## Snapshot Ordering

The appliance retains only the latest inventory
snapshot for each device.

| Incoming snapshot | Behavior |
| --- | --- |
| First snapshot | Insert |
| Newer snapshot | Replace |
| Older snapshot | Reject |
| Identical retry | Accept without modification |
| Same timestamp with different contents | Reject |

Identical retries preserve the original received_at
timestamp.

Snapshot ordering is enforced atomically by SQLite.

## Transactional Persistence

Device authentication and inventory persistence occur
within the same database transaction.

The appliance acknowledges a report only after the
transaction successfully commits.

Failed transactions preserve previously stored inventory.

Inventory reporting does not modify device heartbeat
timestamps.

## Automatic Reporting

The agent collects hardware inventory at startup
and refreshes it every six hours after a successful
report.

Inventory reporting runs independently of heartbeat
monitoring.

Temporary reporting failures preserve the pending
snapshot and retry using exponential backoff.

Retry delays begin at five seconds and are capped
at five minutes.

A successful report resets the retry delay.

Invalid, stale, or conflicting snapshots are discarded
and recollected after five minutes.

Authentication failures terminate the agent.

An Unimplemented response disables inventory reporting
for the current agent session while allowing heartbeat
monitoring to continue.

## Security

- Existing device credentials are reused.
- Authentication and persistence are transactional.
- Inventory is validated before storage.
- SQL queries use parameterized values.
- Cross-device authentication is rejected.
- Credentials are not included in inventory records.
- Hardware serial numbers and UUIDs are not collected.
- Inventory receipt timestamps use the appliance clock.
- Raw inventory and credentials are not logged by
  the inventory reporting scheduler.

## Upgrading Existing Installations

Migration 005 is applied automatically when the
appliance opens its database.

Existing installation identity, devices, enrollment
tokens, and agent credentials are preserved.

Existing agents do not need to enroll again.

Devices without inventory remain valid registered devices.

## Inspecting Stored Inventory

For local development and troubleshooting:

```bash
sqlite3 -readonly data/rackos.db \
  "SELECT * FROM device_inventory;"
```

Direct database inspection is not intended to replace
the future inventory management API.

## Testing

Inventory storage is covered by:

- Database migration tests.
- Schema constraint tests.
- Repository insertion and retrieval tests.
- Stale snapshot and conflict tests.
- Concurrent write tests.
- Transaction commit and rollback tests.
- Authenticated TLS integration tests.
- Database failure injection tests.
- Cascading deletion tests.
- Reporting scheduler tests.

## Current Limitations

- Only the latest hardware snapshot is retained.
- Inventory refresh intervals are not configurable.
- Retry jitter is not implemented.
- No inventory REST API or management dashboard.
- No historical hardware inventory.
- Significant backwards clock adjustments can cause
  temporary stale snapshot rejection.
- Extended connectivity failures can leave an older
  snapshot pending until reporting succeeds.
- Older appliances with validation-only reporting
  may acknowledge inventory without persisting it.
