import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PlatformIcon from '../PlatformIcon.vue'
import GroupBadge from '../GroupBadge.vue'
import GroupOptionItem from '../GroupOptionItem.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: null }) }))
describe('domestic brand presentation', () => {
  it.each(['qwen', 'hunyuan', 'mimo'] as const)('renders the %s brand mark without a letter placeholder', provider => {
    const icon = mount(PlatformIcon, { props: { platform: 'openai', upstreamProvider: provider } })
    expect(icon.find('text').exists()).toBe(false)
    expect(icon.find('path').attributes('d').length).toBeGreaterThan(100)
    expect(icon.find('svg').attributes('aria-label')).toBeTruthy()
  })
  it.each([['qwen', 'bg-violet-50'], ['hunyuan', 'bg-blue-50'], ['mimo', 'bg-zinc-50']] as const)('uses the %s palette for both badge and rate pill', (provider, color) => {
    const props = { platform: 'openai' as const, upstreamProvider: provider, name: '标准线路', rateMultiplier: 1 }
    expect(mount(GroupBadge, { props }).classes()).toContain(color)
    const option = mount(GroupOptionItem, { props })
    expect(option.findAll('.' + color).length).toBeGreaterThanOrEqual(2)
    expect(option.findAll('.bg-green-50')).toHaveLength(0)
  })
  it('keeps generic OpenAI styling', () => {
    expect(mount(GroupBadge, { props: { platform: 'openai', name: 'GPT' } }).classes()).toContain('bg-green-50')
  })
  it('does not repeat a brand already present in the group name', () => {
    expect(mount(GroupBadge, { props: { platform: 'openai', upstreamProvider: 'qwen', name: '千问高速' } }).find('.opacity-75').exists()).toBe(false)
  })
})
