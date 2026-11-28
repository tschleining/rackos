import {
  fileURLToPath,
} from "node:url";

import {
  join,
} from "node:path";

import {
  expect,
  test,
  type BrowserContext,
  type Page,
} from "@playwright/test";

import type {
  AdministratorSession,
} from "@rackos/api-client";

const ORIGIN = "https://rackos.test";

const DIST_DIRECTORY = fileURLToPath(
  new URL("../dist/", import.meta.url),
);

const credentials = {
  username: "admin",
  password: "synthetic-test-password",
};

function createSession(): AdministratorSession {
  const now = Date.now();

  return {
    administrator_id:
      "12345678-1234-4000-8000-123456789abc",

    created_at:
      new Date(now).toISOString(),

    last_activity_at:
      new Date(now).toISOString(),

    expires_at:
      new Date(
        now + 12 * 60 * 60_000,
      ).toISOString(),

    idle_expires_at:
      new Date(
        now + 30 * 60_000,
      ).toISOString(),
  };
}

/**
 * A controlled authentication API fixture.
 *
 * All authentication state is synthetic.
 * No real appliance is contacted.
 */
async function installMockAppliance(
  context: BrowserContext,
) {
  const fixture = {
    session:
      null as AdministratorSession | null,

    unavailable: false,

    sessionReads: 0,

    loginRequests: 0,

    logoutRequests: 0,

    activityRequests: 0,

    deviceReads: 0,

    unauthorizedDeviceReads: 0,
  };

  await context.route(
    "**/*",
    async (route) => {
      const request = route.request();

      const url = new URL(
        request.url(),
      );

      if (url.origin !== ORIGIN) {
        await route.abort();

        return;
      }

      const path = url.pathname;

      const method = request.method();

      async function json(
        status: number,
        body: object,
      ): Promise<void> {
        await route.fulfill({
          status,

          json: body,

          headers: {
            "Cache-Control": "no-store",
          },
        });
      }

      async function unauthorized() {
        await json(401, {
          error:
            "authentication required",
        });
      }

      if (
        path.startsWith("/api/v1/auth/")
      ) {
        if (fixture.unavailable) {
          await json(503, {
            error:
              "authentication unavailable",
          });

          return;
        }

        if (
          method === "POST" &&
          request.headers()[
            "x-rackos-csrf"
          ] !== "1"
        ) {
          await json(403, {
            error: "request forbidden",
          });

          return;
        }
      }

      switch (path) {
        case "/api/v1/auth/session": {
          fixture.sessionReads++;

          if (method !== "GET") {
            await json(405, {
              error: "method not allowed",
            });

            return;
          }

          if (fixture.session === null) {
            await unauthorized();
          } else {
            await json(
              200,
              fixture.session,
            );
          }

          return;
        }

        case "/api/v1/auth/login": {
          fixture.loginRequests++;

          if (method !== "POST") {
            await json(405, {
              error: "method not allowed",
            });

            return;
          }

          const body =
            request.postDataJSON();

          if (
            body?.username !==
              credentials.username ||

            body?.password !==
              credentials.password
          ) {
            await json(401, {
              error: "invalid credentials",
            });

            return;
          }

          fixture.session =
            createSession();

          await json(
            200,
            fixture.session,
          );

          return;
        }

        case "/api/v1/auth/logout": {
          fixture.logoutRequests++;

          if (method !== "POST") {
            await json(405, {
              error: "method not allowed",
            });

            return;
          }

          if (fixture.session === null) {
            await unauthorized();

            return;
          }

          fixture.session = null;

          await route.fulfill({
            status: 204,
          });

          return;
        }

        case "/api/v1/auth/activity": {
          fixture.activityRequests++;

          if (method !== "POST") {
            await json(405, {
              error: "method not allowed",
            });

            return;
          }

          if (fixture.session === null) {
            await unauthorized();

            return;
          }

          const now = Math.max(
            Date.now(),

            Date.parse(
              fixture.session.last_activity_at,
            ),
          );

          const expires = Date.parse(
            fixture.session.expires_at,
          );

          if (now >= expires) {
            fixture.session = null;

            await unauthorized();

            return;
          }

          fixture.session = {
            ...fixture.session,

            last_activity_at:
              new Date(now).toISOString(),

            idle_expires_at:
              new Date(
                Math.min(
                  now + 30 * 60_000,
                  expires,
                ),
              ).toISOString(),
          };

          await json(
            200,
            fixture.session,
          );

          return;
        }

        case "/healthz":
          await json(200, {
            status: "healthy",
          });

          return;

        case "/readyz":
          await json(200, {
            status: "ready",
          });

          return;

        case "/api/v1/version":
          await json(200, {
            name: "RackOS",
            version: "0.1.0",
          });

          return;

        case "/api/v1/devices":
          fixture.deviceReads++;

          if (fixture.session === null) {
            fixture.unauthorizedDeviceReads++;

            await unauthorized();

            return;
          }

          await json(200, {
            devices: [],
            total: 0,
          });

          return;

        case "/":
        case "/devices":
        case "/system":
        case "/index.html":
          await route.fulfill({
            status: 200,

            contentType: "text/html",

            path: join(
              DIST_DIRECTORY,
              "index.html",
            ),

            headers: {
              "Cache-Control": "no-store",
            },
          });

          return;
      }

      // Serve only generated build assets.
      // Do not expose arbitrary filesystem
      // paths through the test fixture.
      if (
        /^\/assets\/[A-Za-z0-9_-][A-Za-z0-9_.-]*\.(js|css|woff2|svg)$/.test(
          path,
        )
      ) {
        await route.fulfill({
          status: 200,

          path: join(
            DIST_DIRECTORY,
            path.slice(1),
          ),
        });

        return;
      }

      await route.fulfill({
        status: 404,
        body: "Not found",
      });
    },
  );

  return fixture;
}

