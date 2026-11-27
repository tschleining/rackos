import { fileURLToPath } from "node:url";

import {
  defineConfig,
  devices,
} from "@playwright/test";

const rootDirectory = fileURLToPath(
  new URL("../../..", import.meta.url),
);

const baseURL = "http://127.0.0.1:18780";

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

  webServer: {
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