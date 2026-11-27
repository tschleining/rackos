
import type {
  DeviceID,
  HistoryCursor,
  Int64String,
  RFC3339Timestamp,
  SampleID,
  UInt64String,
} from "./common";

// CPU measurements

export interface CPUPerformance {
  usage_percent: number;
  sample_interval_millis: Int64String;
}

// Memory measurements

export interface MemoryPerformance {
  total_bytes: UInt64String;
  available_bytes: UInt64String;
  used_bytes: UInt64String;

  usage_percent: number;
}

// Filesystem measurements

export interface FilesystemCapacityPerformance {
  total_bytes: UInt64String;
  free_bytes: UInt64String;
  available_bytes: UInt64String;
  used_bytes: UInt64String;
  reserved_bytes: UInt64String;

  usage_percent: number;
}

export interface FilesystemPerformance {
  source: string;
  mount_point: string;
  filesystem_type: string;

  read_only: boolean;

  capacity: FilesystemCapacityPerformance;
}

// Disk activity measurements

/**
 * Cumulative storage device activity counters.
 */
export interface DiskPerformance {
  device: string;

  read_bytes: UInt64String;
  write_bytes: UInt64String;

  read_operations: UInt64String;
  write_operations: UInt64String;
}

/**
 * Calculated storage device activity rates.
 */
export interface DiskRate {
  device: string;

  read_bytes_per_second: number;
  write_bytes_per_second: number;

  read_operations_per_second: number;
  write_operations_per_second: number;

  sample_interval_millis: Int64String;
}

/**
 * Complete storage performance measurements.
 */
export interface StoragePerformance {
  filesystems: FilesystemPerformance[];
  disks: DiskPerformance[];
  disk_rates: DiskRate[];
}

// Network measurements

/**
 * Network interface metadata and cumulative counters.
 */
export interface NetworkInterfacePerformance {
  name: string;
  index: number;

  up: boolean;
  loopback: boolean;

  mtu: number;

  link_speed_bits_per_second?: UInt64String;

  received_bytes: UInt64String;
  received_packets: UInt64String;
  receive_errors: UInt64String;

  transmitted_bytes: UInt64String;
  transmitted_packets: UInt64String;
  transmit_errors: UInt64String;

  receive_dropped?: UInt64String;
  transmit_dropped?: UInt64String;
}

/**
 * Calculated network interface activity rates.
 */
export interface NetworkInterfaceRate {
  name: string;
  index: number;

  receive_bytes_per_second: number;
  transmit_bytes_per_second: number;

  receive_packets_per_second: number;
  transmit_packets_per_second: number;

  sample_interval_millis: Int64String;
}

/**
 * Complete network performance measurements.
 */
export interface NetworkPerformance {
  interfaces: NetworkInterfacePerformance[];
  interface_rates: NetworkInterfaceRate[];
}

// Performance snapshot

/**
 * Complete native agent performance measurement.
 */
export interface PerformanceSnapshot {
  collected_at: RFC3339Timestamp;

  cpu: CPUPerformance;
  memory: MemoryPerformance;
  storage: StoragePerformance;
  network: NetworkPerformance;
}

// Management API responses

/**
 * Persisted historical performance record.
 *
 * GET /api/v1/devices/{id}/performance/latest
 *
 * Also used for individual historical page entries.
 */
export interface PerformanceRecord {
  device_id: DeviceID;
  sample_id: SampleID;

  collected_at: RFC3339Timestamp;
  received_at: RFC3339Timestamp;

  payload_version: number;

  snapshot: PerformanceSnapshot;
}

/**
 * Historical performance query parameters.
 *
 * from: inclusive collection timestamp.
 * to: exclusive collection timestamp.
 *
 * Time bounds must be millisecond-aligned.
 *
 * Page limits must be between 1 and 25.
 * The appliance defaults to 10.
 */
export interface PerformanceHistoryQuery {
  from?: RFC3339Timestamp;
  to?: RFC3339Timestamp;

  limit?: number;

  cursor?: HistoryCursor;
}

/**
 * Bounded historical performance response.
 *
 * GET /api/v1/devices/{id}/performance/history
 */
export interface PerformanceHistoryResponse {
  device_id: DeviceID;

  samples: PerformanceRecord[];

  count: number;
  has_more: boolean;

  next_cursor: HistoryCursor | null;
}