async function signIn(
  page: Page,
): Promise<void> {
  await page.getByLabel(
    "Username",
  ).fill(credentials.username);

  await page.getByLabel(
    "Password",
  ).fill(credentials.password);

  await page.getByRole(
    "button",
    {
      name: "Sign in",
      exact: true,
    },
  ).click();
}

function loginHeading(page: Page) {
  return page.getByRole("heading", {
    name: "Administrator sign in",
  });
}

function navigation(page: Page) {
  return page.getByRole("navigation", {
    name: "Main navigation",
  });
}

function devicesHeading(page: Page) {
  return page.getByRole("heading", {
    name: "Devices",
    exact: true,
  });
}

test("protects deep links until authentication succeeds", async ({
  context,
  page,
}) => {
  const fixture =
    await installMockAppliance(context);

  await page.goto("/devices");

  await expect(
    loginHeading(page),
  ).toBeVisible();

  await expect(
    navigation(page),
  ).toHaveCount(0);

  expect(
    fixture.deviceReads,
  ).toBe(0);

  await signIn(page);

  await expect(
    devicesHeading(page),
  ).toBeVisible();

  await expect(page).toHaveURL(
    `${ORIGIN}/devices`,
  );

  await expect(
    page.getByText(
      "No registered devices",
    ),
  ).toBeVisible();

  await expect(
    navigation(page),
  ).toBeVisible();

  await expect(
    page.getByLabel("Password"),
  ).toHaveCount(0);

  expect(
    fixture.loginRequests,
  ).toBe(1);

  expect(
    fixture.unauthorizedDeviceReads,
  ).toBe(0);

  await page.reload();

  await expect(
    devicesHeading(page),
  ).toBeVisible();

  expect(
    fixture.loginRequests,
  ).toBe(1);
});

