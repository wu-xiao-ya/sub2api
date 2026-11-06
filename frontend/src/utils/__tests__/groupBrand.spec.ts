import { describe, it, expect } from 'vitest'
import { getGroupBrand, groupBrandLabel } from '../groupBrand'
import { getKeyGroupProvider } from '../keyGroupProviders'
describe('domestic upstream brands', () => {
 it.each(['qwen', 'hunyuan', 'mimo'])('classifies %s independently of the routing platform', provider => {
   const group = { platform: 'openai', upstream_provider: provider }
   expect(getGroupBrand(group)).toBe(provider)
   expect(getKeyGroupProvider('openai', provider)).toBe('domestic')
   expect(groupBrandLabel(group)).not.toBe('openai')
 })
 it('keeps generic OpenAI and rejects misapplied or unknown brands', () => {
   expect(getKeyGroupProvider('openai')).toBe('openai')
   expect(getKeyGroupProvider('openai', 'unknown')).toBe('openai')
   expect(getGroupBrand({platform:'anthropic',upstream_provider:'qwen'})).toBe('anthropic')
   expect(getKeyGroupProvider('kimi')).toBe('domestic')
 })
})
