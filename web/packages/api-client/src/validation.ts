
import type {
  APIErrorResponse,
  ApplianceHealthResponse,
  ApplianceReadinessResponse,
  ApplianceVersionResponse,
  CPUPerformance,
  Device,
  DeviceListResponse,
  DeviceSummaryResponse,
  DiskPerformance,
  DiskRate,
  FilesystemCapacityPerformance,
  FilesystemPerformance,
  HardwareInventory,
  MemoryPerformance,
  NetworkInterfacePerformance,
  NetworkInterfaceRate,
  NetworkPerformance,
  PerformanceHistoryResponse,
  PerformanceRecord,
  PerformanceSnapshot,
  StoragePerformance,
} from "./index";

export type Guard<T> = (
  value: unknown,
) => value is T;

function isObject(
  value: unknown,
): value is Record<string, unknown> {
  return (
    typeof value === "object" &&
    value !== null &&
    !Array.isArray(value)
  );
}

function isString(
  value: unknown,
): value is string {
  return typeof value === "string";
}

function isNonEmptyString(
  value: unknown,
): value is string {
  return (
    typeof value === "string" &&
    value.length > 0
  );
}

function isBoolean(
  value: unknown,
): value is boolean {
  return typeof value === "boolean";
}

function isCount(
  value: unknown,
): value is number {
  return (
    typeof value === "number" &&
    Number.isSafeInteger(value) &&
    value >= 0
  );
}

function isUInt32(
  value: unknown,
): value is number {
  return (
    isCount(value) &&
    value <= 4294967295
  );
}

function isPositiveUInt32(
  value: unknown,
): value is number {
  return isUInt32(value) && value > 0;
}

function isNonNegativeNumber(
  value: unknown,
): value is number {
  return (
    typeof value === "number" &&
    Number.isFinite(value) &&
    value >= 0
  );
}

function isPercentage(
  value: unknown,
): value is number {
  return (
    isNonNegativeNumber(value) &&
    value <= 100
  );
}

export function isUInt64String(
  value: unknown,
): value is string {
  return (
    typeof value === "string" &&
    value.length <= 20 &&
    /^(0|[1-9][0-9]*)$/.test(value) &&
    BigInt(value) <= 18446744073709551615n
  );
}

function isSamplingInterval(
  value: unknown,
): value is string {
  return (
    isUInt64String(value) &&
    BigInt(value) >= 1n &&
    BigInt(value) <= 60000n
  );
}

export function isDeviceID(
  value: unknown,
): value is string {
  return (
    typeof value === "string" &&
    /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      value,
    )
  );
}

function isSampleID(
  value: unknown,
): value is string {
  return (
    typeof value === "string" &&
    /^[0-9a-f]{32}$/.test(value)
  );
}

export function isHistoryCursor(
  value: unknown,
): value is string {
  return (
    typeof value === "string" &&
    value.length >= 1 &&
    value.length <= 1024 &&
    /^[A-Za-z0-9_-]+$/.test(value)
  );
}

/**
 * Parse an RFC 3339 timestamp without modifying
 * the original wire representation.
 *
 * Date.parse alone can normalize invalid calendar
 * dates, so verify the calendar components first.
 */
export function timestampMillis(
  value: unknown,
  requireMillisecondAlignment = false,
): number | null {
  if (typeof value !== "string") {
    return null;
  }

  const match =
    /^([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2})(?:\.([0-9]{1,9}))?(Z|[+-](?:[01][0-9]|2[0-3]):[0-5][0-9])$/.exec(
      value,
    );

  if (!match) {
    return null;
  }

  const local = match[1];
  const fraction = match[2] ?? "";
  const zone = match[3];

  if (
    !local ||
    !zone ||
    local.startsWith("0000")
  ) {
    return null;
  }

  if (
    requireMillisecondAlignment &&
    /[1-9]/.test(fraction.slice(3))
  ) {
    return null;
  }

  const localMillis = Date.parse(`${local}Z`);

  if (!Number.isFinite(localMillis)) {
    return null;
  }

  if (
    new Date(localMillis)
      .toISOString()
      .slice(0, 19) !== local
  ) {
    return null;
  }

  const millis = Date.parse(
    `${local}.${fraction.padEnd(3, "0").slice(0, 3)}${zone}`,
  );

  if (!Number.isFinite(millis)) {
    return null;
  }

  const year = new Date(millis).getUTCFullYear();

  if (year < 1 || year > 9999) {
    return null;
  }

  return millis;
}