test("fails closed during authentication service failure", async ({
  context,
  page,
}) => {
  const fixture =
    await installMockAppliance(context);

  fixture.unavailable = true;

  await page.goto("/devices");

  await expect(
    page.getByRole("heading", {
      name: "Authentication unavailable",
    }),
  ).toBeVisible();

  await expect(
    navigation(page),
  ).toHaveCount(0);

  expect(
    fixture.deviceReads,
  ).toBe(0);

  fixture.unavailable = false;

  await page.getByRole("button", {
    name: "Check session again",
  }).click();

  await expect(
    loginHeading(page),
  ).toBeVisible();

  expect(
    fixture.deviceReads,
  ).toBe(0);
});

test("renews activity only for qualifying browser interaction", async ({
  context,
  page,
}) => {
  const fixture =
    await installMockAppliance(context);

  await page.clock.install();

  await page.goto("/devices");

  await signIn(page);

  await expect(
    page.getByText(
      "No registered devices",
    ),
  ).toBeVisible();

  expect(
    fixture.activityRequests,
  ).toBe(0);

  const initialReads =
    fixture.deviceReads;

  // Advance through a dashboard polling
  // interval without administrator input.
  await page.clock.fastForward(16_000);

  await expect.poll(
    () => fixture.deviceReads,
  ).toBeGreaterThan(initialReads);

  expect(
    fixture.activityRequests,
  ).toBe(0);

  // Synthetic DOM events must not renew
  // administrator activity.
  await page.evaluate(() => {
    window.dispatchEvent(
      new MouseEvent("click"),
    );

    window.dispatchEvent(
      new KeyboardEvent("keydown"),
    );
  });

  expect(
    fixture.activityRequests,
  ).toBe(0);

  // Playwright performs genuine browser
  // pointer interaction.
  await devicesHeading(page).click();

  await expect.poll(
    () => fixture.activityRequests,
  ).toBe(1);

  await devicesHeading(page).click();

  await page.keyboard.press("Tab");

  expect(
    fixture.activityRequests,
  ).toBe(1);

  // Allow another activity attempt.
  // No automatic renewal should occur.
  await page.clock.fastForward(60_000);

  expect(
    fixture.activityRequests,
  ).toBe(1);

  await page.keyboard.press("Tab");

  await expect.poll(
    () => fixture.activityRequests,
  ).toBe(2);
});

test("locks protected content after unauthorized management polling", async ({
  context,
  page,
}) => {
  const fixture =
    await installMockAppliance(context);

  await page.clock.install();

  await page.goto("/devices");

  await signIn(page);

  await expect(
    page.getByText(
      "No registered devices",
    ),
  ).toBeVisible();

  // Simulate appliance-side revocation
  // without a browser logout operation.
  fixture.session = null;

  await page.clock.fastForward(16_000);

  await expect(
    loginHeading(page),
  ).toBeVisible();

  await expect(
    navigation(page),
  ).toHaveCount(0);

  expect(
    fixture.unauthorizedDeviceReads,
  ).toBeGreaterThan(0);

  expect(
    fixture.logoutRequests,
  ).toBe(0);
});

test("synchronizes authentication between browser tabs", async ({
  context,
  page,
}) => {
  const fixture =
    await installMockAppliance(context);

  const secondPage =
    await context.newPage();

  try {
    await page.goto("/devices");

    await secondPage.goto("/devices");

    await expect(
      loginHeading(page),
    ).toBeVisible();

    await expect(
      loginHeading(secondPage),
    ).toBeVisible();

    await page.bringToFront();

    await signIn(page);

    await expect(
      devicesHeading(page),
    ).toBeVisible();

    // The second tab recognizes the session
    // change through the native messaging
    // channel and its own verification.
    await expect(
      devicesHeading(secondPage),
    ).toBeVisible();

    expect(
      fixture.loginRequests,
    ).toBe(1);

    await page.getByRole("button", {
      name: "Sign out",
      exact: true,
    }).click();

    await expect(
      loginHeading(page),
    ).toBeVisible();

    await expect(
      loginHeading(secondPage),
    ).toBeVisible();

    await expect(
      navigation(secondPage),
    ).toHaveCount(0);

    expect(
      fixture.logoutRequests,
    ).toBe(1);
  } finally {
    await secondPage.close();
  }
});