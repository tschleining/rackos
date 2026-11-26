
# Hardware Inventory

The RackOS agent provides cross-platform system and
hardware inventory collection.

Inventory collection is independent of agent enrollment,
authentication, and appliance connectivity.

## Usage

Collect a local inventory snapshot:

```bash
rackos-agent inventory
```

Display command help:

```bash
rackos-agent inventory --help
```

Inventory is printed to standard output as formatted JSON.

The command does not modify agent state or transmit
information to the appliance.

## Platform Support

| Platform | Collection method |
| --- | --- |
| macOS | Native sysctl and sw_vers utilities |
| Linux | procfs and sysfs |
| Other | Portable Go runtime information |

The portable fallback does not imply official support
for running the complete agent on additional platforms.

## Inventory Fields

| Field | Description |
| --- | --- |
| hostname | System hostname |
| os | Operating system family |
| architecture | Agent architecture |
| logical_cpus | Logical CPUs available to the process at startup |
| host_logical_cpus | System-reported logical CPUs |
| physical_cores | System-reported physical processor cores |
| cpu_model | Processor model |
| cpu_manufacturer | Processor manufacturer |
| memory_bytes | Total system memory in bytes |
| hardware_model | System hardware model |
| os_version | Operating system version |
| kernel_version | Running kernel release |
| boot_time | System boot timestamp |
| collected_at | Inventory collection timestamp |

Unavailable optional fields are omitted from JSON output.

Timestamps are represented in UTC using RFC 3339
with optional fractional seconds.

## Example Output

```json
{
  "hostname": "workstation.local",
  "os": "darwin",
  "architecture": "arm64",
  "logical_cpus": 14,
  "host_logical_cpus": 14,
  "physical_cores": 14,
  "cpu_model": "Apple M4 Pro",
  "cpu_manufacturer": "Apple",
  "memory_bytes": 25769803776,
  "hardware_model": "Mac16,8",
  "os_version": "26.6.2",
  "kernel_version": "25.6.0",
  "boot_time": "2026-08-31T06:50:03Z",
  "collected_at": "2026-11-26T20:39:39Z"
}
```

## macOS Collection

Hardware information is collected using native macOS
system utilities with absolute executable paths.

No elevated privileges are required.

CPU branding may be unavailable on certain hardware
or virtualized configurations.

## Linux Collection

Linux inventory is collected from procfs and sysfs.

CPU topology is determined using online logical CPUs
and their core sibling groups.

Physical core counts may be unavailable when the kernel
does not expose sufficient topology information.

Memory capacity is derived from MemTotal, which represents
usable physical memory rather than exact installed RAM.

The operating system version is retrieved from os-release.

## Security and Privacy

- No elevated privileges are required.
- No third-party monitoring dependencies are required.
- Collection does not require network access.
- Hardware serial numbers are not collected.
- Hardware UUIDs are not collected.
- Agent credentials are not included in inventory output.
- Collection respects context cancellation and deadlines.

Inventory contains system-identifying information such as
hostnames and hardware models and should be handled accordingly.

## Error Handling

Native collection failures return an error.

Unavailable optional hardware properties are omitted
rather than populated with guessed values.

Native collectors use a five-second context deadline.
The inventory CLI command uses a ten-second deadline.

Individual Linux filesystem reads are not interruptible
through context cancellation.

## Testing

The inventory implementation includes:

- Portable collector tests.
- JSON serialization tests.
- Context cancellation tests.
- Concurrent collection tests.
- Native macOS integration tests.
- Linux system information parsing tests.
- Linux native integration tests.
- Agent CLI regression tests.

## Current Limitations

- No inventory management API.
- No storage device inventory.
- No network interface inventory.
- No Windows-specific hardware collection.
- No performance telemetry.

Virtualized and containerized environments may expose
hardware information that differs from the physical host.

## Automatic Reporting

The agent automatically reports hardware inventory at
startup and refreshes it every six hours after a
successful report.

The appliance persists the latest snapshot for each
registered device.

The standalone inventory command remains local-only.

See [Persistent Hardware Inventory](inventory-storage.md)
for storage and reporting details.