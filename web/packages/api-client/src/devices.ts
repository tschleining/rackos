
import type {
  DeviceID,
  RFC3339Timestamp,
} from "./common";

/**
 * Device connectivity status calculated by the
 * appliance using the most recent heartbeat.
 */
export type DeviceStatus = "online" | "offline";

/**
 * Registered device metadata.
 *
 * GET /api/v1/devices/{id}
 */
export interface Device {
  id: DeviceID;

  hostname: string;
  os: string;
  architecture: string;
  agent_version: string;

  status: DeviceStatus;

  created_at: RFC3339Timestamp;
  updated_at: RFC3339Timestamp;
  last_seen_at: RFC3339Timestamp;
}

/**
 * Registered device list.
 *
 * GET /api/v1/devices
 */
export interface DeviceListResponse {
  devices: Device[];
  total: number;
}

/**
 * Aggregate registered device status.
 *
 * GET /api/v1/devices/summary
 */
export interface DeviceSummaryResponse {
  total: number;
  online: number;
  offline: number;
}
