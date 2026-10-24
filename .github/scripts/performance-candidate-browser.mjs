import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import path from 'node:path'

const base = 'http://127.0.0.1:18091'
const out = '/tmp/console-candidate-smoke/browser'
const require = createRequire(process.env.PLAYWRIGHT_PACKAGE_JSON || import.meta.url)
const { chromium } = require('playwright')
await mkdir(out, { recursive: true })
const browser = await chromium.launch({ headless: true })
const results = [], errors = []
let activePage
try {
  for (const theme of ['light', 'dark']) {
    const prefix = theme === 'dark' ? '/starlightai' : ''
    const context = await browser.newContext({ colorScheme: theme, locale: 'zh-CN', reducedMotion: 'reduce' })
    await context.route('**/*', route => {
      const url = new URL(route.request().url())
      return url.origin === base || ['data:', 'blob:'].includes(url.protocol) ? route.continue() : route.abort()
    })
    await context.addInitScript(theme => {
      localStorage.setItem('theme', theme)
      localStorage.setItem('sub2api_locale', 'zh')
    }, theme)
    const page = await context.newPage()
    activePage = page
    page.setDefaultTimeout(15000)
    page.on('pageerror', error => errors.push(error.message))
    let detailRequests = 0
    page.on('request', req => { if (new URL(req.url()).pathname.endsWith('/channels/performance/detail')) detailRequests++ })
    await page.goto(base + prefix + '/login')
    await page.locator('#email').fill('performance-user@example.invalid')
    await page.locator('#password').fill('Isolated-User-Password-39')
    await page.locator('button[type="submit"]').click()
    await page.waitForURL('**/dashboard')
    for (const width of [375, 768, 1440, 1920]) {
      await page.setViewportSize({ width, height: 1000 })
      await page.goto(base + prefix + '/available-channels')
      const entry = page.getByRole('button', { name: '性能', exact: true }).first()
      await entry.waitFor()
      await page.getByText('66.7%', { exact: true }).first().waitFor()
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
      assert.equal(await page.locator('html').evaluate(el => el.classList.contains('dark')), theme === 'dark')
      await page.screenshot({ path: path.join(out, 'cards-' + theme + '-' + width + '.png'), fullPage: true })
      await entry.focus()
      await page.keyboard.press('Enter')
      const dialog = page.getByRole('dialog')
      await dialog.locator('tbody tr').first().waitFor()
      assert.equal(await dialog.locator('tbody tr').count(), 1)
      assert((await dialog.locator('tbody').innerText()).includes('CI visible'))
      assert(!(await dialog.innerText()).includes('CI hidden'))
      await dialog.getByLabel('刷新间隔', { exact: true }).selectOption('0')
      const close = dialog.getByRole('button', { name: 'Close modal', exact: true })
      const last = dialog.locator('span[tabindex="0"]').last()
      const pixels = await dialog.locator('canvas').evaluateAll(nodes => nodes.map(canvas => {
        const data = canvas.getContext('2d').getImageData(0, 0, canvas.width, canvas.height).data
        let painted = 0
        for (let i = 3; i < data.length; i += 4) if (data[i]) painted++
        return { width: canvas.width, height: canvas.height, painted }
      }))
      assert.equal(pixels.length, 3)
      assert(pixels.every(p => p.width > 50 && p.height > 50 && p.painted > 50))
      await page.screenshot({ path: path.join(out, 'panel-' + theme + '-' + width + '.png'), fullPage: true })
      const box = await dialog.locator('.modal-content').boundingBox()
      assert(box && box.x >= 0 && box.x + box.width <= width, 'Panel must fit the viewport')
      // Capture the initial table position before keyboard focus scrolls the
      // rightmost status cells into view on narrow screens.
      await close.focus()
      await page.keyboard.press('Shift+Tab')
      assert(await last.evaluate(el => el === document.activeElement), 'Keyboard focus must stay inside the panel')
      await page.keyboard.press('Tab')
      assert(await close.evaluate(el => el === document.activeElement))
      if (width === 1440 && theme === 'light') {
        const detailResponse = () => page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/channels/performance/detail') && r.status() === 200)
        let pending = detailResponse()
        await dialog.getByLabel('服务档位', { exact: true }).selectOption('unknown')
        await pending
        await dialog.getByText('暂无数据', { exact: true }).waitFor()
        pending = detailResponse()
        await dialog.getByLabel('服务档位', { exact: true }).selectOption('')
        await pending
        pending = detailResponse()
        await dialog.getByLabel('请求类型', { exact: true }).selectOption('false')
        await pending
        await dialog.locator('dl dd').nth(1).filter({ hasText: '—' }).waitFor()
        pending = detailResponse()
        await dialog.getByLabel('请求类型', { exact: true }).selectOption('')
        await pending
        const chart = dialog.locator('canvas').last()
        await chart.scrollIntoViewIfNeeded()
        const point = await chart.evaluate(canvas => {
          const pixels = canvas.getContext('2d').getImageData(0, 0, canvas.width, canvas.height).data
          canvas.__qaBeforeHover = pixels
          for (let i = pixels.length - 4; i >= 0; i -= 4) {
            if (pixels[i] === 217 && pixels[i + 1] === 119 && pixels[i + 2] === 6 && pixels[i + 3] > 200) {
              const rect = canvas.getBoundingClientRect()
              return { x: rect.x + (i / 4 % canvas.width) * rect.width / canvas.width,
                y: rect.y + Math.floor(i / 4 / canvas.width) * rect.height / canvas.height }
            }
          }
          return null
        })
        assert(point, 'Real success-rate samples must paint a chart point')
        await page.mouse.move(point.x, point.y)
        await page.waitForTimeout(350)
        const changed = await chart.evaluate(canvas => {
          const after = canvas.getContext('2d').getImageData(0, 0, canvas.width, canvas.height).data
          let changed = 0
          for (let i = 0; i < after.length; i += 4) {
            if (after[i] !== canvas.__qaBeforeHover[i] || after[i + 3] !== canvas.__qaBeforeHover[i + 3]) changed++
          }
          delete canvas.__qaBeforeHover
          return changed
        })
        assert(changed > 100, 'Hover must display chart details, not an inert point')
        await page.screenshot({ path: path.join(out, 'real-point-tooltip.png'), fullPage: true })
        await dialog.getByLabel('刷新间隔', { exact: true }).selectOption('5')
        await page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/channels/performance/detail') && r.status() === 200, { timeout: 10000 })
        await dialog.getByLabel('刷新间隔', { exact: true }).selectOption('0')
        const stopped = detailRequests
        await page.waitForTimeout(5500)
        assert.equal(detailRequests, stopped, 'Disabled refresh must stop performance polling')
        const routeMatcher = url => url.pathname.endsWith('/channels/performance/detail')
        await page.route(routeMatcher, route => route.fulfill({ status: 503, contentType: 'application/json', body: '{"code":503,"message":"CI unavailable"}' }))
        await dialog.getByRole('button', { name: '刷新', exact: true }).click()
        await dialog.getByRole('alert').waitFor()
        await page.unroute(routeMatcher)
        pending = detailResponse()
        await dialog.getByRole('button', { name: '刷新', exact: true }).click()
        await pending
        await dialog.getByRole('alert').waitFor({ state: 'hidden' })
        await dialog.getByLabel('刷新间隔', { exact: true }).selectOption('5')
      }
      await page.keyboard.press('Escape')
      await dialog.waitFor({ state: 'hidden' })
      assert(await entry.evaluate(el => el === document.activeElement), 'Closing the panel must restore its entry focus')
      if (width === 1440 && theme === 'light') {
        const stopped = detailRequests
        await page.waitForTimeout(5500)
        assert.equal(detailRequests, stopped, 'Unmounted panel must stop polling')
        await page.getByText('gpt-5.1', { exact: true }).first().waitFor()
      }
      results.push({ theme, width, prefix, pixels })
    }
    await page.goto(base + prefix + '/usage')
    for (const label of ['首响应', '首事件', '首输出', '首吐字', '总耗时']) await page.getByText(label, { exact: true }).first().waitFor()
    await page.screenshot({ path: path.join(out, 'usage-' + theme + '.png'), fullPage: true })
    await context.close()
    activePage = null
  }
  assert.deepEqual(errors, [])
  await writeFile(path.join(out, 'results.json'), JSON.stringify({ results, errors, successfulData: 'real isolated gateway requests', injectedCase: '503 error-state only' }, null, 2))
  console.log('Packaged candidate browser checks passed: 8 viewport/theme combinations and both entry paths')
} catch (error) {
  if (activePage && !activePage.isClosed()) await activePage.screenshot({ path: path.join(out, 'failure.png'), fullPage: true }).catch(() => {})
  await writeFile(path.join(out, 'failure.json'), JSON.stringify({ message: error.message, errors, completed: results }, null, 2))
  throw error
} finally {
  await browser.close()
}
