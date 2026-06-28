import { test, expect } from '@playwright/test'

// Smoke for the epub.js rendering path. Requires a running dev server proxying
// to a backend seeded with a multi-page book (see e2e/README.md and
// make-fixture.mjs). E2E_BOOK_ID selects which book to open.
const BOOK_ID = process.env.E2E_BOOK_ID ?? '1'

test('opens a book and advances pages', async ({ page }) => {
  await page.goto(`/reader/${BOOK_ID}`)

  // The reading surface hosts epub.js's iframe.
  const iframe = page.locator('.reader__surface iframe')
  await expect(iframe).toBeVisible({ timeout: 20_000 })

  // Real text landed inside the rendered section.
  const frameBody = page.frameLocator('.reader__surface iframe').locator('body')
  await expect(frameBody).not.toBeEmpty({ timeout: 20_000 })
  expect((await frameBody.innerText()).trim().length).toBeGreaterThan(0)

  // Loading overlay cleared once the first page rendered.
  await expect(page.locator('.overlay')).toHaveCount(0)

  // epub.js paginates a section with CSS columns, so body text is constant
  // within a section; the CFI is the reliable signal that the page advanced.
  const reader = page.locator('.reader')
  await expect.poll(async () => reader.getAttribute('data-cfi')).not.toBe('')
  const firstCfi = await reader.getAttribute('data-cfi')

  await page.locator('.snav--next').click()
  await expect.poll(async () => reader.getAttribute('data-cfi'), { timeout: 20_000 }).not.toBe(
    firstCfi,
  )

  // And prev returns toward the start.
  const advancedCfi = await reader.getAttribute('data-cfi')
  await page.locator('.snav--prev').click()
  await expect
    .poll(async () => reader.getAttribute('data-cfi'), { timeout: 20_000 })
    .not.toBe(advancedCfi)
})
