
import {
  describe,
  expect,
  it,
  vi,
} from "vitest";

import {
  RackOSAPIClient,
} from "@rackos/api-client";

function pendingFetch(
  _input: RequestInfo | URL,
  init?: RequestInit,
): Promise<Response> {
  return new Promise(
    (_resolve, reject) => {
      const signal = init?.signal;

      if (!signal) {
        reject(
          new Error("Missing abort signal"),
        );

        return;
      }

      if (signal.aborted) {
        reject(signal.reason);
        return;
      }

      signal.addEventListener(
        "abort",
        () => reject(signal.reason),
        { once: true },
      );
    },
  );
}

describe("Management request cancellation", () => {
  it("does not send an already cancelled request", async () => {
    const fetcher = vi.fn<typeof fetch>();

    const api = new RackOSAPIClient({
      fetch: fetcher,
    });

    const controller =
      new AbortController();

    controller.abort();

    await expect(
      api.getHealth({
        signal: controller.signal,
      }),
    ).rejects.toMatchObject({
      kind: "cancelled",
    });

    expect(fetcher).not.toHaveBeenCalled();
  });

  it("cancels an active request", async () => {
    const api = new RackOSAPIClient({
      fetch: pendingFetch,
    });

    const controller =
      new AbortController();

    const request = api.getHealth({
      signal: controller.signal,
    });

    const assertion =
      expect(request).rejects.toMatchObject({
        kind: "cancelled",
      });

    controller.abort();

    await assertion;
  });

  it("enforces the request timeout", async () => {
    vi.useFakeTimers();

    try {
      const api = new RackOSAPIClient({
        fetch: pendingFetch,
        timeoutMs: 100,
      });

      const request = api.getHealth();

      const assertion =
        expect(request).rejects.toMatchObject({
          kind: "timeout",
        });

      await vi.advanceTimersByTimeAsync(100);

      await assertion;
    } finally {
      vi.useRealTimers();
    }
  });

  it("enforces timeouts during response body retrieval", async () => {
    vi.useFakeTimers();

    try {
      const fetcher: typeof fetch =
        async (_input, init) => {
          const stream =
            new ReadableStream<Uint8Array>({
              start(controller) {
                init?.signal?.addEventListener(
                  "abort",
                  () => {
                            controller.error(
                            init?.signal?.reason,
                            );
                  },
                  { once: true },
                );
              },
            });

          return new Response(
            stream,
            {
              status: 200,

              headers: {
                "Content-Type":
                  "application/json",
              },
            },
          );
        };

      const api = new RackOSAPIClient({
        fetch: fetcher,
        timeoutMs: 100,
      });

      const request = api.getHealth();

      const assertion =
        expect(request).rejects.toMatchObject({
          kind: "timeout",
        });

      await vi.advanceTimersByTimeAsync(100);

      await assertion;
    } finally {
      vi.useRealTimers();
    }
  });
});
