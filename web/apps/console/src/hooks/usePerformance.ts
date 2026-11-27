import { useQuery } from "@tanstack/react-query";

import type {
  DeviceID,
} from "@rackos/api-client";

import { api } from "../app/services";

export const performanceKeys = {
  all: ["performance"] as const,

  latest: (deviceID: DeviceID) =>
    ["performance", deviceID, "latest"] as const,

  history: (
    deviceID: DeviceID,
    durationMs: number,
  ) =>
    [
      "performance",
      deviceID,
      "history",
      durationMs,
    ] as const,
};

export function useLatestPerformance(
  deviceID: DeviceID,
) {
  return useQuery({
    queryKey: performanceKeys.latest(deviceID),

    queryFn: ({ signal }) =>
      api.getLatestPerformance(
        deviceID,
        { signal },
      ),

    staleTime: 15_000,

    refetchInterval: 30_000,
  });
}

export function usePerformanceHistory(
  deviceID: DeviceID,
  durationMs: number,
) {
  return useQuery({
    queryKey: performanceKeys.history(
      deviceID,
      durationMs,
    ),

    queryFn: async ({ signal }) => {
      const end = Date.now() + 1;
      const start = end - durationMs;

      const history =
        await api.getPerformanceHistoryRange(
          deviceID,
          {
            from: new Date(start).toISOString(),
            to: new Date(end).toISOString(),
            limit: 25,
          },
          {
            signal,
            maxPages: 4,
          },
        );

      return {
        ...history,
        fromMillis: start,
        toMillis: end,
      };
    },

    staleTime: 30_000,

    refetchInterval: 60_000,
  });
}