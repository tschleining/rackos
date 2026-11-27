import { useQuery } from "@tanstack/react-query";

import type {
  DeviceID,
} from "@rackos/api-client";

import { api } from "../app/services";

export const deviceKeys = {
  all: ["devices"] as const,

  list: ["devices", "list"] as const,

  detail: (deviceID: DeviceID) =>
    ["devices", "detail", deviceID] as const,
};

export function useDevices() {
  return useQuery({
    queryKey: deviceKeys.list,

    queryFn: ({ signal }) =>
      api.getDevices({ signal }),

    refetchInterval: 15_000,
  });
}

export function useDevice(
  deviceID: DeviceID,
) {
  return useQuery({
    queryKey: deviceKeys.detail(deviceID),

    queryFn: ({ signal }) =>
      api.getDevice(deviceID, { signal }),

    refetchInterval: 15_000,
  });
}