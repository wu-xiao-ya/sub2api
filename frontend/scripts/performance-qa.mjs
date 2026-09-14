import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import path from 'node:path'

const base = process.env.CONSOLE_PREVIEW_URL || 'http://127.0.0.1:5194'
assert(['127.0.0.1', 'localhost'].includes(new URL(base).hostname))
const require = createRequire(process.env.PLAYWRIGHT_PACKAGE_JSON || import.meta.url)
const { chromium } = require('playwright')
const out = path.resolve('../outputs/performance-qa')
await mkdir(out, { recursive: true })
const browser = await chromium.launch({ headless: true, ...(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}) })
const results = [], errors = []
try {
  for (const theme of ['light', 'dark']) {
    const context = await browser.newContext({ reducedMotion: 'reduce', colorScheme: theme })
    await context.route('**/*', route => {
      const url = new URL(route.request().url())
      return ['127.0.0.1', 'localhost'].includes(url.hostname) || ['data:', 'blob:'].includes(url.protocol) ? route.continue() : route.abort()
    })
    const page = await context.newPage()
    page.on('pageerror', error => errors.push(error.message))
    await page.goto(base + '/__preview'); await page.waitForURL('**/dashboard')
    await page.evaluate(theme => localStorage.setItem('theme', theme), theme)
    for (const width of [375, 768, 1440, 1920]) {
      await page.setViewportSize({ width, height: 1000 })
      await page.goto(base + '/available-channels')
      const entry = page.getByRole('button', { name: '性能', exact: true }).first()
      await entry.waitFor({ timeout: 15000 })
      await page.getByText('48.6 tok/s', { exact: true }).first().waitFor()
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
      await page.screenshot({ path: path.join(out, 'cards-' + theme + '-' + width + '.png'), fullPage: true })
      await entry.click()
      const dialog = page.getByRole('dialog')
      await dialog.getByText('实际覆盖', { exact: false }).waitFor()
      await dialog.locator('tbody tr').first().waitFor()
      const canvases = dialog.locator('canvas')
      assert.equal(await canvases.count(), 3)
      const pixels = await canvases.evaluateAll(nodes => nodes.map(canvas => {
        const data = canvas.getContext('2d').getImageData(0,0,canvas.width,canvas.height).data
        let painted = 0; for (let i=3;i<data.length;i+=4) if (data[i]) painted++
        return {width:canvas.width,height:canvas.height,painted}
      }))
      assert(pixels.every(canvas => canvas.width > 50 && canvas.height > 50 && canvas.painted > 50))
      await dialog.getByLabel('刷新间隔', { exact: true }).selectOption('0')
      const response = page.waitForResponse(res => res.url().includes('/performance/detail') && res.url().includes('range=7d'))
      await dialog.getByLabel('统计范围', { exact: true }).selectOption('7d'); await response
      await page.screenshot({ path: path.join(out, 'panel-' + theme + '-' + width + '.png'), fullPage: true })
      const box = await dialog.locator('.modal-content').boundingBox()
      assert(box && box.x >= 0 && box.x + box.width <= width, JSON.stringify({width,theme,box}))
      await page.keyboard.press('Escape'); await dialog.waitFor({ state: 'hidden' })
      results.push({theme,width,pixels})
    }
    await context.close()
  }
  assert.deepEqual(errors, [])
  await writeFile(path.join(out, 'results.json'), JSON.stringify({results,errors}, null, 2))
  console.log(JSON.stringify({screenshots:results.length*2,errors:errors.length,output:out}))
} finally { await browser.close() }