function isTimestamp(
  value: unknown,
): value is string {
  return (
    typeof value === "string" &&
    value.endsWith("Z") &&
    timestampMillis(value) !== null
  );
}

function optional<T>(
  guard: Guard<T>,
): Guard<T | undefined> {
  return (
    value,
  ): value is T | undefined =>
    value === undefined || guard(value);
}

function nullable<T>(
  guard: Guard<T>,
): Guard<T | null> {
  return (
    value,
  ): value is T | null =>
    value === null || guard(value);
}

function arrayOf<T>(
  guard: Guard<T>,
): Guard<T[]> {
  return (value): value is T[] =>
    Array.isArray(value) &&
    value.every(guard);
}

/**
 * Require a validator for every declared model field.
 *
 * Additional fields are tolerated for forward
 * compatibility with additive API changes.
 */
function shape<T extends object>(
  fields: {
    [K in keyof T]-?: Guard<T[K]>;
  },
): Guard<T> {
  const entries = Object.entries(fields) as [
    string,
    Guard<unknown>,
  ][];

  return (value): value is T =>
    isObject(value) &&
    entries.every(([key, guard]) =>
      guard(value[key]),
    );
}

// Appliance responses

export const isHealthResponse =
  shape<ApplianceHealthResponse>({
    status: (
      value,
    ): value is "healthy" =>
      value === "healthy",
  });

export const isReadinessResponse =
  shape<ApplianceReadinessResponse>({
    status: (
      value,
    ): value is "ready" | "not_ready" =>
      value === "ready" ||
      value === "not_ready",
  });

export const isVersionResponse =
  shape<ApplianceVersionResponse>({
    name: isNonEmptyString,
    version: isNonEmptyString,
  });

export const isErrorResponse =
  shape<APIErrorResponse>({
    error: isNonEmptyString,
  });

// Device responses

export const isDevice = shape<Device>({
  id: isDeviceID,

  hostname: isNonEmptyString,
  os: isNonEmptyString,
  architecture: isNonEmptyString,
  agent_version: isNonEmptyString,

  status: (
    value,
  ): value is "online" | "offline" =>
    value === "online" ||
    value === "offline",

  created_at: isTimestamp,
  updated_at: isTimestamp,
  last_seen_at: isTimestamp,
});

const deviceListShape =
  shape<DeviceListResponse>({
    devices: arrayOf(isDevice),
    total: isCount,
  });

export function isDeviceList(
  value: unknown,
): value is DeviceListResponse {
  return (
    deviceListShape(value) &&
    value.total === value.devices.length &&
    new Set(
      value.devices.map((device) => device.id),
    ).size === value.devices.length
  );
}

const summaryShape =
  shape<DeviceSummaryResponse>({
    total: isCount,
    online: isCount,
    offline: isCount,
  });

export function isDeviceSummary(
  value: unknown,
): value is DeviceSummaryResponse {
  return (
    summaryShape(value) &&
    value.total ===
      value.online + value.offline
  );
}

// Hardware inventory

export const isHardwareInventory =
  shape<HardwareInventory>({
    device_id: isDeviceID,

    hostname: isNonEmptyString,
    os: isNonEmptyString,
    architecture: isNonEmptyString,

    logical_cpus: isPositiveUInt32,

    host_logical_cpus: optional(
      isPositiveUInt32,
    ),

    physical_cores: optional(
      isPositiveUInt32,
    ),

    cpu_model: optional(isNonEmptyString),
    cpu_manufacturer: optional(
      isNonEmptyString,
    ),

    memory_bytes: optional(
      isUInt64String,
    ),

    hardware_model: optional(
      isNonEmptyString,
    ),

    os_version: optional(isNonEmptyString),
    kernel_version: optional(
      isNonEmptyString,
    ),

    boot_time: optional(isTimestamp),

    collected_at: isTimestamp,
    received_at: isTimestamp,
  });

// CPU and memory

