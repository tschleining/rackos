import {
  useLayoutEffect,
  useRef,
  useState,
  type PropsWithChildren,
} from "react";

import {
  QueryClientProvider,
} from "@tanstack/react-query";

import {
  createQueryClient,
} from "../../app/services";

/**
 * Own an independent management query cache
 * for a single authenticated generation.
 *
 * The parent authentication boundary must
 * key this component by sessionGeneration.
 */
export function ProtectedQueryProvider({
  children,
}: PropsWithChildren) {
  const [client] = useState(
    createQueryClient,
  );

  const activeLease =
    useRef<object | null>(null);

  useLayoutEffect(() => {
    const lease = {};

    activeLease.current = lease;

    return () => {
      /**
       * React Strict Mode can immediately
       * repeat effect setup after cleanup.
       *
       * Defer cache disposal until that
       * synchronous lifecycle has settled.
       */
      queueMicrotask(() => {
        if (
          activeLease.current !== lease
        ) {
          return;
        }

        activeLease.current = null;

        // Cancellation aborts outstanding
        // queries and their request signals.
        void client.cancelQueries();

        // Remove all query and mutation
        // entries owned by this generation.
        client.clear();
      });
    };
  }, [client]);

  return (
    <QueryClientProvider client={client}>
      {children}
    </QueryClientProvider>
  );
}