import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const routerPath = resolve(dirname(fileURLToPath(import.meta.url)), '../index.ts')
const routerSource = readFileSync(routerPath, 'utf8')

describe('legacy admin subscriptions route cleanup', () => {
  it('does not register the removed admin subscriptions route', () => {
    expect(routerSource).not.toContain("path: '/admin/subscriptions'")
    expect(routerSource).not.toContain("name: 'AdminSubscriptions'")
  })

  it('continues to create history from the detected app base path', () => {
    expect(routerSource).toContain('createWebHistory(getAppBasePath())')
  })
})
