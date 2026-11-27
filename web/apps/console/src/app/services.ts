
import { QueryClient } from "@tanstack/react-query";

import {
  APIError,
  RackOSAPIClient,
} from "@rackos/api-client";

export const api = new RackOSAPIClient();

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      gcTime: 5 * 60_000,

      refetchOnWindowFocus: true,
      refetchOnReconnect: true,
      refetchIntervalInBackground: false,

      retry: (failureCount, error) => {
        if (failureCount >= 1) {
          return false;
        }

        if (!(error instanceof APIError)) {
          return false;
        }

        if (
          error.kind === "network" ||
          error.kind === "timeout"
        ) {
          return true;
        }

        return (
          error.kind === "http" &&
          (
            error.status === 502 ||
            error.status === 503 ||
            error.status === 504
          )
        );
      },

      retryDelay: 1_000,
    },
  },
});
