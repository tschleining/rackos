
# Performance Metrics

RackOS provides native system-wide CPU, memory,
filesystem capacity, and disk activity monitoring
on macOS and Linux.

Performance metrics are collected independently of
hardware inventory and agent heartbeat operations.

## Supported Platforms

| Platform | Architecture | CPU | Memory | Filesystems | Disk I/O |
| --- | --- | --- | --- | --- | --- |
| macOS | ARM64 | Yes | Yes | Yes | Yes |
| macOS | AMD64 | Yes | Yes | Yes | Yes |
| Linux | ARM64 | Yes | Yes | Yes | Yes |
| Linux | AMD64 | Yes | Yes | Yes | Yes |

Other platforms return an explicit unsupported
platform error.

Android and iOS are not supported targets.

Linux filesystem collection requires kernel support
for STATX_MNT_ID, introduced in Linux 5.8.

Cross-compilation does not guarantee runtime support
on an untested platform.

## Architecture

The performance metrics implementation is located in:

    internal/agent/metrics/

The package provides:

- Portable performance snapshot models.
- CPU and memory utilization calculations.
- Mounted filesystem discovery.
- Filesystem capacity accounting.
- Cumulative disk activity collection.
- Disk throughput and operation rate calculations.
- Platform-specific native collectors.
- Context-aware collection orchestration.
- Unsupported platform handling.
- Unit tests and native integration tests.

The public collection entry point is:

    metrics.Collect(ctx)

Collection returns a complete performance snapshot
or an error.

Partial snapshots are not returned.

## Collection Lifecycle

A performance snapshot is collected in the
following order:

1. Read initial disk activity counters.
2. Record the disk sampling start time.
3. Read initial CPU counters.
4. Wait for the default CPU sampling interval.
5. Read final CPU counters.
6. Calculate CPU utilization.
7. Collect memory statistics.
8. Read final disk activity counters.
9. Calculate disk throughput and operation rates.
10. Collect mounted filesystem capacity.
11. Record the UTC collection completion timestamp.

CPU and disk collection share a single sampling wait.

CPU and disk sampling durations are measured
independently and may differ.

Filesystem capacity is collected after disk sampling
and is not included in the disk rate interval.

The default CPU sampling interval is one second.

## CPU Utilization

CPU utilization is calculated from two cumulative
counter readings.

    busy_delta / (busy_delta + idle_delta) * 100

CPU utilization represents aggregate activity across
all logical processors and ranges from zero to 100
percent.

The actual sampling interval is reported in
milliseconds.

### Linux CPU Collection

CPU statistics are read from:

    /proc/stat

The aggregate CPU line is used.

Busy time includes:

- User execution.
- Nice execution.
- System execution.
- Hardware interrupts.
- Software interrupts.
- Hypervisor steal time.

Idle time includes idle processor time and I/O wait.

Guest execution counters are not added separately
because they are already included in user and nice
execution counters.

Hypervisor steal time represents unavailable CPU
capacity rather than active guest execution.

### macOS CPU Collection

CPU counters are collected using the Mach
host_statistics function with HOST_CPU_LOAD_INFO.

Busy time includes user, system, and nice execution.

Idle time includes the native idle counter.

Native functions are accessed through PureGo without
requiring CGO or external monitoring commands.

Native library and host references are reused for
the lifetime of the process.

### CPU Counter Validation

CPU samples are rejected when:

- Aggregate counters decrease.
- No counter activity has elapsed between readings.
- Counter addition overflows.
- The sampling interval is invalid.
- Native statistics are malformed.

Native counter rollover and CPU topology changes
may invalidate individual samples.

## Memory Utilization

Memory measurements include:

- Total physical or usable memory capacity.
- Estimated available memory.
- Derived used memory.
- Memory utilization percentage.

All memory capacity measurements are expressed
in bytes.

Used memory is calculated as:

    total_bytes - available_bytes

