import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import Toggle from '../Toggle.vue'

describe('Toggle', () => {
  it('does not change a disabled switch', async () => {
    const wrapper = mount(Toggle, { props: { modelValue: true, disabled: true } })
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.get('button').attributes('aria-checked')).toBe('true')
  })

  it('preserves normal switch behavior', async () => {
    const wrapper = mount(Toggle, { props: { modelValue: false } })
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[true]])
  })
})
