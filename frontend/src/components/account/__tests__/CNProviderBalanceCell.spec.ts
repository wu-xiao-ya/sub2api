import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CNProviderBalanceCell from '../CNProviderBalanceCell.vue'
import type { Account } from '@/types'

const { queryBalance, getAccountCredits } = vi.hoisted(() => ({
  queryBalance: vi.fn(),
  getAccountCredits: vi.fn()
}))

vi.mock('@/api/admin/plugins', () => ({ getAccountCredits }))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    cnProviders: { queryBalance }
  }
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
    locale: { value: 'en-US' }
  })
}))

const account = {
  id: 7,
  platform: 'kimi',
  type: 'apikey',
  credentials: { account_mode: 'payg' },
  extra: {
    kimi_balance: 12.5,
    kimi_balance_currency: 'CNY'
  }
} as Account

describe('CNProviderBalanceCell', () => {
  beforeEach(() => {
    queryBalance.mockReset()
    getAccountCredits.mockReset()
  })

  it('reuses the query control for WorkBuddy and formats upstream credits to two decimals', async () => {
    getAccountCredits.mockResolvedValue({ remaining: 2136.68000105, limit: 3000 })
    const wrapper = mount(CNProviderBalanceCell, {
      props: { account: { ...account, type: 'plugin', platform: 'deepseek' } }
    })
    const button = wrapper.get('[data-test="cn-provider-balance-probe"]')
    expect(button.text()).toBe('admin.accounts.pluginCredits.query')
    await button.trigger('click')
    await flushPromises()
    expect(getAccountCredits).toHaveBeenCalledWith(account.id)
    expect(queryBalance).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toContain('2,136.68 / 3,000.00')
  })

  it('renders the persisted balance as static text with an explicit query action', async () => {
    const wrapper = mount(CNProviderBalanceCell, { props: { account } })
    await flushPromises()

    // Snapshot value renders without any probe.
    expect(queryBalance).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="cn-provider-balance-value"]').text()).toContain('CNY 12.50')

    // The control reads as an action; the i18n mock returns the key itself.
    const probeButton = wrapper.get('[data-test="cn-provider-balance-probe"]')
    expect(probeButton.text()).toBe('admin.accounts.cnProviders.probe')

    await probeButton.trigger('click')
    await flushPromises()
    expect(queryBalance).toHaveBeenCalledWith(account.id)
  })

  it('shows the low-balance badge from the snapshot marker', () => {
    const lowAccount = {
      ...account,
      extra: { kimi_balance: 0.4, kimi_balance_low: true }
    } as Account

    const wrapper = mount(CNProviderBalanceCell, { props: { account: lowAccount } })

    expect(wrapper.text()).toContain('admin.accounts.cnProviders.balanceLow')
  })

  it('keeps the snapshot balance visible when a query fails', async () => {
    queryBalance.mockResolvedValue({ success: false, error: 'HTTP 401' })
    const wrapper = mount(CNProviderBalanceCell, { props: { account } })

    await wrapper.get('[data-test="cn-provider-balance-probe"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('CNY 12.50')
    expect(wrapper.text()).toContain('HTTP 401')
  })
})
