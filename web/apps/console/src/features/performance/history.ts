import type {
  PerformanceRecord,
} from "@rackos/api-client";

export const HISTORY_RANGES = [
  { label: "Last 15 minutes", durationMs: 15 * 60_000 },
  { label: "Last hour", durationMs: 60 * 60_000 },
  { label: "Last 6 hours", durationMs: 6 * 60 * 60_000 },
  { label: "Last 24 hours", durationMs: 24 * 60 * 60_000 },
] as const;

export const DEFAULT_HISTORY_DURATION =
  60 * 60_000;

export const HISTORY_GAP_MS =
  150_000;

export interface HistoryPoint {
  timestamp: number;
  first: number | null;
  second: number | null;
}

type MeasurementValues = readonly [
  number | null,
  number | null,
];

export type ResourceKind =
  | "filesystem"
  | "disk"
  | "network";

export interface ResourceOption {
  id: string;
  label: string;
}

/**
 * Preserve actual collection times.
 *
 * Insert a null point between samples separated
 * by an unexpectedly long measurement interval.
 */
export function buildHistoryPoints(
  samples: readonly PerformanceRecord[],
  project: (
    sample: PerformanceRecord,
  ) => MeasurementValues,
): HistoryPoint[] {
  const ordered = [...samples].sort(
    (first, second) =>
      Date.parse(first.collected_at) -
        Date.parse(second.collected_at) ||
      first.sample_id.localeCompare(
        second.sample_id,
      ),
  );

  const points: HistoryPoint[] = [];

  let previousTimestamp: number | null = null;

  for (const sample of ordered) {
    const timestamp = Date.parse(
      sample.collected_at,
    );

    if (!Number.isFinite(timestamp)) {
      continue;
    }

    if (
      previousTimestamp !== null &&
      timestamp - previousTimestamp >
        HISTORY_GAP_MS
    ) {
      points.push({
        timestamp:
          previousTimestamp +
          Math.floor(
            (timestamp - previousTimestamp) / 2,
          ),

        first: null,
        second: null,
      });
    }

    const [first, second] = project(sample);

    points.push({
      timestamp,
      first,
      second,
    });

    previousTimestamp = timestamp;
  }

  return points;
}

export function buildUtilizationHistory(
  samples: readonly PerformanceRecord[],
): HistoryPoint[] {
  return buildHistoryPoints(
    samples,
    (sample) => [
      sample.snapshot.cpu.usage_percent,
      sample.snapshot.memory.usage_percent,
    ],
  );
}

function filesystemID(
  mountPoint: string,
  source: string,
  filesystemType: string,
): string {
  return JSON.stringify([
    mountPoint,
    source,
    filesystemType,
  ]);
}

function networkID(
  index: number,
  name: string,
): string {
  return JSON.stringify([index, name]);
}

/**
 * Discover resources across all retrieved samples,
 * including resources not present in the latest one.
 */
export function getResourceOptions(
  samples: readonly PerformanceRecord[],
  kind: ResourceKind,
): ResourceOption[] {
  const options = new Map<string, string>();

  for (const { snapshot } of samples) {
    switch (kind) {
      case "filesystem":
        for (const filesystem of snapshot.storage.filesystems) {
          options.set(
            filesystemID(
              filesystem.mount_point,
              filesystem.source,
              filesystem.filesystem_type,
            ),
            filesystem.source
              ? `${filesystem.mount_point} (${filesystem.source}, ${filesystem.filesystem_type})`
              : `${filesystem.mount_point} (${filesystem.filesystem_type})`,
          );
        }

        break;

      case "disk":
        for (const disk of snapshot.storage.disks) {
          options.set(
            disk.device,
            disk.device,
          );
        }

        break;

      case "network":
        for (const networkInterface of snapshot.network.interfaces) {
          options.set(
            networkID(
              networkInterface.index,
              networkInterface.name,
            ),
            `${networkInterface.name} (index ${networkInterface.index})`,
          );
        }

        break;
    }
  }

  return Array.from(
    options,
    ([id, label]) => ({ id, label }),
  ).sort(
    (first, second) =>
      first.label.localeCompare(
        second.label,
        undefined,
        { numeric: true },
      ) ||
      first.id.localeCompare(second.id),
  );
}

export function buildResourceHistory(
  samples: readonly PerformanceRecord[],
  kind: ResourceKind,
  resourceID: string,
): HistoryPoint[] {
  return buildHistoryPoints(
    samples,
    ({ snapshot }) => {
      switch (kind) {
        case "filesystem": {
          const filesystem =
            snapshot.storage.filesystems.find(
              (item) =>
                filesystemID(
                  item.mount_point,
                  item.source,
                  item.filesystem_type,
                ) === resourceID,
            );

          return [
            filesystem?.capacity.usage_percent
              ?? null,
            null,
          ];
        }

        case "disk": {
          const rate =
            snapshot.storage.disk_rates.find(
              (item) => item.device === resourceID,
            );

          return [
            rate?.read_bytes_per_second ?? null,
            rate?.write_bytes_per_second ?? null,
          ];
        }

        case "network": {
          const rate =
            snapshot.network.interface_rates.find(
              (item) =>
                networkID(
                  item.index,
                  item.name,
                ) === resourceID,
            );

          return [
            rate?.receive_bytes_per_second ?? null,
            rate?.transmit_bytes_per_second ?? null,
          ];
        }
      }
    },
  );
}