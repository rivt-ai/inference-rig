import { defineConfig, devices } from '@playwright/test';

// The gateway, control daemon, engine, and model are supplied by the Go process
// harness (test/e2e/browser_test.go), which exports the URL and token below.
// Playwright deliberately starts no server of its own: the point of this layer
// is to drive the real built app against the real backend, not a stand-in.
const baseURL = process.env.INFERENCERIG_E2E_BASE_URL;

export default defineConfig({
  testDir: './e2e',
  // A browser workflow that only passes when retried is a flaky workflow, and
  // hiding that would defeat the purpose of the layer.
  retries: 0,
  workers: 1,
  timeout: 120_000,
  expect: { timeout: 20_000 },
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : [['list']],
  use: {
    baseURL,
    // Enough to diagnose a CI failure without an interactive rerun.
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure'
  },
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        // A sandbox with a preinstalled browser opts out of Playwright's
        // pinned-build lookup, which fails when the two disagree.
        ...(process.env.CHROMIUM_PATH ? { launchOptions: { executablePath: process.env.CHROMIUM_PATH } } : {})
      }
    }
  ]
});
