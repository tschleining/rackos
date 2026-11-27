import { useQuery } from "@tanstack/react-query";

import type {
  DeviceID,
} from "@rackos/api-client";

import { api } from "../app/services";

export const inventoryKeys = {
  all: ["inventory"] as const,

  detail: (deviceID: DeviceID) =>
    ["inventory", deviceID] as const,
};

export function useInventory(
  deviceID: DeviceID,
) {
  return useQuery({
    queryKey: inventoryKeys.detail(deviceID),

    queryFn: ({ signal }) =>
      api.getInventory(deviceID, { signal }),

    staleTime: 5 * 60_000,

    refetchInterval: 5 * 60_000,
  });
}