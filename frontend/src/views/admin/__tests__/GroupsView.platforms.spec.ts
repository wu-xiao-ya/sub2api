import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const currentDir = dirname(fileURLToPath(import.meta.url))
const groupsView = readFileSync(resolve(currentDir, '../GroupsView.vue'), 'utf8')
const zhOverview = readFileSync(resolve(currentDir, '../../../i18n/locales/zh/admin/overview.ts'), 'utf8')
const enOverview = readFileSync(resolve(currentDir, '../../../i18n/locales/en/admin/overview.ts'), 'utf8')

describe('GroupsView compatible platform display', () => {
  it('renders labels and distinct platform colors for DeepSeek, Kimi, and GLM', () => {
    for (const platform of ['deepseek', 'kimi', 'glm', 'qwen', 'minimax', 'mimo', 'hunyuan']) {
      expect(groupsView).toContain(`value === '${platform}'`)
      expect(zhOverview).toContain(`${platform}: '`)
      expect(enOverview).toContain(`${platform}: '`)
    }
    expect(groupsView).toContain('bg-blue-100 text-blue-700')
    expect(groupsView).toContain('bg-cyan-100 text-cyan-700')
    expect(groupsView).toContain('bg-indigo-100 text-indigo-700')
  })
})