Memory utilization is calculated as:

    used_bytes / total_bytes * 100

### Linux Memory Collection

Memory statistics are read from:

    /proc/meminfo

The collector uses MemTotal and MemAvailable.

MemAvailable estimates memory that can be allocated
without swapping and accounts for reclaimable memory.

MemFree is not used as a substitute for MemAvailable.

Values reported in kB are converted to bytes using
a multiplier of 1024.

### macOS Memory Collection

Physical memory capacity is obtained using hw.memsize.

Virtual memory statistics are collected using
host_statistics64 with HOST_VM_INFO64.

The native page size is obtained using host_page_size.

Available memory is estimated as:

    (free_pages + inactive_pages) * page_size

The native free-page counter already includes
speculative pages.

Speculative pages are not added separately.

Physical memory capacity and page size are cached
for the lifetime of the process.

Available memory is an estimate and does not
necessarily match the Memory Used measurement
displayed in Activity Monitor.

### Memory Validation

Memory measurements are rejected when:

- Total memory capacity is zero.
- Available memory exceeds total capacity.
- Required statistics are missing or malformed.
- Required Linux statistics appear more than once.
- Native page size is invalid.
- Byte conversion overflows.

Zero available memory is a valid measurement.

## Storage Monitoring

Storage monitoring consists of three independent
measurement collections:

- Mounted filesystem capacity.
- Cumulative disk activity counters.
- Disk throughput and operation rates.

Filesystem capacity and device-level disk activity
are maintained separately.

Multiple filesystems can share an underlying storage
device or storage pool.

## Filesystem Capacity

Each filesystem record includes:

- Filesystem source.
- Mount point.
- Filesystem type.
- Read-only status.
- Capacity measurements.

Capacity measurements are expressed in bytes.

| Field | Description |
| --- | --- |
| total_bytes | Total reported filesystem capacity |
| free_bytes | All reported unused capacity |
| available_bytes | Capacity available to ordinary users |
| used_bytes | Total capacity minus free capacity |
| reserved_bytes | Free capacity unavailable to ordinary users |
| usage_percent | Used capacity as a percentage of total |

Used capacity is calculated as:

    total_bytes - free_bytes

Reserved capacity is calculated as:

    free_bytes - available_bytes

Utilization is calculated as:

    used_bytes / total_bytes * 100

Reserved but unused space is not classified as
used capacity.

Reserved bytes represent the difference between
free and available capacity. This difference does
not necessarily correspond to an explicit filesystem
reservation.

RackOS utilization percentages may differ from df
because df may calculate utilization relative to
used capacity plus user-available capacity.

### Linux Filesystem Discovery

Mounted filesystems are discovered through:

    /proc/self/mountinfo

The parser preserves:

- Mount identifiers.
- Parent mount identifiers.
- Major and minor device numbers.
- Filesystem roots.
- Mount points.
- Filesystem sources.
- Filesystem types.
- Read-only status.

Escaped mount paths and sources are decoded.

An explicit filesystem allowlist limits collection
to supported storage filesystem types.

Supported types include common Linux filesystems
such as ext4, XFS, Btrfs, ZFS, Bcachefs, F2FS,
and common removable-media filesystem formats.

OverlayFS is included only when mounted at the
root filesystem.

Remote filesystems, temporary filesystems, kernel
pseudo-filesystems, and unknown filesystem types
are excluded.

Discovery reflects the current mount namespace.

### Linux Filesystem Collection

Filesystem capacity is collected using native
Linux system calls:

- Open with O_PATH.
- Statx with STATX_MNT_ID.
- Fstatfs.

The opened filesystem's mount identifier is checked
against the identifier discovered through mountinfo.

Identity verification and capacity collection use
the same file descriptor.

Filesystem fragment size is preferred for block
conversion. Block size is used as a fallback when
fragment size is unavailable.

Disappeared and overmounted filesystems are skipped.

