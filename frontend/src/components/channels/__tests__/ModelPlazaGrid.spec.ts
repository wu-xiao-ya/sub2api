import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../ModelPlazaGrid.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('ModelPlazaGrid', () => {
  it('uses a responsive model card gallery', () => {
    expect(componentSource).toContain(
      'grid grid-cols-1 gap-4 sm:grid-cols-2 2xl:grid-cols-3 min-[1900px]:grid-cols-4',
    )
    expect(componentSource).toContain('<ModelPlazaCard')
    expect(componentSource).not.toContain('<table')
  })

  it('forwards a group selection from cards to the parent filter', () => {
    expect(componentSource).toContain("@toggle-group=\"emit('toggleGroup', $event)\"")
  })
})
