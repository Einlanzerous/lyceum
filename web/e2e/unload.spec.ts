import { test, expect } from '@playwright/test'

// Verifies LYCM-206 in a real browser, isolated from the LYCM-205 debounce:
// after a page turn (which only *schedules* a debounced PUT ~1s out), sending
// the page to the background must flush a keepalive PUT /sync immediately. We
// assert a PUT arrives well inside the debounce window, so it can only be the
// unload flush, not the debounce timer.
const BOOK_ID = process.env.E2E_BOOK_ID ?? '1'
const DEBOUNCE_MS = 1000
const FLUSH_BUDGET_MS = 500

test('flushes the latest position immediately on backgrounding', async ({ page }) => {
  const putTimes: number[] = []
  page.on('request', (req) => {
    if (req.method() === 'PUT' && req.url().includes('/sync')) putTimes.push(Date.now())
  })

  const reader = page.locator('.reader')
  await page.goto(`/reader/${BOOK_ID}`)
  await expect(page.locator('.reader__surface iframe')).toBeVisible({ timeout: 20_000 })
  await expect.poll(async () => reader.getAttribute('data-cfi')).not.toBe('')

  const startCfi = await reader.getAttribute('data-cfi')
  await page.locator('.reader__nav--next').click()
  await expect.poll(async () => reader.getAttribute('data-cfi')).not.toBe(startCfi)

  // Background the page and time the resulting flush.
  const firedAt = Date.now()
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true })
    document.dispatchEvent(new Event('visibilitychange'))
  })

  // A PUT inside FLUSH_BUDGET_MS of backgrounding — and comfortably before the
  // page-turn's debounce (~DEBOUNCE_MS) could fire — proves the flush path.
  await expect
    .poll(() => putTimes.some((t) => t >= firedAt && t - firedAt < FLUSH_BUDGET_MS), {
      timeout: FLUSH_BUDGET_MS,
      intervals: [25, 25, 50],
    })
    .toBe(true)
  expect(FLUSH_BUDGET_MS).toBeLessThan(DEBOUNCE_MS)
})