Unexpected native errors invalidate the collection.

### macOS Filesystem Discovery

Mounted filesystems are discovered using getfsstat
with MNT_NOWAIT.

Cached kernel information is used during discovery.

Only supported local filesystem types are included.

Supported types include APFS, HFS, FAT, exFAT,
NTFS, and supported local optical-media and ZFS
filesystems.

Remote and unsupported filesystem types are excluded.

Native filesystem identifiers are retained for
subsequent identity verification.

Mount table collection uses bounded allocation
and retry behavior to handle changing mount counts.

### macOS Filesystem Collection

Fresh filesystem statistics are retrieved using
the native statfs interface.

The returned filesystem identifier, source,
mount point, and filesystem type are compared
against discovery metadata.

Read-only status is refreshed during collection.

Native filesystem block counters are converted
into bytes using the reported block size.

Disappeared or changed filesystems are skipped.

Unexpected native errors invalidate the collection.

### APFS Shared Capacity

APFS volumes may share capacity within the same
underlying storage container.

RackOS preserves individual mounted filesystem
records, including system and data volumes.

Reported filesystem capacities must not be summed
to calculate physical storage capacity.

Filesystem utilization does not necessarily
represent physical SSD utilization or exclusive
space consumed by an individual APFS volume.

The current implementation does not model APFS
containers or physical-to-logical storage topology.

### Filesystem Capacity Validation

Filesystem measurements are rejected when:

- Block size is invalid.
- Total capacity is zero.
- Free capacity exceeds total capacity.
- Available capacity exceeds free capacity.
- Block-to-byte conversion overflows.

## Cumulative Disk Activity

Each eligible disk reports the following counters:

| Field | Description |
| --- | --- |
| device | Operating system device name |
| read_bytes | Cumulative bytes read |
| write_bytes | Cumulative bytes written |
| read_operations | Cumulative completed read operations |
| write_operations | Cumulative completed write operations |

Disk activity counters represent native cumulative
measurements.

Counters may reset following a reboot, device
reconnection, or device reinitialization.

They are not persistent lifetime hardware counters.

### Linux Disk Collection

Whole block devices are discovered through:

    /sys/block

Native device paths are resolved to exclude
software-defined devices under:

    /sys/devices/virtual

Disk statistics are collected from:

    /proc/diskstats

Major and minor device numbers are validated
against the discovered device information.

Partitions are excluded by whole-device discovery.

Virtual block devices such as loopback, RAM,
device mapper, and software RAID devices are
excluded by the virtual-device filtering policy.

Linux disk accounting sectors represent 512 bytes,
regardless of the physical or logical sector size
of the underlying storage device.

Sector counters are converted into bytes with
unsigned integer overflow protection.

Discard and flush statistics are not collected.

### macOS Disk Collection

Disk activity is collected using native IOKit
and Core Foundation interfaces through PureGo.

The collector enumerates IOMedia objects and
selects whole media with an immediate parent
conforming to IOBlockStorageDriver.

The native Statistics dictionary provides:

- Bytes (Read).
- Bytes (Write).
- Operations (Read).
- Operations (Write).

Byte counters require no sector conversion.

Core Foundation values are type-checked before
integer conversion.

Negative, floating-point, and unrepresentable
counter values are rejected.

Native framework initialization is cached.

Owned IOKit and Core Foundation references are
released after use.

Ordinary partitions and unrelated logical media
are excluded.

Some virtual storage devices may still qualify
under the whole-media filtering policy.

Duplicate storage driver identities invalidate
the collection to avoid counting a driver twice.

## Disk Throughput and Operation Rates

Disk rates are calculated using two consecutive
cumulative counter readings.

Each rate record contains:

| Field | Unit |
| --- | --- |
| device | Operating system device name |
| read_bytes_per_second | Bytes per second |
| write_bytes_per_second | Bytes per second |
| read_operations_per_second | Operations per second |
| write_operations_per_second | Operations per second |
| sample_interval_millis | Milliseconds |

