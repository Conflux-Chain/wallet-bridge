import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/wallet',
  workers: 1,
  retries: 0,
  timeout: 30_000,
  reporter: 'list',
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
    { name: 'chrome', use: { browserName: 'chromium', channel: 'chrome' } },
  ],
  use: { baseURL: 'http://127.0.0.1:4173', trace: 'off', screenshot: 'off', video: 'off' },
  webServer: {
    command: 'npm run dev -- --port 4173 --strictPort',
    url: 'http://127.0.0.1:4173',
    reuseExistingServer: false,
  },
});
