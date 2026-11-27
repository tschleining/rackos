import {
  useOutletContext,
} from "react-router";

import type {
  Device,
} from "@rackos/api-client";

export interface DeviceDetailContext {
  device: Device;
  dataUpdatedAt: number;
  isStale: boolean;
}

export function useDeviceDetail() {
  return useOutletContext<DeviceDetailContext>();
}

const timestampFormatter =
  new Intl.DateTimeFormat(
    undefined,
    {
      dateStyle: "medium",
      timeStyle: "medium",
    },
  );

export function formatDeviceTimestamp(
  value: string,
): string {
  const date = new Date(value);

  if (!Number.isFinite(date.getTime())) {
    return "Unavailable";
  }

  return timestampFormatter.format(date);
}