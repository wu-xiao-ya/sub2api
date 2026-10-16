import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('retired intelligence probe', () => {
  for (const file of [
    '../views/admin/SettingsView.vue',
    '../views/admin/ChannelMonitorView.vue',
    '../views/user/ChannelStatusV1View.vue',
  ]) {
    it(`has no active feature entry in ${file}`, () => {
      const source = readFileSync(new URL(file, import.meta.url), 'utf8')
      expect(source).not.toMatch(/IntelligenceProbe|ProbeGallery|intelligenceItems|loadIntelligenceResults/)
      expect(source).not.toContain("'intelligence'")
    })
  }
})
