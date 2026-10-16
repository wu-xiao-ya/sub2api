import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import BaseDialog from '../BaseDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('BaseDialog', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    document.body.classList.remove('modal-open')
  })

  it('resets body scroll position when reopened', async () => {
    const wrapper = mount(BaseDialog, {
      attachTo: document.body,
      props: { show: false, title: 'Details' },
      slots: { default: '<div style="height: 2000px">content</div>' },
      global: { stubs: { Icon: true } }
    })

    await wrapper.setProps({ show: true })
    await nextTick()
    const body = document.body.querySelector<HTMLElement>('.modal-body')
    expect(body).not.toBeNull()
    body!.scrollTop = 480

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(document.body.querySelector<HTMLElement>('.modal-body')?.scrollTop).toBe(0)
    wrapper.unmount()
  })
})

describe('BaseDialog keyboard lifecycle', () => {
  afterEach(() => {
    document.body.replaceChildren()
    vi.restoreAllMocks()
  })

  it('restores the opening control when an always-open dialog is unmounted', async () => {
    const entry = document.createElement('button')
    document.body.append(entry)
    entry.focus()
    const wrapper = mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'Performance' } })
    await nextTick()
    expect(document.activeElement?.getAttribute('aria-label')).toBe('Close modal')
    wrapper.unmount()
    expect(document.activeElement).toBe(entry)
  })

  it('cycles Tab and Shift+Tab within visible enabled controls', async () => {
    vi.spyOn(HTMLElement.prototype, 'getClientRects').mockReturnValue([{}] as unknown as DOMRectList)
    const wrapper = mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'Performance' },
      slots: { default: '<button disabled>Unavailable</button><select aria-label="Range"><option>24h</option></select>' } })
    await nextTick()
    const first = document.querySelector<HTMLButtonElement>('[aria-label="Close modal"]')!
    const last = document.querySelector<HTMLSelectElement>('select')!
    first.focus()
    first.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(last)
    last.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    expect(document.activeElement).toBe(first)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('also restores focus when show changes to false', async () => {
    const entry = document.createElement('button')
    document.body.append(entry)
    entry.focus()
    const wrapper = mount(BaseDialog, { attachTo: document.body, props: { show: true, title: 'Performance' } })
    await nextTick()
    await wrapper.setProps({ show: false })
    expect(document.activeElement).toBe(entry)
    wrapper.unmount()
  })
})
