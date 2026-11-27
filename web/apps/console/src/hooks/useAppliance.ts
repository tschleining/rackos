
import { useQuery } from "@tanstack/react-query";

import { api } from "../app/services";

export const applianceKeys = {
  health: ["appliance", "health"] as const,
  readiness: ["appliance", "readiness"] as const,
  version: ["appliance", "version"] as const,
};

export function useApplianceHealth() {
  return useQuery({
    queryKey: applianceKeys.health,

    queryFn: ({ signal }) =>
      api.getHealth({ signal }),

    refetchInterval: 15_000,
  });
}

export function useApplianceReadiness() {
  return useQuery({
    queryKey: applianceKeys.readiness,

    queryFn: ({ signal }) =>
      api.getReadiness({ signal }),

    refetchInterval: 15_000,
  });
}

export function useApplianceVersion() {
  return useQuery({
    queryKey: applianceKeys.version,

    queryFn: ({ signal }) =>
      api.getVersion({ signal }),

    staleTime: Infinity,
  });
}
