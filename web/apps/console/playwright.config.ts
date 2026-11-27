
import { fileURLToPath } from "node:url";

import {
  defineConfig,
  devices,
} from "@playwright/test";

const rootDirectory = fileURLToPath(
  new URL("../../..", import.meta.url),
);

const httpsEnabled =
  process.env.RACKOS_E2E_HTTPS === "true";

if (
  httpsEnabled &&
  !process.env.NODE_EXTRA_CA_CERTS
) {
  throw new Error(
    "HTTPS browser tests require a trusted management CA",
  );
}

const scheme = httpsEnabled
  ? "https"
  : "http";

const baseURL =
  `${scheme}://127.0.0.1:18780`;

export default defineConfig({
  testDir: "./e2e",

  fullyParallel: false,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  timeout: 30_000,

  expect: {
    timeout: 10_000,
  },

  reporter: "list",

  use: {
    baseURL,

    ignoreHTTPSErrors: false,

    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },

  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
      },
    },
  ],

  // HTTPS tests use the isolated launcher to
  // initialize certificate trust before Playwright
  // and its browser processes start.
  webServer: httpsEnabled
    ? undefined
    : {
        command: "./scripts/test-console-server.sh",
        cwd: rootDirectory,
        url: `${baseURL}/readyz`,
        timeout: 60_000,
        reuseExistingServer: false,

        gracefulShutdown: {
          signal: "SIGTERM",
          timeout: 15_000,
        },

        stdout: "pipe",
        stderr: "pipe",
      },
});
