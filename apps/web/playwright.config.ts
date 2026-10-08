import { defineConfig, devices } from "@playwright/test";

// E2E runs the real stack on isolated ports: Go API (offline deterministic planner, dedicated DB)
// on :8090 and the Next.js app on :3100 proxying /api to it. The E2E database is reset on start.
const API_PORT = 8090;
const WEB_PORT = 3100;
const DB = process.env.E2E_DATABASE_URL || `postgres://ideavault:ideavault@localhost:${process.env.POSTGRES_PORT || "5432"}/ideavault_e2e?sslmode=disable`;
const isCI = Boolean(process.env.CI);

export default defineConfig({
  testDir: "./tests/e2e",
  timeout: 90_000,
  expect: { timeout: 15_000 },
  fullyParallel: false,
  workers: 1,
  retries: isCI ? 1 : 0,
  reporter: isCI ? [["list"], ["html", { open: "never" }]] : [["list"]],
  use: {
    baseURL: `http://localhost:${WEB_PORT}`,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    viewport: { width: 1440, height: 900 },
  },
  projects: [
    { name: "setup", testMatch: /auth\.setup\.ts/ },
    {
      name: "chromium",
      dependencies: ["setup"],
      use: { ...devices["Desktop Chrome"], viewport: { width: 1440, height: 900 }, storageState: "tests/e2e/.auth/owner.json" },
    },
  ],
  webServer: [
    {
      // Reset the E2E database (all migrations down), then start the API, which migrates up.
      command: `sh -c "cd ../api && go build -o bin/api ./cmd/api && go build -o bin/migrate ./cmd/migrate && DATABASE_URL='${DB}' ./bin/migrate down 100 >/dev/null 2>&1; exec ./bin/api"`,
      url: `http://localhost:${API_PORT}/readyz`,
      timeout: 180_000,
      reuseExistingServer: false,
      env: {
        PORT: String(API_PORT),
        DATABASE_URL: DB,
        REDIS_URL: "",
        MOCK_AI: "true",
        APP_ENV: "development",
        LOG_LEVEL: "warn",
        LOG_FORMAT: "text",
        WEB_ORIGINS: `http://localhost:${WEB_PORT}`,
        RATE_LIMIT_AUTH_PER_MIN: "1000",
        RATE_LIMIT_AGENT_PER_MIN: "1000",
        RATE_LIMIT_IMPORT_PER_MIN: "1000",
        RATE_LIMIT_API_PER_MIN: "100000",
        JOB_POLL_INTERVAL: "200ms",
      },
    },
    {
      command: isCI ? `npm run build && npx next start -p ${WEB_PORT}` : `npx next dev -p ${WEB_PORT}`,
      url: `http://localhost:${WEB_PORT}/login`,
      timeout: 300_000,
      reuseExistingServer: !isCI,
      env: { API_ORIGIN: `http://localhost:${API_PORT}`, NEXT_DIST_DIR: ".next-e2e" },
    },
  ],
});
