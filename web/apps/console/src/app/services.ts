import {
  QueryClient,
} from "@tanstack/react-query";

import {
  APIError,
  RackOSAPIClient,
} from "@rackos/api-client";

/**
 * Authentication remains opt-in until the
 * appliance daemon enforces authenticated
 * management access.
 *
 * There is no automatic runtime fallback.
 */
export const authenticationEnabled =
  import.meta.env.VITE_RACKOS_AUTHENTICATION === "true";

type UnauthorizedCapture =
  () => (() => void) | undefined;

interface UnauthorizedBinding {
  capture: UnauthorizedCapture;
}

let unauthorizedBinding:
  UnauthorizedBinding | null = null;

/**
 * Connect the management API client to the
 * currently mounted authentication boundary.
 */
export function bindManagementUnauthorized(
  capture: UnauthorizedCapture,
): () => void {
  const binding: UnauthorizedBinding = {
    capture,
  };

  unauthorizedBinding = binding;

  return () => {
    // An obsolete cleanup must not remove
    // a newer authentication binding.
    if (unauthorizedBinding === binding) {
      unauthorizedBinding = null;
    }
  };
}

export function captureManagementUnauthorized():
  (() => void) | undefined {
  return unauthorizedBinding?.capture();
}

export const api = new RackOSAPIClient({
  authenticatedManagement:
    authenticationEnabled,

  captureManagementUnauthorized,
});

/**
 * Create an independent management query cache.
 *
 * Authenticated application instances must
 * never reuse another session's query cache.
 */
export function createQueryClient(): QueryClient {
  return new QueryClient({
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

      mutations: {
        retry: false,
      },
    },
  });
}

// Preserve the existing query client for the
// legacy, unauthenticated development console.
export const queryClient = createQueryClient();