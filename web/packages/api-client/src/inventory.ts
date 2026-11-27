
import type {
  DeviceID,
  RFC3339Timestamp,
  UInt64String,
} from "./common";

/**
 * Latest stored hardware inventory snapshot.
 *
 * GET /api/v1/devices/{id}/inventory
 *
 * Unavailable optional hardware measurements are
 * omitted from the response.
 */
export interface HardwareInventory {
  device_id: DeviceID;

  hostname: string;
  os: string;
  architecture: string;

  logical_cpus: number;

  host_logical_cpus?: number;
  physical_cores?: number;

  cpu_model?: string;
  cpu_manufacturer?: string;

  memory_bytes?: UInt64String;

  hardware_model?: string;
  os_version?: string;
  kernel_version?: string;

  boot_time?: RFC3339Timestamp;

  collected_at: RFC3339Timestamp;
  received_at: RFC3339Timestamp;
}
