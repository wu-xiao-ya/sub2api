import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import path from 'node:path'

const base = process.env.CONSOLE_PREVIEW_URL || 'http://127.0.0.1:5194'
assert(['127.0.0.1', 'localhost'].includes(new URL(base).hostname))
const require = createRequire(process.env.PLAYWRIGHT_PACKAGE_JSON || import.meta.url)
const { chromium } = require('playwright')
const out = path.resolve(process.env.CONSOLE_QA_OUTPUT || '../outputs/latency-qa')
await mkdir(out, { recursive: true })
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}) })
const results = []
const errors = []
try {
  for (const theme of ['light', 'dark']) {
    const context = await browser.newContext({ reducedMotion: 'reduce', colorScheme: theme })
    await context.route('**/*', route => {
      const url = new URL(route.request().url())
      return ['127.0.0.1', 'localhost'].includes(url.hostname) || ['data:', 'blob:'].includes(url.protocol) ? route.continue() : route.abort()
    })
    const page = await context.newPage()
    page.on('pageerror', error => errors.push(error.message))
    await page.goto(base + '/__preview')
    await page.waitForURL('**/dashboard')
    await page.evaluate(theme => localStorage.setItem('theme', theme), theme)
    for (const width of [375, 768, 1440, 1920]) {
      await page.setViewportSize({ width, height: 960 })
      await page.goto(base + '/usage')
      const trigger = page.getByRole('button', { name: '延迟分析', exact: true }).first()
      await trigger.waitFor({ timeout: 15000 })
      await trigger.scrollIntoViewIfNeeded()
      for (const label of ['首响应','首事件','首输出','首吐字']) assert((await trigger.innerText()).includes(label))
      await trigger.focus()
      await trigger.press('Enter')
      const tooltip = page.locator('body > div.fixed').filter({ hasText: '口径 V2' })
      await tooltip.waitFor({ timeout: 3000 })
      const box = await tooltip.boundingBox()
      assert(box && box.x >= 0 && box.y >= 0 && box.x + box.width <= width && box.y + box.height <= 960, JSON.stringify({width,theme,box}))
      assert((await tooltip.innerText()).includes('本次转发前耗时'))
      await page.screenshot({ path: path.join(out, 'usage-' + theme + '-' + width + '.png'), fullPage: true })
      await trigger.press('Escape')
      assert.equal(await trigger.getAttribute('aria-expanded'), 'false')
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
      results.push({ theme, width, tooltip: box })
    }
    await context.close()
  }
  assert.deepEqual(errors, [])
  await writeFile(path.join(out, 'results.json'), JSON.stringify({results,errors}, null, 2))
  console.log(JSON.stringify({screenshots:results.length,errors:errors.length,output:out}))
} finally {
  await browser.close()
}
