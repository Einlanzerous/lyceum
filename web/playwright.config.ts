import { defineConfig, devices } from '@playwright/test'

// Reader smoke (LYCM-204): epub.js renders into a real iframe, so this is the
// acceptance path the unit tests cannot cover. It drives the system Chrome
// (channel 'chrome') against a running dev server + backend — no bundled
// browser download. Bring the stack up first (see web/e2e/README.md), or set
// PLAYWRIGHT_BASE_URL to point elsewhere.
const baseURL = process.env.PLAYWRIGHT_BASE_URL ?? 'http://localhost:5180'

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  // The specs share one backend book's reading position, so run serially.
  fullyParallel: false,
  workers: 1,
  reporter: [['list']],
  use: {
    baseURL,
    channel: 'chrome',
    headless: true,
  },
  projects: [{ name: 'chrome', use: { ...devices['Desktop Chrome'], channel: 'chrome' } }],
})
