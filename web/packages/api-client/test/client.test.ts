
import {
  describe,
  expect,
  it,
  vi,
} from "vitest";

import type {
  PerformanceHistoryQuery,
} from "@rackos/api-client";

import {
  APIError,
  RackOSAPIClient,
} from "@rackos/api-client";

import {
  device,
  deviceList,
  fullInventory,
  historyPage,
  performanceRecord,
  summary,
  version,
} from "./contracts.type-test";

function jsonResponse(
  body: unknown,
  status = 200,
): Response {
  return new Response(
    JSON.stringify(body),
    {
      status,

      headers: {
        "Content-Type":
          "application/json; charset=utf-8",
      },
    },
  );
}

function createTestClient() {
  const fetcher = vi.fn<typeof fetch>();

  const api = new RackOSAPIClient({
    fetch: fetcher,
  });

  return { api, fetcher };
}

describe("RackOS API client", () => {
  it("retrieves appliance health", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse({
        status: "healthy",
      }),
    );

    await expect(
      api.getHealth(),
    ).resolves.toEqual({
      status: "healthy",
    });

    expect(fetcher).toHaveBeenCalledWith(
      "/healthz",
      expect.objectContaining({
        method: "GET",
        mode: "same-origin",
        cache: "no-store",
        credentials: "omit",
        redirect: "error",

        headers: {
          Accept: "application/json",
        },

        signal: expect.any(AbortSignal),
      }),
    );
  });

  it("accepts an unavailable readiness response", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(
        { status: "not_ready" },
        503,
      ),
    );

    await expect(
      api.getReadiness(),
    ).resolves.toEqual({
      status: "not_ready",
    });
  });

  it("rejects inconsistent readiness responses", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(
        { status: "ready" },
        503,
      ),
    );

    await expect(
      api.getReadiness(),
    ).rejects.toMatchObject({
      kind: "invalid-response",
    });
  });

  it("retrieves appliance version", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(version),
    );

    await expect(
      api.getVersion(),
    ).resolves.toEqual(version);
  });

  it("retrieves registered devices", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(deviceList),
    );

    await expect(
      api.getDevices(),
    ).resolves.toEqual(deviceList);
  });

  it("retrieves an empty device list", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse({
        devices: [],
        total: 0,
      }),
    );

    await expect(
      api.getDevices(),
    ).resolves.toEqual({
      devices: [],
      total: 0,
    });
  });

  it("retrieves device summary", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(summary),
    );

    await expect(
      api.getDeviceSummary(),
    ).resolves.toEqual(summary);
  });

  it("rejects inconsistent device summary", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse({
        total: 5,
        online: 1,
        offline: 1,
      }),
    );

    await expect(
      api.getDeviceSummary(),
    ).rejects.toMatchObject({
      kind: "invalid-response",
    });
  });

  it("retrieves a specific device", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(device),
    );

    await expect(
      api.getDevice(device.id),
    ).resolves.toEqual(device);
  });

  it("rejects a response for another device", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse({
        ...device,

        id:
          "22222222-2222-4222-8222-222222222222",
      }),
    );

    await expect(
      api.getDevice(device.id),
    ).rejects.toMatchObject({
      kind: "invalid-response",
    });
  });

  it("rejects invalid device IDs before sending requests", async () => {
    const { api, fetcher } =
      createTestClient();

    await expect(
      api.getDevice("../summary"),
    ).rejects.toMatchObject({
      kind: "invalid-request",
    });

    expect(fetcher).not.toHaveBeenCalled();
  });

  it("retrieves hardware inventory", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(fullInventory),
    );

    await expect(
      api.getInventory(device.id),
    ).resolves.toEqual(fullInventory);
  });

  it("returns null for missing inventory", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(
        {
          error:
            "no inventory snapshot found",
        },
        404,
      ),
    );

    await expect(
      api.getInventory(device.id),
    ).resolves.toBeNull();
  });

  it("does not hide unknown-device errors", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(
        { error: "device not found" },
        404,
      ),
    );

    await expect(
      api.getInventory(device.id),
    ).rejects.toMatchObject({
      kind: "http",
      status: 404,
      serverMessage: "device not found",
    });
  });

  it("preserves exact performance counters", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(performanceRecord),
    );

    const result =
      await api.getLatestPerformance(
        device.id,
      );

    expect(result).toEqual(
      performanceRecord,
    );

    expect(
      result?.snapshot.network
        .interfaces[0]?.received_bytes,
    ).toBe("9007199254740993");

    expect(
      result?.snapshot.network
        .interfaces[0]?.receive_dropped,
    ).toBe("0");
  });

  it("returns null when no performance samples exist", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(
        {
          error:
            "no performance samples found",
        },
        404,
      ),
    );

    await expect(
      api.getLatestPerformance(device.id),
    ).resolves.toBeNull();
  });

  it("rejects numeric 64-bit counter values", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse({
        ...performanceRecord,

        snapshot: {
          ...performanceRecord.snapshot,

          memory: {
            ...performanceRecord.snapshot.memory,

            total_bytes: 17179869184,
          },
        },
      }),
    );

    await expect(
      api.getLatestPerformance(device.id),
    ).rejects.toMatchObject({
      kind: "invalid-response",
    });
  });

  it("rejects unsupported payload versions", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse({
        ...performanceRecord,
        payload_version: 2,
      }),
    );

    await expect(
      api.getLatestPerformance(device.id),
    ).rejects.toMatchObject({
      kind: "invalid-response",
    });
  });

  it("handles JSON HTTP errors", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(
        {
          error: "failed to list devices",
        },
        500,
      ),
    );

    await expect(
      api.getDevices(),
    ).rejects.toMatchObject({
      kind: "http",
      status: 500,

      serverMessage:
        "failed to list devices",
    });
  });

  it("handles non-JSON proxy errors", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      new Response(
        "Bad gateway",
        { status: 502 },
      ),
    );

    await expect(
      api.getDevices(),
    ).rejects.toMatchObject({
      kind: "http",
      status: 502,
      serverMessage: null,
    });
  });

  it("rejects malformed successful JSON", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      new Response(
        "{invalid",
        {
          status: 200,

          headers: {
            "Content-Type":
              "application/json",
          },
        },
      ),
    );

    await expect(
      api.getDevices(),
    ).rejects.toMatchObject({
      kind: "invalid-response",
    });
  });

  it("rejects HTML returned instead of JSON", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      new Response(
        "<html>Unexpected page</html>",
        {
          status: 200,

          headers: {
            "Content-Type": "text/html",
          },
        },
      ),
    );

    await expect(
      api.getDevices(),
    ).rejects.toMatchObject({
      kind: "invalid-response",
    });
  });

  it("classifies network failures", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockRejectedValue(
      new TypeError("Connection refused"),
    );

    await expect(
      api.getDevices(),
    ).rejects.toMatchObject({
      kind: "network",
    });
  });

  it("rejects invalid timeout configuration", () => {
    expect(
      () =>
        new RackOSAPIClient({
          timeoutMs: 0,
        }),
    ).toThrow(APIError);
  });
});

