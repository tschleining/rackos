import type {
  PerformanceSnapshot,
  CPUPerformance,
  MemoryPerformance,
  StoragePerformance,
  NetworkPerformance,
} from "@rackos/api-client";

import {
  ByteCapacity,
  MetricPanel,
  MetricRow,
  UsageMeter,
} from "./MetricPanel";

import {
  formatByteRate,
  formatCounter,
  formatCount,
  formatRate,
} from "../../lib/format";

function EmptyMeasurements({
  message,
}: {
  message: string;
}) {
  return (
    <p className="rounded-lg border border-rack-border bg-rack-background p-5 text-sm text-rack-muted">
      {message}
    </p>
  );
}

function CPUDetails({
  cpu,
}: {
  cpu: CPUPerformance;
}) {
  return (
    <MetricPanel
      title="CPU"
      description="System-wide processor utilization."
    >
      <UsageMeter
        label="CPU utilization"
        value={cpu.usage_percent}
      />

      <dl className="mt-5 divide-y divide-rack-border">
        <MetricRow label="Sampling interval">
          {formatCounter(cpu.sample_interval_millis)} ms
        </MetricRow>
      </dl>
    </MetricPanel>
  );
}

function MemoryDetails({
  memory,
}: {
  memory: MemoryPerformance;
}) {
  return (
    <MetricPanel
      title="Memory"
      description="System memory capacity and utilization."
    >
      <UsageMeter
        label="Memory utilization"
        value={memory.usage_percent}
      />

      <dl className="mt-5 divide-y divide-rack-border">
        <MetricRow label="Total memory">
          <ByteCapacity value={memory.total_bytes} />
        </MetricRow>

        <MetricRow label="Used memory">
          <ByteCapacity value={memory.used_bytes} />
        </MetricRow>

        <MetricRow label="Available memory">
          <ByteCapacity value={memory.available_bytes} />
        </MetricRow>
      </dl>
    </MetricPanel>
  );
}

function FilesystemDetails({
  storage,
}: {
  storage: StoragePerformance;
}) {
  return (
    <MetricPanel
      title="Filesystems"
      description="Mounted filesystem capacities and utilization."
    >
      {storage.filesystems.length === 0 ? (
        <EmptyMeasurements
          message="No filesystem measurements in this sample."
        />
      ) : (
        <div className="grid gap-4 xl:grid-cols-2">
          {storage.filesystems.map((filesystem, index) => (
            <article
              key={`${filesystem.mount_point}:${filesystem.source}:${index}`}
              className="min-w-0 rounded-lg border border-rack-border bg-rack-background p-4"
            >
              <h4 className="font-medium break-all">
                {filesystem.mount_point}
              </h4>

              <p className="mt-1 mb-5 text-xs break-all text-rack-muted">
                {filesystem.filesystem_type}
                {" · "}
                {filesystem.read_only
                  ? "Read-only"
                  : "Read/write"}
              </p>

              <UsageMeter
                label={`Usage of ${filesystem.mount_point}`}
                value={
                  filesystem.capacity.usage_percent
                }
              />

              <dl className="mt-4 divide-y divide-rack-border">
                <MetricRow label="Source">
                  {filesystem.source || undefined}
                </MetricRow>

                <MetricRow label="Total">
                  <ByteCapacity
                    value={
                      filesystem.capacity.total_bytes
                    }
                  />
                </MetricRow>

                <MetricRow label="Used">
                  <ByteCapacity
                    value={
                      filesystem.capacity.used_bytes
                    }
                  />
                </MetricRow>

                <MetricRow label="Free">
                  <ByteCapacity
                    value={
                      filesystem.capacity.free_bytes
                    }
                  />
                </MetricRow>

                <MetricRow label="Available">
                  <ByteCapacity
                    value={
                      filesystem.capacity.available_bytes
                    }
                  />
                </MetricRow>

                <MetricRow label="Reserved">
                  <ByteCapacity
                    value={
                      filesystem.capacity.reserved_bytes
                    }
                  />
                </MetricRow>
              </dl>
            </article>
          ))}
        </div>
      )}
    </MetricPanel>
  );
}

