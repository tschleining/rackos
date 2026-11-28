// @vitest-environment jsdom

import {
  useLayoutEffect,
} from "react";

import {
  act,
  cleanup,
  render,
} from "@testing-library/react";

import {
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";

import {
  afterEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";

import {
  ProtectedQueryProvider,
} from "../src/features/auth";

const deviceQueryKey = [
  "devices",
  "list",
] as const;

const performanceQueryKey = [
  "performance",
  "history",
] as const;

interface CacheProbeProps {
  capture: (client: QueryClient) => void;
}

/**
 * Capture the query client owned by
 * the current protected generation.
 */
function CacheProbe({
  capture,
}: CacheProbeProps) {
  const client = useQueryClient();

  useLayoutEffect(() => {
    capture(client);
  }, [capture, client]);

  return null;
}

function deferred<T>() {
  let resolve!: (
    value: T | PromiseLike<T>,
  ) => void;

  let reject!: (
    reason?: unknown,
  ) => void;

  const promise = new Promise<T>(
    (resolvePromise, rejectPromise) => {
      resolve = resolvePromise;
      reject = rejectPromise;
    },
  );

  return {
    promise,
    resolve,
    reject,
  };
}

async function flushCleanup(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
  });
}

afterEach(async () => {
  cleanup();

  await Promise.resolve();

  vi.restoreAllMocks();
});

describe("Protected query provider", () => {
  it("provides an independent query client", () => {
    const capture =
      vi.fn<CacheProbeProps["capture"]>();

    render(
      <ProtectedQueryProvider>
        <CacheProbe capture={capture} />
      </ProtectedQueryProvider>,
    );

    expect(capture).toHaveBeenCalledOnce();

    const client =
      capture.mock.calls[0]?.[0];

    expect(client).toBeDefined();

    expect(
      client?.getQueryCache().getAll(),
    ).toHaveLength(0);

    expect(
      client?.getMutationCache().getAll(),
    ).toHaveLength(0);
  });

  it("preserves the query client during ordinary rerenders", () => {
    const capture =
      vi.fn<CacheProbeProps["capture"]>();

    const { rerender } = render(
      <ProtectedQueryProvider>
        <CacheProbe capture={capture} />
      </ProtectedQueryProvider>,
    );

    const client =
      capture.mock.calls[0]?.[0];

    expect(client).toBeDefined();

    client?.setQueryData(
      deviceQueryKey,
      "Cached device data",
    );

    rerender(
      <ProtectedQueryProvider>
        <CacheProbe capture={capture} />
      </ProtectedQueryProvider>,
    );

    expect(
      capture,
    ).toHaveBeenCalledOnce();

    expect(
      client?.getQueryData(
        deviceQueryKey,
      ),
    ).toBe("Cached device data");
  });

  it("clears cached management data after unmount", async () => {
    const capture =
      vi.fn<CacheProbeProps["capture"]>();

    const { unmount } = render(
      <ProtectedQueryProvider>
        <CacheProbe capture={capture} />
      </ProtectedQueryProvider>,
    );

    const client =
      capture.mock.calls[0]?.[0];

    if (!client) {
      throw new Error(
        "Missing protected query client",
      );
    }

    client.setQueryData(
      deviceQueryKey,
      "Previous device data",
    );

    client.setQueryData(
      performanceQueryKey,
      "Previous performance data",
    );

    expect(
      client.getQueryCache().getAll(),
    ).toHaveLength(2);

    unmount();

    await flushCleanup();

    expect(
      client.getQueryCache().getAll(),
    ).toHaveLength(0);

    expect(
      client.getQueryData(
        deviceQueryKey,
      ),
    ).toBeUndefined();

    expect(
      client.getQueryData(
        performanceQueryKey,
      ),
    ).toBeUndefined();
  });

  it("clears retired mutation cache entries", async () => {
    const capture =
      vi.fn<CacheProbeProps["capture"]>();

    const { unmount } = render(
      <ProtectedQueryProvider>
        <CacheProbe capture={capture} />
      </ProtectedQueryProvider>,
    );

    const client =
      capture.mock.calls[0]?.[0];

    if (!client) {
      throw new Error(
        "Missing protected query client",
      );
    }

    // Use a synthetic, unexecuted mutation.
    // Authentication credentials must never
    // enter the mutation cache.
    client.getMutationCache().build(
      client,
      {
        mutationKey: [
          "synthetic-test-mutation",
        ],

        mutationFn: async () =>
          "Synthetic result",
      },
    );

    expect(
      client.getMutationCache().getAll(),
    ).toHaveLength(1);

    unmount();

    await flushCleanup();

    expect(
      client.getMutationCache().getAll(),
    ).toHaveLength(0);
  });

  it("cancels outstanding queries when the generation ends", async () => {
    const capture =
      vi.fn<CacheProbeProps["capture"]>();

    const { unmount } = render(
      <ProtectedQueryProvider>
        <CacheProbe capture={capture} />
      </ProtectedQueryProvider>,
    );

    const client =
      capture.mock.calls[0]?.[0];

    if (!client) {
      throw new Error(
        "Missing protected query client",
      );
    }

    const pending = deferred<string>();

    let requestSignal:
      AbortSignal | undefined;

    const request = client.fetchQuery({
      queryKey: deviceQueryKey,

      queryFn: ({ signal }) => {
        requestSignal = signal;

        return pending.promise;
      },
    });

    // Attach a rejection handler before
    // cancellation to avoid an unhandled
    // rejected promise.
    const settled = request.catch(
      () => undefined,
    );

    expect(
      requestSignal?.aborted,
    ).toBe(false);

    unmount();

    await flushCleanup();

    expect(
      requestSignal?.aborted,
    ).toBe(true);

    await settled;

    expect(
      client.getQueryCache().getAll(),
    ).toHaveLength(0);

    pending.resolve(
      "Delayed device data",
    );

    await flushCleanup();

    expect(
      client.getQueryCache().getAll(),
    ).toHaveLength(0);
  });

  it("preserves the active cache during Strict Mode effect replay", async () => {
    const initialized =
      new Set<QueryClient>();

    const capture = vi.fn(
      (client: QueryClient) => {
        if (!initialized.has(client)) {
          initialized.add(client);

          client.setQueryData(
            deviceQueryKey,
            "Active session data",
          );
        }
      },
    );

    const { unmount } = render(
      <ProtectedQueryProvider>
        <CacheProbe capture={capture} />
      </ProtectedQueryProvider>,
      {
        reactStrictMode: true,
      },
    );

    await flushCleanup();

    expect(
      capture,
    ).toHaveBeenCalledTimes(2);

    expect(initialized.size).toBe(1);

    const client =
      capture.mock.calls[0]?.[0];

    expect(
      client?.getQueryData(
        deviceQueryKey,
      ),
    ).toBe("Active session data");

    unmount();

    await flushCleanup();

    expect(
      client?.getQueryCache().getAll(),
    ).toHaveLength(0);
  });
});

