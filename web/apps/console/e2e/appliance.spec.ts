
import {
  expect,
  test,
} from "@playwright/test";

test("embedded console and management API are available", async ({
  page,
  request,
}) => {
  const health = await request.get("/healthz");

  expect(health.status()).toBe(200);
  expect(await health.json()).toEqual({
    status: "healthy",
  });

  const readiness = await request.get("/readyz");

  expect(readiness.status()).toBe(200);
  expect(await readiness.json()).toEqual({
    status: "ready",
  });

  const response = await page.goto("/");

  expect(response?.status()).toBe(200);

  const headers = response?.headers() ?? {};

  expect(headers["content-security-policy"])
    .toContain("script-src 'self'");

  expect(headers["cache-control"])
    .toBe("no-store");

  await expect(
    page.getByRole("heading", {
      name: "Overview",
      exact: true,
    }),
  ).toBeVisible();

  await expect(
    page.getByText("Appliance ready", {
      exact: true,
    }),
  ).toBeVisible();
});

test("empty appliance displays no registered devices", async ({
  page,
  request,
}) => {
  const response = await request.get("/api/v1/devices");

  expect(response.status()).toBe(200);

  expect(await response.json()).toEqual({
    devices: [],
    total: 0,
  });

  await page.goto("/");

  await page.getByRole("link", {
    name: "Devices",
    exact: true,
  }).first().click();

  await expect(page).toHaveURL(/\/devices$/);

  await expect(
    page.getByText("No registered devices", {
      exact: true,
    }),
  ).toBeVisible();

  await expect(
    page.getByText("Showing 0 of 0 devices"),
  ).toBeVisible();

  await page.reload();

  await expect(
    page.getByRole("heading", {
      name: "Devices",
      exact: true,
    }),
  ).toBeVisible();
});

test("system page displays the real appliance version", async ({
  page,
  request,
}) => {
  const response = await request.get("/api/v1/version");

  expect(response.status()).toBe(200);

  const version = await response.json();

  await page.goto("/system");

  await expect(
    page.getByRole("heading", {
      name: "System",
      exact: true,
    }),
  ).toBeVisible();

  await expect(
    page.locator("main").getByText(
      version.version,
      { exact: true },
    ),
  ).toBeVisible();
});

test("malformed and unknown device IDs are handled", async ({
  page,
}) => {
  await page.goto("/devices/invalid-id");

  await expect(
    page.getByRole("heading", {
      name: "Invalid device ID",
      level: 1,
    }),
  ).toBeVisible();

  await page.goto(
    "/devices/11111111-1111-4111-8111-111111111111",
  );

  await expect(
    page.getByRole("heading", {
      name: "Device not found",
      level: 1,
    }),
  ).toBeVisible();
});

test("frontend and API not-found responses remain distinct", async ({
  page,
  request,
}) => {
  await page.goto("/unknown-frontend-route");

  await expect(
    page.getByRole("heading", {
      name: "Page not found",
    }),
  ).toBeVisible();

  const apiResponse = await request.get(
    "/api/v1/unknown",
    {
      headers: {
        Accept: "text/html",
      },
    },
  );

  expect(apiResponse.status()).toBe(404);

  const assetResponse = await request.get(
    "/assets/missing.js",
  );

  expect(assetResponse.status()).toBe(404);

  const writeResponse = await request.post("/");

  expect(writeResponse.status()).toBe(405);
});

test("management security rejects foreign origins", async ({
  request,
}) => {
  const response = await request.get("/", {
    headers: {
      Origin: "https://example.com",
    },
  });

  expect(response.status()).toBe(403);
});
