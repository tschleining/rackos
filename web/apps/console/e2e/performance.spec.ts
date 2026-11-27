
import {
  expect,
  test,
} from "@playwright/test";

import type {
  Device,
  PerformanceRecord,
} from "@rackos/api-client";

import { devices } from "../test/fixtures";

function createSample(
  deviceID: string,
  timestamp: number,
  sampleNumber: number,
  cpuPercent: number,
): PerformanceRecord {
  const collectedAt =
    new Date(timestamp).toISOString();

  return {
    device_id: deviceID,

    sample_id: sampleNumber
      .toString(16)
      .padStart(32, "0"),

    collected_at: collectedAt,
    received_at: collectedAt,
    payload_version: 1,

    snapshot: {
      collected_at: collectedAt,

      cpu: {
        usage_percent: cpuPercent,
        sample_interval_millis: "1000",
      },

      memory: {
        total_bytes: "1073741824",
        available_bytes: "536870912",
        used_bytes: "536870912",
        usage_percent: 50,
      },

      storage: {
        filesystems: [],
        disks: [],
        disk_rates: [],
      },

      network: {
        interfaces: [],
        interface_rates: [],
      },
    },
  };
}

test("production performance page renders historical charts", async ({
  page,
}) => {
  const now = Date.now();

  const timestamp =
    new Date(now).toISOString();

  const device: Device = {
    ...devices[2]!,
    status: "online",
    updated_at: timestamp,
    last_seen_at: timestamp,
  };

  const samples = [
    createSample(
      device.id,
      now - 60_000,
      2,
      35,
    ),
    createSample(
      device.id,
      now - 120_000,
      1,
      20,
    ),
  ];

  const pageErrors: string[] = [];
  const cspErrors: string[] = [];

  page.on("pageerror", (error) => {
    pageErrors.push(error.message);
  });

  page.on("console", (message) => {
    if (
      message.type() === "error" &&
      /content security policy/i.test(message.text())
    ) {
      cspErrors.push(message.text());
    }
  });

  await page.route(
    `**/api/v1/devices/${device.id}`,
    async (route) => {
      await route.fulfill({
        status: 200,
        json: device,
      });
    },
  );

  await page.route(
    `**/api/v1/devices/${device.id}/performance/latest`,
    async (route) => {
      await route.fulfill({
        status: 200,
        json: samples[0]!,
      });
    },
  );

  await page.route(
    /\/performance\/history\?/,
    async (route) => {
      const url = new URL(
        route.request().url(),
      );

      const from = Date.parse(
        url.searchParams.get("from") ?? "",
      );

      const to = Date.parse(
        url.searchParams.get("to") ?? "",
      );

      const matching = samples.filter((sample) => {
        const collected =
          Date.parse(sample.collected_at);

        return collected >= from &&
          collected < to;
      });

      await route.fulfill({
        status: 200,

        json: {
          device_id: device.id,
          samples: matching,
          count: matching.length,
          has_more: false,
          next_cursor: null,
        },
      });
    },
  );

  await page.goto(
    `/devices/${device.id}/performance`,
  );

  await expect(
    page.getByRole("heading", {
      name: "Latest performance",
      exact: true,
    }),
  ).toBeVisible();

  const history = page.locator(
    "#historical-performance",
  );

  await expect(history).toBeVisible();

  await expect(
    history.locator("path.recharts-line-curve"),
  ).toHaveCount(2);

  await expect(
    history.locator("svg.recharts-surface").first(),
  ).toBeVisible();

  const rangeRequest = page.waitForRequest(
    (request) => {
      const url = new URL(request.url());

      if (!url.pathname.endsWith(
        "/performance/history",
      )) {
        return false;
      }

      const from = Date.parse(
        url.searchParams.get("from") ?? "",
      );

      const to = Date.parse(
        url.searchParams.get("to") ?? "",
      );

      return to - from === 15 * 60_000;
    },
  );

  await history.getByRole("combobox").first()
    .selectOption(String(15 * 60_000));

  await rangeRequest;

  await expect(
    history.locator("path.recharts-line-curve"),
  ).toHaveCount(2);

  expect(pageErrors).toEqual([]);
  expect(cspErrors).toEqual([]);
});
