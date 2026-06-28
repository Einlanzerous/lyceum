import { test, expect } from '@playwright/test'

// Verifies LYCM-205 against the live backend: turning pages persists the CFI via
// PUT /sync (debounced), and reopening the book restores that page via GET
// /sync. Requires the same seeded multi-page book as reader.spec.ts.
//
// Note on the restore assertion: epub.js normalizes a mid-page CFI to its
// column-start on display(), so the restored CFI need not byte-equal the saved
// one. The meaningful guarantees are (a) the backend stored exactly what we were
// reading, and (b) reopening lands off page one — both checked below.
const BOOK_ID = process.env.E2E_BOOK_ID ?? '1'

test('persists reading position and restores it on reopen', async ({ page, request }) => {
  const reader = page.locator('.reader')

  await page.goto(`/reader/${BOOK_ID}`)
  await expect(page.locator('.reader__surface iframe')).toBeVisible({ timeout: 20_000 })
  await expect.poll(async () => reader.getAttribute('data-cfi')).not.toBe('')
  const startCfi = await reader.getAttribute('data-cfi')

  // Advance a couple of pages.
  await page.locator('.snav--next').click()
  await expect.poll(async () => reader.getAttribute('data-cfi')).not.toBe(startCfi)
  await page.locator('.snav--next').click()

  // The debounced PUT /sync converges the backend to the page on screen. Poll
  // until the stored position (latest across devices) matches the displayed
  // CFI — proving page turns are persisted, without racing the debounce.
  await expect
    .poll(
      async () => {
        const current = await reader.getAttribute('data-cfi')
        const resp = await request.get(`/sync?book_id=${BOOK_ID}`)
        if (!resp.ok() || !current) return null
        return (await resp.json()).cfi === current ? current : null
      },
      { timeout: 15_000 },
    )
    .not.toBeNull()

  // Reopen the book from scratch; GET /sync should restore a non-first page.
  await page.goto('about:blank')
  await page.goto(`/reader/${BOOK_ID}`)
  await expect(page.locator('.reader__surface iframe')).toBeVisible({ timeout: 20_000 })
  await expect.poll(async () => reader.getAttribute('data-cfi'), { timeout: 20_000 }).not.toBe('')
  // Restored to a non-first page: Prev is enabled (bound to atStart) and the
  // restored CFI is not the page we started this test on.
  await expect(page.locator('.snav--prev')).toBeEnabled()
  expect(await reader.getAttribute('data-cfi')).not.toBe(startCfi)
})
