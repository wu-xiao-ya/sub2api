import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AvailableChannelsTable.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('AvailableChannelsTable directory layout', () => {
  it('uses a responsive channel card grid instead of a wide table', () => {
    expect(componentSource).toContain('grid grid-cols-1 items-start gap-3 xl:grid-cols-2')
    expect(componentSource).not.toContain('<table')
  })

  it('keeps platform rows inside each channel card instead of nesting cards', () => {
    expect(componentSource).toContain('sm:grid-cols-[7.5rem_minmax(0,1fr)]')
    expect(componentSource).not.toContain('rounded-2xl')
  })
})
