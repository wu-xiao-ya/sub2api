import assert from 'node:assert/strict'
import { mkdir, writeFile } from 'node:fs/promises'
import { createRequire } from 'node:module'

const base = 'http://127.0.0.1:18091'
const output = '/tmp/console-candidate-smoke/relay-browser'
const require = createRequire(process.env.PLAYWRIGHT_PACKAGE_JSON || import.meta.url)
const { chromium } = require('playwright')
await mkdir(output, { recursive: true })
const browser = await chromium.launch({ headless: true })
const errors = [], checks = []
try {
  for (const theme of ['light', 'dark']) {
    const prefix = theme === 'dark' ? '/starlightai' : ''
    const context = await browser.newContext({ colorScheme: theme, locale: 'zh-CN', reducedMotion: 'reduce' })
    let configured = false
    await context.route('**/*', async route => {
      const url = new URL(route.request().url())
      if (url.origin !== base && !['data:', 'blob:'].includes(url.protocol)) return route.abort()
      if (url.pathname.endsWith('/api/v1/admin/settings') && route.request().method() === 'GET') {
        const response = await route.fetch()
        const body = await response.json()
        body.data.traffic_relay_enabled = configured
        body.data.traffic_relay_proxy_id = configured ? 1 : 0
        return route.fulfill({ response, json: body })
      }
      return route.continue()
    })
    await context.addInitScript(theme => {
      localStorage.setItem('theme', theme)
      localStorage.setItem('sub2api_locale', 'zh')
      // The isolated bootstrap administrator is user 1. This suite verifies
      // account controls, not the first-login onboarding overlay.
      localStorage.setItem('admin_guide_1_admin_v4_interactive', 'true')
    }, theme)
    const page = await context.newPage()
    page.setDefaultTimeout(15000)
    page.on('pageerror', error => errors.push(error.message))
    await page.goto(base + prefix + '/login')
    await page.locator('#email').fill('ci@example.invalid')
    await page.locator('#password').fill('Isolated-CI-Password-39')
    await page.locator('button[type="submit"]').click()
    await page.waitForURL(url => !url.pathname.endsWith('/login'))
    await page.goto(base + prefix + '/admin/accounts')
    for (const width of [375, 768, 1440, 1920]) {
      await page.setViewportSize({ width, height: 1000 })
      configured = false
      await page.getByRole('button', { name: '添加账号', exact: true }).click()
      const dialog = page.getByRole('dialog')
      const relay = dialog.getByRole('switch', { name: '使用中转线路', exact: true })
      await relay.waitFor()
      assert(await relay.isDisabled(), 'Unconfigured relay must be disabled')
      assert.equal(await relay.getAttribute('aria-checked'), 'false')
      await dialog.getByRole('button', { name: 'Close modal', exact: true }).click()
      configured = true
      await page.getByRole('button', { name: '添加账号', exact: true }).click()
      await relay.waitFor()
      await page.waitForFunction(() => {
        const toggle = document.querySelector('[role="switch"][aria-label="使用中转线路"]')
        return toggle && !toggle.disabled
      })
      await relay.scrollIntoViewIfNeeded()
      await relay.focus()
      await page.keyboard.press('Space')
      assert.equal(await relay.getAttribute('aria-checked'), 'true')
      const box = await relay.boundingBox()
      assert(box && box.x >= 0 && box.x + box.width <= width)
      await page.screenshot({ path: `${output}/${theme}-${width}.png`, fullPage: true })
      await dialog.getByRole('button', { name: 'Close modal', exact: true }).click()
      checks.push({ theme, width, disabled_when_unconfigured: true, keyboard_toggle: true })
    }
    await context.close()
  }
  assert.deepEqual(errors, [])
  await writeFile(output + '/checks.json', JSON.stringify(checks, null, 2))
} finally {
  await browser.close()
}
