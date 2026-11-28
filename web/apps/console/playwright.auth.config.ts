import {
  defineConfig,
  devices,
} from "@playwright/test";

export default defineConfig({
  testDir: "./e2e-auth",

  outputDir:
    "./test-results/authentication",

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
    baseURL: "https://rackos.test",

    ignoreHTTPSErrors: false,

    serviceWorkers: "block",

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
});