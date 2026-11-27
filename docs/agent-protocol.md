
## Hardware Inventory Reporting

The ReportInventory RPC allows authenticated agents to
submit hardware inventory snapshots to the appliance.

### Request

ReportInventoryRequest contains:

- device_id: Registered device identity.
- inventory: HardwareInventory snapshot.

The existing agent bearer credential must be supplied
through gRPC authorization metadata.

Reports are transmitted over verified TLS connections.

### Inventory Fields

| Field | Type | Required |
| --- | --- | --- |
| hostname | string | Yes |
| os | string | Yes |
| architecture | string | Yes |
| logical_cpus | uint32 | Yes |
| host_logical_cpus | uint32 | No |
| physical_cores | uint32 | No |
| cpu_model | string | No |
| cpu_manufacturer | string | No |
| memory_bytes | uint64 | No |
| hardware_model | string | No |
| os_version | string | No |
| kernel_version | string | No |
| boot_time_unix_millis | int64 | No |
| collected_at_unix_millis | int64 | Yes |

Unavailable optional fields use their Protocol Buffers
default values.

Timestamps are represented as Unix milliseconds.

### Validation

The appliance authenticates the reporting device before
validating the inventory payload.

Validation enforces:

- Required inventory fields.
- Valid UTF-8 text.
- Maximum string lengths.
- No control characters.
- No leading or trailing whitespace.
- Positive and bounded logical CPU counts.
- Valid CPU topology.
- Signed 64-bit memory capacity compatibility.
- Positive collection timestamps.
- Maximum future clock skew of five minutes.
- Boot timestamps that do not follow collection timestamps.

Authentication and validation do not establish the
accuracy of hardware information supplied by an agent.


### Response

ReportInventoryResponse contains an accepted boolean.

On appliances supporting persistent inventory storage,
an accepted response indicates that the inventory
transaction has successfully committed or that an
identical previously stored snapshot was recognized.

Older validation-only appliance implementations may
acknowledge reports without persisting them.

Older snapshots are rejected to prevent overwriting
newer hardware information.



### Status Codes

| Status | Description |
| --- | --- |
| OK | Inventory committed or identical retry accepted |
| Unauthenticated | Invalid or missing agent credential |
| InvalidArgument | Invalid inventory snapshot |
| FailedPrecondition | Snapshot older than stored inventory |
| AlreadyExists | Conflicting snapshot collection timestamp |
| Internal | Internal appliance or database failure |
| Canceled | Request canceled |
| DeadlineExceeded | Request deadline exceeded |
| Unimplemented | Inventory reporting not supported |


### Security

- Existing agent credentials are reused.
- Enrollment, heartbeat, and inventory RPCs share
  bearer credential extraction logic.
- Cross-device credential usage is rejected.
- Revoked and removed devices cannot authenticate.
- Invalid field contents are not echoed in validation errors.
- Inventory reporting does not update heartbeat timestamps.
- The reporting handler does not log credentials or inventory.

### Compatibility

Inventory reporting is an additive extension to the
version 1 agent protocol.

Existing enrollment and heartbeat operations remain
unchanged.

Older agents can operate without inventory reporting.


### Current Limitations

- Only the latest inventory snapshot is retained.
- No inventory retrieval REST API.
- No historical inventory storage.
- Reporting intervals are not configurable.
- Retry jitter is not implemented.

Agents automatically collect inventory at startup and
refresh it every six hours after successful reporting.

See inventory-storage.md for persistence and scheduling
details.


## Performance Reporting

The ReportPerformance RPC allows authenticated agents
to submit historical performance snapshots to the
appliance.

Performance reporting is an additive extension to
version 1 of the agent protocol.

### Request

ReportPerformanceRequest contains:

| Field | Type | Required |
| --- | --- | --- |
| device_id | string | Yes |
| sample_id | string | Yes |
| snapshot | PerformanceSnapshot | Yes |

The device_id identifies the registered agent.

The sample_id contains a cryptographically random
128-bit identity encoded as 32 lowercase hexadecimal
characters.

The same sample identity must be retained throughout
all retries of an existing performance report.

### Performance Snapshot

PerformanceSnapshot contains:

| Field | Type | Required |
| --- | --- | --- |
| collected_at | google.protobuf.Timestamp | Yes |
| cpu | CPUPerformance | Yes |
| memory | MemoryPerformance | Yes |
| storage | StoragePerformance | Yes |
| network | NetworkPerformance | Yes |

CPU measurements include aggregate utilization and
the actual sampling interval.

Memory measurements include total, available, and
used bytes, together with utilization.

Storage measurements include filesystem capacity,
cumulative disk activity, and calculated disk rates.

Network measurements include interface metadata,
cumulative activity counters, and calculated
interface rates.

Optional numeric fields preserve the distinction
between an unavailable value and an explicit zero.

See api/proto/rackos/v1/agent.proto for the complete
message definitions.

### Authentication

Reports use the existing agent bearer credential
through gRPC authorization metadata.

    authorization: Bearer AGENT_TOKEN

Reports are transmitted over verified TLS connections.

The appliance authenticates the reporting device
before validating the performance payload.