const isCPU = shape<CPUPerformance>({
  usage_percent: isPercentage,
  sample_interval_millis: isSamplingInterval,
});

const isMemory = shape<MemoryPerformance>({
  total_bytes: isUInt64String,
  available_bytes: isUInt64String,
  used_bytes: isUInt64String,

  usage_percent: isPercentage,
});

// Filesystem measurements

const isFilesystemCapacity =
  shape<FilesystemCapacityPerformance>({
    total_bytes: isUInt64String,
    free_bytes: isUInt64String,
    available_bytes: isUInt64String,
    used_bytes: isUInt64String,
    reserved_bytes: isUInt64String,

    usage_percent: isPercentage,
  });

const isFilesystem =
  shape<FilesystemPerformance>({
    source: isString,
    mount_point: isNonEmptyString,
    filesystem_type: isNonEmptyString,

    read_only: isBoolean,

    capacity: isFilesystemCapacity,
  });

// Disk measurements

const isDisk = shape<DiskPerformance>({
  device: isNonEmptyString,

  read_bytes: isUInt64String,
  write_bytes: isUInt64String,

  read_operations: isUInt64String,
  write_operations: isUInt64String,
});

const isDiskRate = shape<DiskRate>({
  device: isNonEmptyString,

  read_bytes_per_second: isNonNegativeNumber,
  write_bytes_per_second: isNonNegativeNumber,

  read_operations_per_second:
    isNonNegativeNumber,

  write_operations_per_second:
    isNonNegativeNumber,

  sample_interval_millis: isSamplingInterval,
});

const isStorage = shape<StoragePerformance>({
  filesystems: arrayOf(isFilesystem),
  disks: arrayOf(isDisk),
  disk_rates: arrayOf(isDiskRate),
});

// Network measurements

const isNetworkInterface =
  shape<NetworkInterfacePerformance>({
    name: isNonEmptyString,
    index: isPositiveUInt32,

    up: isBoolean,
    loopback: isBoolean,

    mtu: isUInt32,

    link_speed_bits_per_second: optional(
      isUInt64String,
    ),

    received_bytes: isUInt64String,
    received_packets: isUInt64String,
    receive_errors: isUInt64String,

    transmitted_bytes: isUInt64String,
    transmitted_packets: isUInt64String,
    transmit_errors: isUInt64String,

    receive_dropped: optional(isUInt64String),
    transmit_dropped: optional(isUInt64String),
  });

const isNetworkRate =
  shape<NetworkInterfaceRate>({
    name: isNonEmptyString,
    index: isPositiveUInt32,

    receive_bytes_per_second:
      isNonNegativeNumber,

    transmit_bytes_per_second:
      isNonNegativeNumber,

    receive_packets_per_second:
      isNonNegativeNumber,

    transmit_packets_per_second:
      isNonNegativeNumber,

    sample_interval_millis: isSamplingInterval,
  });

const isNetwork = shape<NetworkPerformance>({
  interfaces: arrayOf(isNetworkInterface),
  interface_rates: arrayOf(isNetworkRate),
});

// Performance responses

const isSnapshot = shape<PerformanceSnapshot>({
  collected_at: isTimestamp,

  cpu: isCPU,
  memory: isMemory,
  storage: isStorage,
  network: isNetwork,
});

export const isPerformanceRecord =
  shape<PerformanceRecord>({
    device_id: isDeviceID,
    sample_id: isSampleID,

    collected_at: isTimestamp,
    received_at: isTimestamp,

    payload_version: (
      value,
    ): value is number => value === 1,

    snapshot: isSnapshot,
  });

const historyShape =
  shape<PerformanceHistoryResponse>({
    device_id: isDeviceID,

    samples: arrayOf(isPerformanceRecord),

    count: isCount,
    has_more: isBoolean,

    next_cursor: nullable(isHistoryCursor),
  });

export function isPerformanceHistory(
  value: unknown,
): value is PerformanceHistoryResponse {
  return (
    historyShape(value) &&
    value.count === value.samples.length &&
    value.count <= 25 &&
    value.has_more ===
      (value.next_cursor !== null) &&
    (!value.has_more || value.count > 0) &&
    value.samples.every(
      (sample) =>
        sample.device_id === value.device_id,
    )
  );
}