function DiskDetails({
  storage,
}: {
  storage: StoragePerformance;
}) {
  const rates = new Map(
    storage.disk_rates.map((rate) => [
      rate.device,
      rate,
    ]),
  );

  return (
    <MetricPanel
      title="Disk activity"
      description="Measured throughput, operation rates and cumulative device counters."
    >
      {storage.disks.length === 0 ? (
        <EmptyMeasurements
          message="No disk measurements in this sample."
        />
      ) : (
        <div className="grid gap-4 xl:grid-cols-2">
          {storage.disks.map((disk) => {
            const rate = rates.get(disk.device);

            return (
              <article
                key={disk.device}
                className="min-w-0 rounded-lg border border-rack-border bg-rack-background p-4"
              >
                <h4 className="font-medium break-all">
                  {disk.device}
                </h4>

                <div className="my-5 grid gap-4 sm:grid-cols-2">
                  <div>
                    <p className="text-xs text-rack-muted">
                      Read throughput
                    </p>

                    <p className="mt-2 text-xl font-semibold tabular-nums">
                      {rate
                        ? formatByteRate(
                            rate.read_bytes_per_second,
                          )
                        : "Unavailable"}
                    </p>
                  </div>

                  <div>
                    <p className="text-xs text-rack-muted">
                      Write throughput
                    </p>

                    <p className="mt-2 text-xl font-semibold tabular-nums">
                      {rate
                        ? formatByteRate(
                            rate.write_bytes_per_second,
                          )
                        : "Unavailable"}
                    </p>
                  </div>
                </div>

                <dl className="divide-y divide-rack-border">
                  <MetricRow label="Read operations">
                    {rate
                      ? formatRate(
                          rate.read_operations_per_second,
                          "ops/s",
                        )
                      : undefined}
                  </MetricRow>

                  <MetricRow label="Write operations">
                    {rate
                      ? formatRate(
                          rate.write_operations_per_second,
                          "ops/s",
                        )
                      : undefined}
                  </MetricRow>

                  <MetricRow label="Sampling interval">
                    {rate
                      ? `${formatCounter(rate.sample_interval_millis)} ms`
                      : undefined}
                  </MetricRow>

                  <MetricRow label="Cumulative bytes read">
                    {formatCounter(disk.read_bytes)}
                  </MetricRow>

                  <MetricRow label="Cumulative bytes written">
                    {formatCounter(disk.write_bytes)}
                  </MetricRow>

                  <MetricRow label="Cumulative read operations">
                    {formatCounter(disk.read_operations)}
                  </MetricRow>

                  <MetricRow label="Cumulative write operations">
                    {formatCounter(disk.write_operations)}
                  </MetricRow>
                </dl>

                {!rate && (
                  <p className="mt-4 text-xs text-rack-muted">
                    No rate measurement is available
                    for this disk in the selected sample.
                  </p>
                )}
              </article>
            );
          })}
        </div>
      )}
    </MetricPanel>
  );
}

