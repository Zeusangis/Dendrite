import { defineConfig, devices } from "@playwright/test";
const apiPort = process.env.DENDRITE_TEST_API_PORT || "18080";
const uiPort = process.env.DENDRITE_TEST_UI_PORT || "13000";
export default defineConfig({
 testDir: "./tests",
 testMatch: "**/*.spec.ts",
 fullyParallel: false,
 workers: 1,
 timeout: 45000,
 use: { baseURL: `http://127.0.0.1:${uiPort}`, trace: "retain-on-failure" },
 projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
 webServer: [
  { command: "node tests/serve-backend.mjs", url: `http://127.0.0.1:${apiPort}/api/health`, timeout: 60000, reuseExistingServer: false },
  { command: `npx next dev --hostname 127.0.0.1 --port ${uiPort}`, url: `http://127.0.0.1:${uiPort}`, timeout: 60000, reuseExistingServer: false, env: { DENDRITE_API_ORIGIN: `http://127.0.0.1:${apiPort}` } },
 ],
});