describe("Authenticated query generation isolation", () => {
  it("creates a new empty cache when the generation changes", async () => {
    const capture =
      vi.fn<CacheProbeProps["capture"]>();

    const { rerender } = render(
      <ProtectedQueryProvider key={1}>
        <CacheProbe capture={capture} />
      </ProtectedQueryProvider>,
    );

    const previousClient =
      capture.mock.calls[0]?.[0];

    if (!previousClient) {
      throw new Error(
        "Missing previous query client",
      );
    }

    previousClient.setQueryData(
      deviceQueryKey,
      "Previous session device data",
    );

    rerender(
      <ProtectedQueryProvider key={2}>
        <CacheProbe capture={capture} />
      </ProtectedQueryProvider>,
    );

    const currentClient =
      capture.mock.calls[1]?.[0];

    expect(currentClient).toBeDefined();

    expect(currentClient).not.toBe(
      previousClient,
    );

    expect(
      currentClient?.getQueryCache()
        .getAll(),
    ).toHaveLength(0);

    await flushCleanup();

    expect(
      previousClient.getQueryCache()
        .getAll(),
    ).toHaveLength(0);
  });

  it("does not allow a delayed response to repopulate a retired cache", async () => {
    const capture =
      vi.fn<CacheProbeProps["capture"]>();

    const { rerender } = render(
      <ProtectedQueryProvider key={1}>
        <CacheProbe capture={capture} />
      </ProtectedQueryProvider>,
    );

    const previousClient =
      capture.mock.calls[0]?.[0];

    if (!previousClient) {
      throw new Error(
        "Missing previous query client",
      );
    }

    const pending = deferred<string>();

    let previousSignal:
      AbortSignal | undefined;

    const previousRequest =
      previousClient.fetchQuery({
        queryKey: deviceQueryKey,

        queryFn: ({ signal }) => {
          previousSignal = signal;

          // Deliberately ignore cancellation.
          // This simulates a transport that
          // completes despite an abort.
          return pending.promise;
        },
      });

    const settled =
      previousRequest.catch(
        () => undefined,
      );

    rerender(
      <ProtectedQueryProvider key={2}>
        <CacheProbe capture={capture} />
      </ProtectedQueryProvider>,
    );

    const currentClient =
      capture.mock.calls[1]?.[0];

    if (!currentClient) {
      throw new Error(
        "Missing current query client",
      );
    }

    currentClient.setQueryData(
      deviceQueryKey,
      "Current session device data",
    );

    await flushCleanup();

    expect(
      previousSignal?.aborted,
    ).toBe(true);

    await settled;

    // Complete the previous request after
    // the new generation becomes active.
    pending.resolve(
      "Obsolete session device data",
    );

    await flushCleanup();

    expect(
      previousClient.getQueryCache()
        .getAll(),
    ).toHaveLength(0);

    expect(
      currentClient.getQueryData(
        deviceQueryKey,
      ),
    ).toBe(
      "Current session device data",
    );
  });

  it("does not cancel requests owned by the new generation", async () => {
    const capture =
      vi.fn<CacheProbeProps["capture"]>();

    const { rerender } = render(
      <ProtectedQueryProvider key={1}>
        <CacheProbe capture={capture} />
      </ProtectedQueryProvider>,
    );

    rerender(
      <ProtectedQueryProvider key={2}>
        <CacheProbe capture={capture} />
      </ProtectedQueryProvider>,
    );

    const currentClient =
      capture.mock.calls[1]?.[0];

    if (!currentClient) {
      throw new Error(
        "Missing current query client",
      );
    }

    const pending = deferred<string>();

    let currentSignal:
      AbortSignal | undefined;

    const request =
      currentClient.fetchQuery({
        queryKey: deviceQueryKey,

        queryFn: ({ signal }) => {
          currentSignal = signal;

          return pending.promise;
        },
      });

    await flushCleanup();

    expect(
      currentSignal?.aborted,
    ).toBe(false);

    pending.resolve(
      "Current device response",
    );

    await expect(
      request,
    ).resolves.toBe(
      "Current device response",
    );

    expect(
      currentClient.getQueryData(
        deviceQueryKey,
      ),
    ).toBe(
      "Current device response",
    );
  });
});