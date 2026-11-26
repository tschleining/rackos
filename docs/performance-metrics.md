
# Performance Metrics

RackOS provides native system-wide CPU and memory
utilization collection on macOS and Linux.

Performance metrics are collected independently of
hardware inventory and agent heartbeat operations.

## Supported Platforms

| Platform | Architecture | Collection Method |
| --- | --- | --- |
| macOS | ARM64 | Mach host statistics |
| macOS | AMD64 | Mach host statistics |
| Linux | ARM64 | proc filesystem |
| Linux | AMD64 | proc filesystem |

Other platforms return an explicit unsupported platform
error.

Android and iOS are not supported targets.

Cross-compilation does not guarantee runtime support
on an untested platform.

## Architecture

The metrics implementation is located in:

    internal/agent/metrics/

The package consists of:

- Portable performance snapshot models.
- CPU and memory utilization calculations.
- Platform-specific native collectors.
- Context-aware sampling orchestration.
- Unsupported platform handling.
- Unit tests and native smoke tests.

The public collection entry point is:

    metrics.Collect(ctx)

Collection returns a complete performance snapshot
or an error. Partial snapshots are not returned.

## CPU Utilization

CPU utilization is calculated using two cumulative
CPU counter readings separated by a sampling interval.

The default sampling interval is one second.

Utilization is calculated as:

    busy_delta / (busy_delta + idle_delta) * 100

CPU utilization represents aggregate activity across
all logical processors and ranges from zero to 100
percent.

The actual elapsed sampling interval is included
in each snapshot in milliseconds.

### Linux CPU Collection

Linux CPU counters are read from:

    /proc/stat

The aggregate CPU line is used rather than individual
processor statistics.

Busy time includes:

- User execution.
- Nice execution.
- System execution.
- Hardware interrupts.
- Software interrupts.
- Hypervisor steal time.

Idle time includes:

- Idle processor time.
- I/O wait.

Guest execution counters are not added separately
because they are already included in user and nice
execution counters.

Hypervisor steal time represents unavailable CPU
capacity rather than active execution by the guest.

### macOS CPU Collection

macOS CPU counters are collected using the Mach
host_statistics function with HOST_CPU_LOAD_INFO.

Busy time includes user, system, and nice execution.

Idle time includes the native idle counter.

Native functions are accessed through PureGo without
requiring CGO or external command execution.

The native system library and Mach host port are
reused for the lifetime of the process.

### Counter Validation

CPU samples are rejected when:

- Aggregate counters decrease.
- No CPU activity has elapsed between readings.
- Counter addition overflows.
- The measured sampling interval is invalid.
- Native statistics are malformed.

Native counter rollover and CPU topology changes
may invalidate individual samples.

## Memory Utilization

Memory measurements contain:

- Total physical or usable memory capacity.
- Estimated available memory.
- Derived used memory.
- Memory utilization percentage.

All memory capacity values are expressed in bytes.

Used memory is calculated as:

    total_bytes - available_bytes

Memory utilization is calculated as:

    used_bytes / total_bytes * 100

### Linux Memory Collection

Linux memory statistics are read from:

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

The kernel page size is obtained using host_page_size.

Available memory is estimated as:

    (free_pages + inactive_pages) * page_size

The native free-page counter already includes
speculative pages.

Speculative pages must not be added separately.

Physical memory capacity and kernel page size are
cached for the lifetime of the process.

Available memory is an estimate. Inactive memory
is not necessarily immediately reclaimable.

The macOS measurement does not necessarily match
the Memory Used figure displayed in Activity Monitor.

### Memory Validation

Memory measurements are rejected when:

- Total memory capacity is zero.
- Available memory exceeds total capacity.
- Required statistics are missing or malformed.
- Required Linux statistics appear more than once.
- Native page size is invalid.
- Byte conversion overflows.

Zero available memory is valid and represents
100 percent memory utilization.

## Snapshot Format

A performance snapshot contains CPU utilization,
memory utilization, and a UTC collection timestamp.

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
  "collected_at": "2026-11-26T23:00:00Z"
}
```

The values above are illustrative.

CPU utilization represents activity over a sampling
interval. Memory represents a point-in-time reading
taken after CPU sampling completes.

The collection timestamp represents completion of
the snapshot.

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

The command requires no elevated privileges.

## Cancellation and Error Handling

Collection respects caller context cancellation
and deadlines.

The CPU sampling timer supports cancellation.

Native calls and filesystem reads cannot necessarily
be interrupted while already in progress.

Errors preserve their underlying causes so callers
can identify cancellation, invalid statistics, and
unsupported platforms.

## Dependencies

Linux collection uses the Go standard library.

macOS collection uses:

- github.com/ebitengine/purego
- golang.org/x/sys/unix

PureGo provides native function access without
requiring CGO.

Dependencies are versioned through go.mod and go.sum.

## Security and Privacy

Performance collection:

- Does not require administrative privileges.
- Does not execute external monitoring commands.
- Does not collect process names or command lines.
- Does not collect process-level utilization.
- Does not access agent credentials.
- Does not modify appliance or agent databases.

## Testing

Performance metrics are covered by:

- CPU utilization calculation tests.
- Memory accounting tests.
- Integer overflow tests.
- CPU counter regression tests.
- Linux CPU parser tests.
- Linux memory parser tests.
- macOS CPU normalization tests.
- macOS native structure layout tests.
- macOS memory normalization tests.
- Unsupported platform tests.
- Collection orchestration tests.
- Cancellation and deadline tests.
- Native collection smoke tests.
- CLI command and JSON output tests.

Run the metrics tests:

```bash
go test -race -count=1 ./internal/agent/metrics/
```

Run the CLI tests:

```bash
go test -race -count=1 ./cmd/rackos-agent/
```

Run the native collection smoke test:

```bash
go test -count=1 -v \
  -run '^TestCollectNative$' \
  ./internal/agent/metrics/
```

## Current Limitations

- No per-core CPU utilization.
- No per-process performance measurements.
- No CPU temperature or frequency monitoring.
- No separate swap utilization measurements.
- No memory pressure measurements.
- No Windows performance collectors.
- No configurable CPU sampling interval.
- No scheduled background performance collection.
- No performance reporting to the appliance.
- No historical performance storage.
- No performance management REST API.
- No management console visualization.
- Container measurements may describe the host
  rather than individual container resource limits.
- Linux and macOS memory availability estimates
  use different operating system accounting methods.
- Individual native CPU counter rollovers are not
  independently tracked.

Future milestones will extend performance collection
with storage and network metrics, authenticated
reporting, historical persistence, management APIs,
and retention policies.
