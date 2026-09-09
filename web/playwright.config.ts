import { tmpdir } from "node:os";
import { join } from "node:path";
import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "tests",
  testMatch: "**/*.spec.ts",
  workers: 1,
  timeout: 90000,
  reporter: "list",
  outputDir: join(tmpdir(), "aegishook-playwright-results"),
  use: {
    baseURL: "http://127.0.0.1:18794",
    actionTimeout: 12000,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    viewport: { width: 1440, height: 1000 },
    launchOptions: { executablePath: process.env.AEGIS_CHROMIUM },
  },
});
