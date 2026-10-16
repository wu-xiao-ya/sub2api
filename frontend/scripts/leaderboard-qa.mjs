import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import path from 'node:path'

const base = 'http://127.0.0.1:5194'
const require = createRequire(process.env.PLAYWRIGHT_PACKAGE_JSON || import.meta.url)
const { chromium } = require('playwright')
const out = path.resolve('../..', 'leaderboard-release-qa-20261016')
await mkdir(out, { recursive: true })
const browser = await chromium.launch({
  headless: true,
  ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}),
})
const errors = []
const results = []
try {
  for (const theme of ['light', 'dark']) {
    const context = await browser.newContext({ colorScheme: theme, reducedMotion: 'reduce' })
    await context.route('**/*', route => {
      const url = new URL(route.request().url())
      return ['127.0.0.1', 'localhost'].includes(url.hostname) ||
        ['data:', 'blob:'].includes(url.protocol) ? route.continue() : route.abort()
    })
    const page = await context.newPage()
    page.on('pageerror', error => errors.push(error.message))
    const probeRequests = []
    page.on('request', request => {
      if (request.url().includes('/intelligence-probe')) probeRequests.push(request.url())
    })
    await page.goto(base + '/__preview')
    await page.waitForURL('**/dashboard')
    await page.evaluate(value => localStorage.setItem('theme', value), theme)
    for (const width of [320, 375, 768, 1440]) {
      await page.setViewportSize({ width, height: 960 })
      await page.goto(base + '/leaderboard')
      await page.locator('tbody tr').nth(19).waitFor()
      const measurement = await page.evaluate(() => ({
        documentOverflow: document.documentElement.scrollWidth > innerWidth,
        clippedCells: [...document.querySelectorAll('tbody td')].filter(
          item => item.scrollWidth > item.clientWidth + 2
        ).length,
        brokenImages: [...document.images].filter(image => !image.complete || image.naturalWidth === 0).length,
        dark: document.documentElement.classList.contains('dark'),
      }))
      assert.equal(measurement.documentOverflow, false)
      assert.equal(measurement.clippedCells, 0)
      assert.equal(measurement.brokenImages, 0)
      assert.equal(measurement.dark, theme === 'dark')
      assert.equal(await page.locator('tbody tr').count(), 20)
      assert.equal(await page.locator('tbody tr[data-current-user="true"]').count(), 1)
      results.push({ theme, width, ...measurement })
      await page.screenshot({ path: path.join(out, `leaderboard-${theme}-${width}.png`), fullPage: true })
    }
    for (const period of ['today', 'week', 'month', 'all']) {
      const button = page.locator(`[data-period="${period}"]`)
      if (await button.getAttribute('aria-selected') !== 'true') {
        const response = page.waitForResponse(
          item => item.url().includes('/api/v1/leaderboard?') &&
            new URL(item.url()).searchParams.get('period') === period
        )
        await button.click()
        assert.equal((await response).status(), 200)
      }
      await page.locator('tbody tr').nth(19).waitFor()
      assert.equal(await button.getAttribute('aria-selected'), 'true')
    }
    let failNext = true
    await page.route('**/api/v1/leaderboard?**', route => {
      if (failNext) {
        failNext = false
        return route.fulfill({ status: 503, contentType: 'application/json',
          body: JSON.stringify({ code: 503, message: 'Synthetic QA failure' }) })
      }
      return route.continue()
    })
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    const alert = page.locator('section[aria-busy] [role="alert"]')
    await alert.waitFor()
    assert((await alert.innerText()).includes('加载排行榜失败'))
    await alert.getByRole('button').click()
    await page.locator('tbody tr').nth(19).waitFor()
    await page.goto(base + '/usage')
    await page.getByRole('button', { name: '用量排行', exact: true }).click()
    await page.locator('tbody tr[data-current-user="true"]').waitFor()
    await page.screenshot({ path: path.join(out, `usage-ranking-${theme}.png`), fullPage: true })
    await page.goto(base + '/monitor?view=v1')
    await page.locator('.console-page').waitFor()
    await page.waitForTimeout(600)
    assert.equal(probeRequests.length, 0, 'Retired feature must not issue requests')
    await context.close()
  }
} finally {
  await browser.close()
  await writeFile(path.join(out, 'report.json'), JSON.stringify({ results, errors }, null, 2))
}
assert.deepEqual(errors, [])
console.log(JSON.stringify({ viewports: results.length, javascriptErrors: errors.length,
  periodSwitching: true, failureRetry: true, embeddedRanking: true, retiredProbeRequests: 0 }))