describe("Historical performance requests", () => {
  it("encodes historical query parameters", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse(historyPage),
    );

    await api.getPerformanceHistory(
      device.id,
      {
        from: "2026-11-27T11:00:00+00:00",
        to: "2026-11-27T13:00:00Z",
        limit: 25,
        cursor: "opaque-cursor",
      },
    );

    const url = new URL(
      String(fetcher.mock.calls[0]?.[0]),
      "http://127.0.0.1",
    );

    expect(url.pathname).toBe(
      `/api/v1/devices/${device.id}/performance/history`,
    );

    expect(
      url.searchParams.get("from"),
    ).toBe(
      "2026-11-27T11:00:00+00:00",
    );

    expect(
      url.searchParams.get("to"),
    ).toBe(
      "2026-11-27T13:00:00Z",
    );

    expect(
      url.searchParams.get("limit"),
    ).toBe("25");

    expect(
      url.searchParams.get("cursor"),
    ).toBe("opaque-cursor");
  });



it("rejects invalid historical queries", async () => {
  const { api, fetcher } =
    createTestClient();

  const queries: PerformanceHistoryQuery[] = [
    { limit: 0 },
    { limit: 26 },
    { limit: 1.5 },
    { cursor: "" },
    { from: "invalid" },
    {
      from: "2026-02-30T12:00:00Z",
    },
    {
      from:
        "2026-11-27T12:00:00.123456Z",
    },
    {
      from: "2026-11-27T13:00:00Z",
      to: "2026-11-27T12:00:00Z",
    },
  ];

  for (const query of queries) {
    await expect(
      api.getPerformanceHistory(
        device.id,
        query,
      ),
    ).rejects.toMatchObject({
      kind: "invalid-request",
    });
  }

  expect(fetcher).not.toHaveBeenCalled();
});


  it("rejects inconsistent pagination metadata", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse({
        ...historyPage,
        has_more: true,
        next_cursor: null,
      }),
    );

    await expect(
      api.getPerformanceHistory(device.id),
    ).rejects.toMatchObject({
      kind: "invalid-response",
    });
  });

  it("returns empty historical ranges", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValue(
      jsonResponse({
        device_id: device.id,
        samples: [],
        count: 0,
        has_more: false,
        next_cursor: null,
      }),
    );

    await expect(
      api.getPerformanceHistoryRange(
        device.id,
      ),
    ).resolves.toEqual({
      samples: [],
      pagesFetched: 1,
      hasMore: false,
      nextCursor: null,
    });
  });

  it("combines pages and deduplicates samples", async () => {
    const { api, fetcher } =
      createTestClient();

    const secondSample = {
      ...performanceRecord,

      sample_id:
        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    };

    fetcher
      .mockResolvedValueOnce(
        jsonResponse({
          ...historyPage,

          has_more: true,
          next_cursor: "cursor-one",
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          ...historyPage,

          samples: [
            performanceRecord,
            secondSample,
          ],

          count: 2,
        }),
      );

    const result =
      await api.getPerformanceHistoryRange(
        device.id,
        {
          from: "2026-11-27T11:00:00Z",
          to: "2026-11-27T13:00:00Z",
        },
      );

    expect(result.samples).toEqual([
      performanceRecord,
      secondSample,
    ]);

    expect(result.pagesFetched).toBe(2);
    expect(result.hasMore).toBe(false);
    expect(result.nextCursor).toBeNull();

    const secondURL = new URL(
      String(fetcher.mock.calls[1]?.[0]),
      "http://127.0.0.1",
    );

    expect(
      secondURL.searchParams.get("cursor"),
    ).toBe("cursor-one");

    expect(
      secondURL.searchParams.get("from"),
    ).toBe("2026-11-27T11:00:00Z");

    expect(
      secondURL.searchParams.get("to"),
    ).toBe("2026-11-27T13:00:00Z");
  });

  it("enforces the historical page budget", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher.mockResolvedValueOnce(
      jsonResponse({
        ...historyPage,

        has_more: true,
        next_cursor: "cursor-one",
      }),
    );

    const result =
      await api.getPerformanceHistoryRange(
        device.id,
        {},
        { maxPages: 1 },
      );

    expect(fetcher).toHaveBeenCalledTimes(1);

    expect(result).toEqual({
      samples: [performanceRecord],
      pagesFetched: 1,
      hasMore: true,
      nextCursor: "cursor-one",
    });
  });

  it("detects repeated continuation cursors", async () => {
    const { api, fetcher } =
      createTestClient();

    fetcher
      .mockResolvedValueOnce(
        jsonResponse({
          ...historyPage,

          has_more: true,
          next_cursor: "repeated-cursor",
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          ...historyPage,

          has_more: true,
          next_cursor: "repeated-cursor",
        }),
      );

    await expect(
      api.getPerformanceHistoryRange(
        device.id,
      ),
    ).rejects.toMatchObject({
      kind: "invalid-response",
    });

    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it("rejects invalid historical page budgets", async () => {
    const { api, fetcher } =
      createTestClient();

    await expect(
      api.getPerformanceHistoryRange(
        device.id,
        {},
        { maxPages: 11 },
      ),
    ).rejects.toMatchObject({
      kind: "invalid-request",
    });

    expect(fetcher).not.toHaveBeenCalled();
  });
});