Rates are calculated as:

    counter_delta / elapsed_seconds

Counter subtraction is performed before converting
the result to floating-point values.

The full measured elapsed duration is used in
rate calculations.

Sampling duration is reported in milliseconds.

Fractional throughput and operation rates
are supported.

Sampling intervals shorter than one millisecond
are rejected.

### Counter Reset Handling

Consecutive readings are matched by device name.

- Unchanged counters produce zero rates.
- Increased counters produce calculated rates.
- Decreased counters invalidate the device rate.
- Newly discovered devices require a baseline.
- Disconnected devices do not produce rates.
- Duplicate device identifiers invalidate the sample.

A counter regression on one disk does not invalidate
valid rates calculated for other disks.

Missing rates are omitted rather than reported as
zero activity.

A subsequent valid pair of readings can establish
a new rate following a counter reset.

Device names are not permanent hardware identities.

A replacement device reusing the same name may
produce an incorrect rate if its counters do not
decrease.

## Snapshot Format

A complete performance snapshot contains:

- CPU utilization.
- Memory utilization.
- Filesystem capacity measurements.
- Cumulative disk activity counters.
- Disk throughput and operation rates.
- A UTC collection completion timestamp.

Example:

```json
{
  "cpu": {
    "usage_percent": 25,
    "sample_interval_millis": 1000
  },
  "memory": {
    "total_bytes": 17179869184,
    "available_bytes": 6442450944,
    "used_bytes": 10737418240,
    "usage_percent": 62.5
  },
  "storage": {
    "filesystems": [
      {
        "source": "/dev/sda1",
        "mount_point": "/",
        "filesystem_type": "ext4",
        "read_only": false,
        "capacity": {
          "total_bytes": 1000000000000,
          "free_bytes": 350000000000,
          "available_bytes": 300000000000,
          "used_bytes": 650000000000,
          "reserved_bytes": 50000000000,
          "usage_percent": 65
        }
      }
    ],
    "disks": [
      {
        "device": "sda",
        "read_bytes": 4096000,
        "write_bytes": 2048000,
        "read_operations": 1000,
        "write_operations": 500
      }
    ],
    "disk_rates": [
      {
        "device": "sda",
        "read_bytes_per_second": 1024000,
        "write_bytes_per_second": 512000,
        "read_operations_per_second": 250,
        "write_operations_per_second": 125,
        "sample_interval_millis": 1000
      }
    ]
  },
  "collected_at": "2026-11-26T23:00:00Z"
}
```

The values above are illustrative.

Successful unified snapshots include a storage object.

Empty storage collections are serialized as arrays
rather than null values.

```json
{
  "filesystems": [],
  "disks": [],
  "disk_rates": []
}
```

An empty collection indicates that no eligible
measurements were returned.

An absent disk rate must not be interpreted as
zero disk activity.

CPU utilization and disk rates represent activity
over sampling intervals.

Memory and filesystem capacity represent
point-in-time observations.

The collection timestamp represents completion
of the unified snapshot.

Unsigned cumulative counters are represented as
JSON integers. Consumers must preserve integer
precision when processing large counter values.

## Command-Line Interface

Collect a single local performance snapshot:

```bash
rackos-agent metrics
```

Display command help:

```bash
rackos-agent metrics --help
```

The command:

- Produces formatted JSON on standard output.
- Uses a ten-second collection deadline.
- Requires no enrollment or appliance connection.
- Does not read or modify persistent agent state.
- Does not transmit measurements.
- Rejects unexpected command arguments.
- Returns an error if collection fails.

Normal collection does not require elevated
privileges.

## Cancellation and Error Handling

Collection respects caller context cancellation
and deadlines.

The CPU sampling timer supports cancellation.

Native calls and filesystem reads cannot necessarily
be interrupted while already in progress.

