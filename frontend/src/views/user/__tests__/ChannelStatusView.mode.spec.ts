import { describe, expect, it, vi, beforeEach } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'

const replace = vi.fn()
const route = { query: {} as Record<string, string> }

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

  it('renders legacy V1 by default and embeds the selected view', () => {
    const wrapper = mountView()
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="v2"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="v1"]').attributes('data-embedded')).toBe('true')
  })

  it('restores the optional V2 selection from the URL', () => {
    route.query = { view: 'v2' }
    const wrapper = mountView()
    expect(wrapper.find('[data-testid="v2"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="v1"]').exists()).toBe(false)
  })

  it('lets a user switch views and persists the selection in the URL', async () => {
    const wrapper = mountView()

    await wrapper.findAll('[role="tab"]')[0].trigger('click')
    await nextTick()

    expect(wrapper.find('[data-testid="v2"]').exists()).toBe(true)
    expect(replace).toHaveBeenCalledWith({ query: { view: 'v2' } })
  })
})
