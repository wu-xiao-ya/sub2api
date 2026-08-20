import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it } from 'vitest'
import RegionRestrictedView from '@/views/public/RegionRestrictedView.vue'
import { useAppStore } from '@/stores/app'

describe('RegionRestrictedView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('renders the platform brand, effective date, and detected country', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/region-restricted', component: RegionRestrictedView }],
    })
    await router.push('/region-restricted?country=CN')
    await router.isReady()

    const appStore = useAppStore()
    appStore.siteName = 'PetraFlux Labs'
    appStore.siteLogo = '/custom-logo.png'
    appStore.contactInfo = 'Telegram: @petraflux'

    const wrapper = mount(RegionRestrictedView, {
      global: {
        plugins: [router],
      },
    })

    expect(wrapper.text()).toContain('暂不向中国大陆地区提供服务')
    expect(wrapper.text()).toContain('2026年8月20日')
    expect(wrapper.text()).toContain('REGION CN')
    expect(wrapper.text()).toContain('PetraFlux Labs')
    expect(wrapper.text()).toContain('Telegram: @petraflux')
    expect(wrapper.get('.restriction-logo').attributes('src')).toBe('/custom-logo.png')
    expect(wrapper.find('a').exists()).toBe(false)
  })
})
