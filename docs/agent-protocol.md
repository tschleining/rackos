
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