import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import path from 'node:path'

// Runs only against the synthetic local preview, never the production gateway.
const base = process.env.CONSOLE_PREVIEW_URL || 'http://127.0.0.1:5194'
assert(['127.0.0.1', 'localhost'].includes(new URL(base).hostname))
const require = createRequire(process.env.PLAYWRIGHT_PACKAGE_JSON || import.meta.url)
const { chromium } = require('playwright')
const out = path.resolve(process.env.CONSOLE_QA_OUTPUT || '../outputs/console-qa')
await mkdir(out, { recursive: true })
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}) })
const failures = []
const results = []
const routes = ['/dashboard', '/available-channels', '/keys', '/usage', '/subscriptions', '/monitor', '/monitor?view=v2']
try {
  for (const theme of ['light', 'dark']) {
    const context = await browser.newContext({ reducedMotion: 'reduce', colorScheme: theme })
    await context.route('**/*', route => {
      const url = new URL(route.request().url())
      return ['127.0.0.1', 'localhost'].includes(url.hostname) || ['data:', 'blob:'].includes(url.protocol) ? route.continue() : route.abort()
    })
    const page = await context.newPage()
    page.on('pageerror', error => failures.push({ url: page.url(), error: error.message }))
    await page.goto(base + '/__preview')
    await page.waitForURL('**/dashboard')
    await page.evaluate(theme => localStorage.setItem('theme', theme), theme)
    for (const width of [375, 768, 1440, 1920]) {
      await page.setViewportSize({ width, height: 960 })
      for (const route of routes) {
        await page.goto(base + route)
        await page.locator('.console-page').waitFor()
        await page.waitForTimeout(1400)
        const measurement = await page.evaluate(() => ({
          overflow: document.documentElement.scrollWidth > innerWidth,
          dark: document.documentElement.classList.contains('dark'),
          brokenImages: [...document.images].filter(image => !image.complete || image.naturalWidth === 0).map(image => image.src),
          canvas: [...document.querySelectorAll('canvas')].map(canvas => {
            const data = canvas.getContext('2d')?.getImageData(0, 0, canvas.width, canvas.height).data
            let pixels = 0
            if (data) for (let i = 3; i < data.length; i += 4) if (data[i] > 0) pixels++
            return { width: canvas.width, height: canvas.height, pixels }
          })
        }))
        const result = { theme, width, route, ...measurement }
        results.push(result)
        if (measurement.overflow || measurement.brokenImages.length || measurement.dark !== (theme === 'dark') || measurement.canvas.some(c => c.pixels < 100)) failures.push(result)
        const name = route.slice(1).replace('?view=', '-')
        await page.screenshot({ path: path.join(out, name + '-' + theme + '-' + width + '.png'), fullPage: true })
      }
    }
    await context.close()
  }
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: ['clipboard-read', 'clipboard-write'] })
  const page = await context.newPage()
  page.on('pageerror', error => failures.push({ url: page.url(), error: error.message }))
  await page.goto(base + '/__preview')
  await page.waitForURL('**/dashboard')
  await page.goto(base + '/subscriptions')
  await page.getByTestId('shared-subscription-1').waitFor()
  const checkbox = page.getByRole('checkbox').first()
  const original = await checkbox.isChecked()
  await checkbox.setChecked(!original)
  await page.waitForTimeout(200)
  await page.reload()
  await page.getByTestId('shared-subscription-1').waitFor()
  assert.equal(await checkbox.isChecked(), !original, 'Global balance top-up must persist')
  await checkbox.setChecked(original)
  await page.getByRole('button', { name: '余额优先', exact: true }).click()
  await page.waitForTimeout(200)
  await page.reload()
  await page.getByTestId('shared-subscription-1').waitFor()
  assert.equal((await page.request.get(base + '/api/v1/subscriptions/shared')).ok(), true)
  await page.getByRole('button', { name: '订阅优先', exact: true }).click()
  await page.goto(base + '/monitor')
  await page.locator('nav[role="tablist"] > div > button').first().waitFor()
  assert.equal(await page.locator('nav[role="tablist"] > div > button').last().getAttribute('aria-selected'), 'true', 'Legacy is still the default')
  await page.locator('nav[role="tablist"] > div > button').first().click()
  await page.waitForURL('**/monitor?view=v2')
  await page.locator('nav[role="tablist"] > div > button').last().click()
  await page.waitForURL('**/monitor?view=v1')
  await page.goto(base + '/available-channels')
  const search = page.getByPlaceholder('搜索模型、分组或平台...')
  await search.fill('deepseek')
  await page.waitForTimeout(300)
  assert.equal(await page.locator('article').count(), 1, 'Model filtering')
  await search.fill('no-model-exists-here')
  await page.waitForTimeout(300)
  assert.equal(await page.locator('article').count(), 0, 'Model empty state')
  await page.goto(base + '/usage')
  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: '导出 CSV' }).click()
  const download = await downloadPromise
  await download.saveAs(path.join(out, 'usage.csv'))
  await page.goto(base + '/keys')
  await page.locator('[data-tour="keys-create-btn"]').click()
  await page.locator('[data-tour="key-form-name"]').fill('Console QA key')
  await page.locator('[data-tour="key-form-group"]').click()
  await page.getByRole('option').filter({ hasText: 'GPT 专业线路' }).click()
  const creation = page.waitForResponse(response => response.url().endsWith('/api/v1/keys') && response.request().method() === 'POST')
  await page.locator('[data-tour="key-form-submit"]').click()
  assert.equal((await creation).status(), 200)
  await page.waitForTimeout(500)
  assert((await page.locator('body').innerText()).includes('Console QA key'), 'Key creation remains usable')
  await context.close()
} catch (error) {
  failures.push({ error: error.message })
  throw error
} finally {
  await writeFile(path.join(out, 'report.json'), JSON.stringify({ results, failures }, null, 2))
  await browser.close()
}
console.log(JSON.stringify({ screenshots: results.length, failures }, null, 2))
assert.equal(failures.length, 0, 'Console visual/JS checks failed')
