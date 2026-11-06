import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CacheRateLabel from '../CacheRateLabel.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('CacheRateLabel', () => {
  it('distinguishes no input data from a measured zero hit rate', () => {
    expect(mount(CacheRateLabel, { props: { numerator: 0, denominator: 0 } }).text()).toContain('—')
    expect(mount(CacheRateLabel, { props: { numerator: 0, denominator: 100 } }).text()).toContain('0.0%')
  })
  it('shows token-weighted hit share including cache creation', () => {
    const wrapper = mount(CacheRateLabel, { props: { numerator: 80, denominator: 100 + 20 + 80 } })
    expect(wrapper.text()).toContain('40.0%')
    expect(wrapper.attributes('title')).toBe('usage.cacheRateHint')
  })
})
