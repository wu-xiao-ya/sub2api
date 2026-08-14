import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const currentDir = dirname(fileURLToPath(import.meta.url))
const source = readFileSync(resolve(currentDir, '../ChannelsView.vue'), 'utf8')

describe('admin ChannelsView source contract', () => {
  it('keeps the platform order and apiToForm contract aligned for channel pricing', () => {
    expect(source).toMatch(/const platformOrder: GroupPlatform\[\] = \[[^\]]*'deepseek'[^\]]*'kimi'[^\]]*'glm'[^\]]*\]/)
    expect(source).toContain("activePlatforms.add(p.platform as GroupPlatform)")
    expect(source).toContain("const bedrockCCCompatEnabled = fc?.bedrock_cc_compat === true")
  })
})