Cross-device credential usage is rejected.

### Validation

Performance validation enforces:

- Required report and snapshot fields.
- Valid device and sample identities.
- Valid collection timestamps.
- Maximum future clock skew of five minutes.
- Finite CPU and memory utilization percentages.
- Consistent memory capacity accounting.
- Consistent filesystem capacity accounting.
- Valid measurement sampling intervals.
- Bounded collection sizes.
- Valid UTF-8 measurement identifiers.
- Unique disk counter identities.
- Unique network interface names and indexes.
- Valid disk and network rate associations.
- Finite, nonnegative activity rates.
- Valid optional link speed measurements.

Sampling intervals must be between 1 and 60000
milliseconds.

Each filesystem, disk, disk rate, network interface,
and interface rate collection supports at most 256
entries.

Measurement identifiers support a maximum encoded
length of 1024 bytes.

Older collection timestamps are permitted.

A report may contain no eligible filesystem, disk,
or network interface records.

The maximum serialized performance reporting request
size is 512 KiB.

The gRPC transport also enforces its existing
1 MiB message size limit.

### Response

ReportPerformanceResponse contains:

| Field | Type | Description |
| --- | --- | --- |
| accepted | bool | Successful report acceptance |
| sample_id | string | Acknowledged sample identity |
| received_at | google.protobuf.Timestamp | Original appliance receipt timestamp |

An accepted response indicates that the performance
transaction has committed or that an identical
previously committed sample was recognized.

The response sample identity must match the request.

Receipt timestamps originate from the appliance
clock and use millisecond precision.

Identical retries return the original receipt
timestamp without modifying the stored record.

The agent validates acceptance, sample identity,
and receipt timestamp before clearing a pending report.

An invalid acknowledgement does not establish whether
the appliance persisted the report.

### Status Codes

| Status | Description |
| --- | --- |
| OK | Sample committed or identical retry accepted |
| Unauthenticated | Invalid or missing agent credential |
| InvalidArgument | Invalid performance report |
| ResourceExhausted | Reporting or transport resource limit exceeded |
| AlreadyExists | Sample identity conflicts with stored content |
| Internal | Appliance or persistence failure |
| Canceled | Request canceled |
| DeadlineExceeded | Request deadline exceeded |
| Unimplemented | Performance reporting not supported |

### Persistence Semantics

Performance samples are stored independently using
the device identity and sample identity.

Collection timestamps are not unique record
identifiers.

Multiple samples may share a collection timestamp.

Out-of-order historical samples are accepted when
otherwise valid.

Existing historical samples are not overwritten by
new reports.


### Historical Retention

The appliance may automatically delete historical
performance samples according to its configured
retention policy.

Expiration is based on appliance receipt timestamps
rather than agent collection timestamps.

Automatic retention does not modify the reporting
RPC contract or require agent reenrollment.

Identical retries preserve the original receipt
timestamp while the original sample remains stored.

If the original sample has been deleted, a subsequent
report with the same sample identity may be accepted
as a new historical record when authentication and
validation succeed.

The newly accepted record receives a new appliance
receipt timestamp.

Successful acknowledgement establishes persistence
at the time of acknowledgement, not indefinite
historical availability.

The appliance does not maintain permanent
deduplication records for expired samples.

See [Historical Performance Retention](performance-retention.md)
for expiration policy and storage management.


### Automatic Reporting

Agents collect performance measurements at startup.

After successful acknowledgement, the performance
worker waits 60 seconds before collecting again.

Temporary reporting failures preserve the pending
report and retry using exponential backoff.

Retry intervals begin at five seconds and are capped
at 60 seconds.

Pending reports expire after 15 minutes.

Only one pending performance report is retained.

Authentication failures terminate authenticated
agent operation.

Unsupported performance reporting does not prevent
heartbeat or hardware inventory monitoring.

### Compatibility

Existing enrollment, heartbeat, and inventory RPCs
remain unchanged.

Older agents can operate without performance reporting.

Newer agents disable performance reporting when an
older appliance returns Unimplemented.

Performance reporting does not require reenrollment.

### Security

- Existing agent credentials are reused.
- Authentication and persistence are transactional.
- Successful acknowledgement follows transaction commit.
- Cross-device reporting is rejected.
- Revoked credentials cannot report historical samples.
- Performance reporting does not update heartbeat timestamps.
- Raw performance payloads and credentials are not logged.
- Client-facing errors do not expose database details.

Authentication and validation do not establish the
accuracy of performance information supplied by
an agent.

See performance-storage.md for historical persistence,
retry behavior, and current limitations.


### Historical Performance Access

Successfully acknowledged performance samples
are available through the appliance's read-only
Management API while the samples remain stored.

The Management API supports:

- Latest collected performance snapshot retrieval.
- Historical time-range filtering.
- Cursor-based historical pagination.
- Precision-preserving JSON serialization.

Management API access is separate from the
authenticated agent gRPC reporting protocol.

Agents do not require Management API access
to submit performance measurements.

Management API retrieval does not modify agent
credentials, device heartbeat timestamps, or
stored performance snapshots.

See [management-api.md](management-api.md) for
endpoint definitions and response formats.