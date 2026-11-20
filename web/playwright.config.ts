import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  globalSetup: './tests/e2e/setup.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 30_000,
  expect: { timeout: 8000 },
  reporter: 'list',
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
    { name: 'chrome', use: { browserName: 'chromium', channel: 'chrome' } },
  ],
  use: {
    baseURL: 'http://127.0.0.1:18765',
    // Authorization URLs and cookies must not enter diagnostic artifacts.
    trace: 'off', screenshot: 'off', video: 'off',
  },
});
