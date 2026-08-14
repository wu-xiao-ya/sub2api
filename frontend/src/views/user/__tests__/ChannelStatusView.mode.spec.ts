import { describe, expect, it, vi, beforeEach } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'

const getMode = vi.fn<() => 'v1' | 'v2'>().mockReturnValue('v2')
const replace = vi.fn()
const route = { query: {} as Record<string, string> }

vi.mock('@/utils/featureFlags', () => ({
  getChannelMonitorMode: () => getMode(),
}))

vi.mock('../ChannelStatusV1View.vue', () => ({
  default: defineComponent({
    name: 'ChannelStatusV1View',
    props: { embedded: { type: Boolean, default: false } },
    setup: (props) => () => h('div', { 'data-testid': 'v1', 'data-embedded': String(props.embedded) }),
  }),
}))
vi.mock('../ChannelStatusV2View.vue', () => ({
  default: defineComponent({
    name: 'ChannelStatusV2View',
    props: { embedded: { type: Boolean, default: false } },
    setup: (props) => () => h('div', { 'data-testid': 'v2', 'data-embedded': String(props.embedded) }),
  }),
}))
vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ replace }),
}))

import ChannelStatusView from '../ChannelStatusView.vue'

describe('ChannelStatusView mode switch', () => {
  beforeEach(() => {
    getMode.mockReset()
    replace.mockReset()
    route.query = {}
  })

  function mountView() {
    return mount(ChannelStatusView, {
      global: {
        stubs: {
          AppLayout: defineComponent({ template: '<main><slot /></main>' }),
        },
      },
    })
  }

  it('renders V2 by default and embeds the selected view', () => {
    getMode.mockReturnValue('v2')
    const wrapper = mountView()
    expect(wrapper.find('[data-testid="v2"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="v2"]').attributes('data-embedded')).toBe('true')
  })

  it('uses the configured legacy view only as the initial selection', () => {
    getMode.mockReturnValue('v1')
    const wrapper = mountView()
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="v2"]').exists()).toBe(false)
  })

  it('lets a user switch views and persists the selection in the URL', async () => {
    getMode.mockReturnValue('v2')
    const wrapper = mountView()

    await wrapper.findAll('[role="tab"]')[1].trigger('click')
    await nextTick()

    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(true)
    expect(replace).toHaveBeenCalledWith({ query: { view: 'v1' } })
  })
})
