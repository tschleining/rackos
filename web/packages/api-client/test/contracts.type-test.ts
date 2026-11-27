
import type {
  APIErrorResponse,
  ApplianceHealthResponse,
  ApplianceReadinessResponse,
  ApplianceVersionResponse,
  Device,
  DeviceListResponse,
  DeviceSummaryResponse,
  HardwareInventory,
  NetworkInterfacePerformance,
  PerformanceHistoryQuery,
  PerformanceHistoryResponse,
  PerformanceRecord,
  UInt64String,
} from "@rackos/api-client";

const deviceID =
  "11111111-1111-4111-8111-111111111111";

const timestamp = "2026-11-27T12:00:00.000Z";

// Appliance contracts

export const health = {
  status: "healthy",
} satisfies ApplianceHealthResponse;

export const readiness = {
  status: "ready",
} satisfies ApplianceReadinessResponse;

export const unavailable = {
  status: "not_ready",
} satisfies ApplianceReadinessResponse;

export const version = {
  name: "RackOS",
  version: "0.1.0-dev",
} satisfies ApplianceVersionResponse;

// Device contracts

export const device = {
  id: deviceID,
  hostname: "test-device.local",
  os: "linux",
  architecture: "amd64",
  agent_version: "0.1.0-dev",
  status: "online",
  created_at: timestamp,
  updated_at: timestamp,
  last_seen_at: timestamp,
} satisfies Device;

export const deviceList = {
  devices: [device],
  total: 1,
} satisfies DeviceListResponse;

export const emptyDeviceList = {
  devices: [],
  total: 0,
} satisfies DeviceListResponse;

export const summary = {
  total: 1,
  online: 1,
  offline: 0,
} satisfies DeviceSummaryResponse;

// Inventory contracts

export const minimalInventory = {
  device_id: deviceID,
  hostname: "test-device.local",
  os: "linux",
  architecture: "amd64",
  logical_cpus: 8,
  collected_at: timestamp,
  received_at: timestamp,
} satisfies HardwareInventory;

export const fullInventory = {
  ...minimalInventory,

  host_logical_cpus: 8,
  physical_cores: 4,

  cpu_model: "Test processor",
  cpu_manufacturer: "Test manufacturer",

  memory_bytes: "17179869184",

  hardware_model: "Test workstation",
  os_version: "Test OS",
  kernel_version: "Test kernel",

  boot_time: "2026-11-27T08:00:00.000Z",
} satisfies HardwareInventory;

// Network contracts

export const networkInterface = {
  name: "eth0",
  index: 2,

  up: true,
  loopback: false,
  mtu: 1500,

  received_bytes: "9007199254740993",
  received_packets: "1200",
  receive_errors: "0",

  transmitted_bytes: "536870912",
  transmitted_packets: "700",
  transmit_errors: "0",
} satisfies NetworkInterfacePerformance;

export const networkWithOptionalCounters = {
  ...networkInterface,

  link_speed_bits_per_second: "1000000000",

  receive_dropped: "0",
  transmit_dropped: "0",
} satisfies NetworkInterfacePerformance;

// Complete performance response

export const performanceRecord = {
  device_id: deviceID,

  sample_id:
    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",

  collected_at: timestamp,
  received_at: timestamp,

  payload_version: 1,

  snapshot: {
    collected_at: timestamp,

    cpu: {
      usage_percent: 25.5,
      sample_interval_millis: "1000",
    },

    memory: {
      total_bytes: "17179869184",
      available_bytes: "6442450944",
      used_bytes: "10737418240",

      usage_percent: 62.5,
    },

    storage: {
      filesystems: [
        {
          source: "/dev/sda1",
          mount_point: "/",
          filesystem_type: "ext4",
          read_only: false,

          capacity: {
            total_bytes: "107374182400",
            free_bytes: "53687091200",
            available_bytes: "48318382080",
            used_bytes: "53687091200",
            reserved_bytes: "5368709120",

            usage_percent: 50,
          },
        },
      ],

      disks: [
        {
          device: "sda",

          read_bytes: "1073741824",
          write_bytes: "536870912",

          read_operations: "10000",
          write_operations: "5000",
        },
      ],

      disk_rates: [
        {
          device: "sda",

          read_bytes_per_second: 1048576,
          write_bytes_per_second: 524288,

          read_operations_per_second: 100,
          write_operations_per_second: 50,

          sample_interval_millis: "1000",
        },
      ],
    },

    network: {
      interfaces: [
        networkWithOptionalCounters,
      ],

      interface_rates: [
        {
          name: "eth0",
          index: 2,

          receive_bytes_per_second: 1048576,
          transmit_bytes_per_second: 524288,

          receive_packets_per_second: 100,
          transmit_packets_per_second: 50,

          sample_interval_millis: "1000",
        },
      ],
    },
  },
} satisfies PerformanceRecord;

// History contracts

export const historyQuery = {
  from: "2026-11-27T11:00:00.000Z",
  to: timestamp,
  limit: 25,
} satisfies PerformanceHistoryQuery;

export const emptyHistoryQuery = {
} satisfies PerformanceHistoryQuery;

export const historyPage = {
  device_id: deviceID,
  samples: [performanceRecord],
  count: 1,
  has_more: false,
  next_cursor: null,
} satisfies PerformanceHistoryResponse;

export const emptyHistoryPage = {
  device_id: deviceID,
  samples: [],
  count: 0,
  has_more: false,
  next_cursor: null,
} satisfies PerformanceHistoryResponse;

export const continuationPage = {
  device_id: deviceID,
  samples: [performanceRecord],
  count: 1,
  has_more: true,
  next_cursor: "opaque-test-cursor",
} satisfies PerformanceHistoryResponse;

// Error contract

export const apiError = {
  error: "device not found",
} satisfies APIErrorResponse;

// Negative compile-time tests

// @ts-expect-error 64-bit counters must be strings.
export const invalidCounter: UInt64String = 123;

// @ts-expect-error Device status must match the API.
export const invalidDevice: Device = { ...device, status: "unknown" };

// @ts-expect-error Inventory memory must be a string.
export const invalidMemory: HardwareInventory = { ...minimalInventory, memory_bytes: 17179869184 };

// @ts-expect-error Optional counters cannot be null.
export const invalidDrops: NetworkInterfacePerformance = { ...networkInterface, receive_dropped: null };

// @ts-expect-error Optional counters cannot be numbers.
export const invalidNumericDrops: NetworkInterfacePerformance = { ...networkInterface, receive_dropped: 0 };

// @ts-expect-error Unavailable counters must be omitted.
export const invalidUndefinedDrops: NetworkInterfacePerformance = { ...networkInterface, receive_dropped: undefined };

// @ts-expect-error Continuation cursor is required.
export const missingCursor: PerformanceHistoryResponse = { device_id: deviceID, samples: [], count: 0, has_more: false };

// @ts-expect-error Historical query limits are numbers.
export const invalidLimit: PerformanceHistoryQuery = { limit: "25" };