Underlying error causes are preserved.

Unexpected storage collection failures invalidate
the complete performance snapshot.

Disappeared mounts and individual disk counter
regressions are handled without invalidating
otherwise valid measurements.

## Dependencies

Linux collection uses the Go standard library and:

    golang.org/x/sys/unix

macOS collection uses:

    github.com/ebitengine/purego
    golang.org/x/sys/unix

PureGo provides native Mach, IOKit, and Core
Foundation function access without requiring CGO.

Dependencies are versioned through go.mod
and go.sum.

## Security and Privacy

Performance collection:

- Does not normally require administrative privileges.
- Does not execute external monitoring commands.
- Does not read or modify raw disk contents.
- Does not enumerate user files.
- Does not collect process names or command lines.
- Does not collect process-level utilization.
- Does not access agent credentials.
- Does not modify appliance or agent databases.

Filesystem sources and mount paths are included
in the collected metadata.

These fields may contain identifying information,
such as usernames or volume names.

No performance measurements are transmitted
to the appliance in the current implementation.

## Testing

Performance monitoring is covered by:

- CPU and memory calculation tests.
- Native CPU and memory collector tests.
- Filesystem capacity accounting tests.
- Filesystem block conversion and overflow tests.
- Linux mount information parser tests.
- Linux native filesystem identity tests.
- macOS mount discovery tests.
- macOS native filesystem capacity tests.
- Linux disk statistics parser tests.
- Synthetic Linux sysfs discovery tests.
- macOS IOKit function fixture tests.
- Native reference ownership tests.
- Disk throughput and operation rate tests.
- Disk counter regression tests.
- Device arrival and removal tests.
- Unified collection orchestration tests.
- Cancellation and deadline tests.
- Unsupported platform tests.
- CLI dispatch and JSON contract tests.
- Native hardware smoke tests.

Run the metrics tests:

```bash
go test -race -count=1 ./internal/agent/metrics/
```

Run the CLI tests:

```bash
go test -race -count=1 ./cmd/rackos-agent/
```

Run native unified collection tests:

```bash
go test -count=1 -v \
  -run '^TestCollectNative' \
  ./internal/agent/metrics/
```

Run the complete repository validation suite:

```bash
go mod verify
go vet ./...
go test -race -count=1 ./...
go build ./...
```

### Manual Verification

Native filesystem capacity can be compared with df.

On Linux, cumulative disk statistics can be
compared with /proc/diskstats.

On macOS, native storage driver statistics can
be inspected using ioreg.

Controlled temporary file workloads can verify
that disk counters and throughput respond to
storage activity.

These external utilities are used only for
manual verification. They are not required by
the RackOS agent.

## Current Limitations

- No per-core CPU utilization.
- No per-process performance measurements.
- No CPU temperature or frequency monitoring.
- No separate swap or memory pressure measurements.
- No Windows performance collectors.
- No network interface performance measurements.
- No remote filesystem capacity monitoring.
- No physical-to-logical storage topology.
- No APFS container aggregation.
- No RAID health monitoring.
- No SMART or NVMe health statistics.
- No disk temperature monitoring.
- No disk latency or queue-depth measurements.
- No disk utilization percentage.
- No discard or flush statistics.
- No permanent disk hardware identifiers.
- No configurable CPU sampling interval.
- No scheduled background performance collection.
- No authenticated performance reporting.
- No historical performance storage.
- No performance management REST API.
- No management console visualization.

Container measurements may describe the host
or virtual machine rather than individual
container resource limits.

Filesystem records may describe overlapping
storage capacity.

Device-level activity counters may overlap in
storage topologies not covered by the current
filtering policies.

Linux and macOS memory availability estimates
use different operating system accounting methods.

Individual native CPU counter rollovers are
not independently tracked.

Future milestones will introduce network
interface monitoring, authenticated performance
reporting, historical persistence, management
APIs, and retention policies.