function NetworkDetails({
  network,
}: {
  network: NetworkPerformance;
}) {
  const rates = new Map(
    network.interface_rates.map((rate) => [
      `${rate.index}:${rate.name}`,
      rate,
    ]),
  );

  return (
    <MetricPanel
      title="Network activity"
      description="Interface metadata, measured throughput and cumulative network counters."
    >
      {network.interfaces.length === 0 ? (
        <EmptyMeasurements
          message="No network interface measurements in this sample."
        />
      ) : (
        <div className="grid gap-4 xl:grid-cols-2">
          {network.interfaces.map((networkInterface) => {
            const key =
              `${networkInterface.index}:${networkInterface.name}`;

            const rate = rates.get(key);

            return (
              <article
                key={key}
                className="min-w-0 rounded-lg border border-rack-border bg-rack-background p-4"
              >
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <h4 className="font-medium break-all">
                    {networkInterface.name}
                  </h4>

                  <span
                    className={
                      networkInterface.up
                        ? "text-xs text-rack-success"
                        : "text-xs text-rack-muted"
                    }
                  >
                    {networkInterface.up
                      ? "Interface up"
                      : "Interface down"}
                  </span>
                </div>

                <div className="my-5 grid gap-4 sm:grid-cols-2">
                  <div>
                    <p className="text-xs text-rack-muted">
                      Receive throughput
                    </p>

                    <p className="mt-2 text-xl font-semibold tabular-nums">
                      {rate
                        ? formatByteRate(
                            rate.receive_bytes_per_second,
                          )
                        : "Unavailable"}
                    </p>
                  </div>

                  <div>
                    <p className="text-xs text-rack-muted">
                      Transmit throughput
                    </p>

                    <p className="mt-2 text-xl font-semibold tabular-nums">
                      {rate
                        ? formatByteRate(
                            rate.transmit_bytes_per_second,
                          )
                        : "Unavailable"}
                    </p>
                  </div>
                </div>

                <dl className="divide-y divide-rack-border">
                  <MetricRow label="Interface index">
                    {formatCount(networkInterface.index)}
                  </MetricRow>

                  <MetricRow label="Loopback">
                    {networkInterface.loopback
                      ? "Yes"
                      : "No"}
                  </MetricRow>

                  <MetricRow label="Reported MTU">
                    {formatCount(networkInterface.mtu)}
                  </MetricRow>

                  <MetricRow label="Link speed">
                    {networkInterface.link_speed_bits_per_second !== undefined
                      ? `${formatCounter(networkInterface.link_speed_bits_per_second)} bit/s`
                      : undefined}
                  </MetricRow>

                  <MetricRow label="Receive packet rate">
                    {rate
                      ? formatRate(
                          rate.receive_packets_per_second,
                          "packets/s",
                        )
                      : undefined}
                  </MetricRow>

                  <MetricRow label="Transmit packet rate">
                    {rate
                      ? formatRate(
                          rate.transmit_packets_per_second,
                          "packets/s",
                        )
                      : undefined}
                  </MetricRow>

                  <MetricRow label="Sampling interval">
                    {rate
                      ? `${formatCounter(rate.sample_interval_millis)} ms`
                      : undefined}
                  </MetricRow>

                  <MetricRow label="Cumulative bytes received">
                    {formatCounter(networkInterface.received_bytes)}
                  </MetricRow>

                  <MetricRow label="Cumulative bytes transmitted">
                    {formatCounter(networkInterface.transmitted_bytes)}
                  </MetricRow>

                  <MetricRow label="Received packets">
                    {formatCounter(networkInterface.received_packets)}
                  </MetricRow>

                  <MetricRow label="Transmitted packets">
                    {formatCounter(networkInterface.transmitted_packets)}
                  </MetricRow>

                  <MetricRow label="Receive errors">
                    {formatCounter(networkInterface.receive_errors)}
                  </MetricRow>

                  <MetricRow label="Transmit errors">
                    {formatCounter(networkInterface.transmit_errors)}
                  </MetricRow>

                  <MetricRow label="Receive drops">
                    {networkInterface.receive_dropped !== undefined
                      ? formatCounter(networkInterface.receive_dropped)
                      : undefined}
                  </MetricRow>

                  <MetricRow label="Transmit drops">
                    {networkInterface.transmit_dropped !== undefined
                      ? formatCounter(networkInterface.transmit_dropped)
                      : undefined}
                  </MetricRow>
                </dl>
              </article>
            );
          })}
        </div>
      )}
    </MetricPanel>
  );
}

export function PerformanceDetails({
  snapshot,
}: {
  snapshot: PerformanceSnapshot;
}) {
  return (
    <div className="space-y-6">
      <div className="grid items-start gap-6 lg:grid-cols-2">
        <CPUDetails cpu={snapshot.cpu} />

        <MemoryDetails memory={snapshot.memory} />
      </div>

      <FilesystemDetails
        storage={snapshot.storage}
      />

      <DiskDetails
        storage={snapshot.storage}
      />

      <NetworkDetails
        network={snapshot.network}
      />
    </div>
  );
}