import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const currentDir = dirname(fileURLToPath(import.meta.url))
const viewSource = readFileSync(resolve(currentDir, '../AvailableChannelsView.vue'), 'utf8')
const zh = readFileSync(resolve(currentDir, '../../../i18n/locales/zh/dashboard.ts'), 'utf8')
const en = readFileSync(resolve(currentDir, '../../../i18n/locales/en/dashboard.ts'), 'utf8')

describe('available channels DeepSeek time pricing copy', () => {
  it('shows standard prices and 2x Beijing peak windows', () => {
    expect(viewSource).toContain("availableChannels.deepSeekTimePricingDescription")
    expect(zh).toContain('DeepSeek 展示标准（非高峰）价格')
    expect(zh).toContain('09:00-12:00')
    expect(zh).toContain('14:00-18:00')
    expect(zh).toContain('2 倍')
    expect(zh).not.toContain('峰值价格')
    expect(zh).not.toContain('50%')
    expect(en).toContain('standard (off-peak) pricing')
    expect(en).toContain('09:00-12:00')
    expect(en).toContain('14:00-18:00')
    expect(en).toContain('2x the displayed price')
    expect(en).not.toContain('displays peak pricing')
    expect(en).not.toContain('50% of the displayed price')
  })
})
