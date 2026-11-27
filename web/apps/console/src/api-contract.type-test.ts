
import type {
  ApplianceHealthResponse,
  ApplianceReadinessResponse,
  ApplianceVersionResponse,
  DeviceListResponse,
  DeviceSummaryResponse,
  HardwareInventory,
  PerformanceRecord,
  PerformanceHistoryResponse,
} from "@rackos/api-client";

/**
 * Compile-time verification that the console can
 * consume every shared management API response type.
 */
export interface ConsoleAPIContract {
  health: ApplianceHealthResponse;
  readiness: ApplianceReadinessResponse;
  version: ApplianceVersionResponse;

  devices: DeviceListResponse;
  summary: DeviceSummaryResponse;

  inventory: HardwareInventory;

  latestPerformance: PerformanceRecord;
  performanceHistory: PerformanceHistoryResponse;
}
