import { test, expect, firefox } from '@playwright/test'
test('firefox behaviours', async () => {
  const b = await firefox.launch(); const p = await b.newPage()
  await p.goto('http://localhost:5199/harness/index.html')
  await p.focus('#open'); await p.keyboard.press('Enter')
  expect(await p.evaluate(() => document.querySelector('dialog')!.matches(':modal'))).toBe(true)
  expect(await p.evaluate(() => document.activeElement!.tagName)).toBe('INPUT')
  await p.keyboard.press('Tab'); await p.keyboard.press('Tab'); await p.keyboard.press('Tab'); await p.keyboard.press('Tab'); await p.keyboard.press('Tab'); await p.keyboard.press('Tab'); await p.keyboard.press('Tab')
  expect(await p.evaluate(() => !!document.activeElement!.closest('dialog') || document.activeElement===document.body)).toBe(true) // trapped
  await p.screenshot({ path: '/tmp/claude-1000/-home-magos-projects/9ea43aac-ebde-4883-8cbb-17b795d9359c/scratchpad/modal.png' })
  await p.keyboard.press('Escape')
  expect(await p.locator('dialog').count()).toBe(0)
  expect(await p.evaluate(() => document.activeElement!.id)).toBe('open')
  // backdrop click
  await p.click('#open'); await p.mouse.click(5, 5)
  expect(await p.locator('dialog').count()).toBe(0)
  // sort menu
  await p.focus('.sort__trigger'); await p.keyboard.press('ArrowDown')
  expect(await p.evaluate(() => document.activeElement!.getAttribute('role'))).toBe('option')
  await p.keyboard.press('ArrowDown')
  await p.keyboard.press('Escape')
  expect(await p.evaluate(() => document.activeElement!.className)).toContain('sort__trigger')
  // tile menu
  await p.focus('#ctx'); await p.keyboard.press('Enter'); await p.waitForTimeout(100)
  expect(await p.evaluate(() => document.activeElement!.textContent)).toBe('One')
  await p.keyboard.press('ArrowDown'); expect(await p.evaluate(() => document.activeElement!.textContent)).toBe('Two')
  await p.keyboard.press('Escape'); await p.waitForTimeout(50)
  expect(await p.evaluate(() => document.activeElement!.id)).toBe('ctx')
  // focus ring
  await p.focus('#bg')
  expect(await p.evaluate(() => getComputedStyle(document.activeElement!).outlineStyle)).toBe('solid')
  await b.close()
})
