import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'

const { getShared, setTopup, push, showError } = vi.hoisted(() => ({
  getShared: vi.fn(),
  setTopup: vi.fn(),
  push: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/subscriptions', () => ({
  default: {
    getMySharedSubscriptions: getShared,
    setSharedBalanceTopup: setTopup,
  },
}))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError }) }))

import SubscriptionsView from '../SubscriptionsView.vue'

const messages = {
  en: {
    nav: { buySubscription: 'Buy subscription' },
    common: { today: 'today', tomorrow: 'tomorrow' },
    userSubscriptions: {
      noActiveSubscriptions: 'No subscriptions', noActiveSubscriptionsDesc: 'Choose a plan',
      sharedTitle: 'Subscriptions', sharedDescription: 'Purchase snapshots', sharedGroups: 'Groups',
      sharedConcurrency: 'Concurrency', balanceTopup: 'Use balance', lifetime: 'Lifetime', daily: 'Daily', weekly: 'Weekly', monthly: 'Monthly', unlimited: 'Unlimited',
      failedToLoad: 'Load failed', failedToUpdate: 'Update failed', daysRemaining: '{days} days remaining',
      status: { active: 'Active', expired: 'Expired' },
    },
    payment: { admin: { tierStandard: 'Standard', tierPro: 'Pro', tierPlus: 'Plus' } },
  },
}

function mountView() {
  return mount(SubscriptionsView, {
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', messages })],
      stubs: { AppLayout: { template: '<main><slot /></main>' }, Icon: true },
    },
  })
}

const purchase = {
  id: 7, name: 'Starlight Pro', tier_code: 'pro', starts_at: '2026-01-01T00:00:00Z', expires_at: '2099-01-01T00:00:00Z', status: 'active',
  concurrency_entitlement: 4, lifetime_quota_usd: 100, daily_quota_usd: 10, weekly_quota_usd: 50, monthly_quota_usd: 100,
  lifetime_usage_usd: 5, daily_usage_usd: 1, weekly_usage_usd: 2, monthly_usage_usd: 3, balance_topup_enabled: false,
  groups: [{ id: 3, name: 'OpenAI A', platform: 'openai' }],
}

describe('SubscriptionsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getShared.mockResolvedValue([structuredClone(purchase)])
    setTopup.mockResolvedValue({ id: 7, balance_topup_enabled: true })
  })

  it('renders only shared purchase snapshots', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(getShared).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('Starlight Pro')
    expect(wrapper.text()).toContain('OpenAI A')
    expect(wrapper.text()).toContain('Pro')
  })

  it('updates balance top-up from the shared purchase endpoint', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('input[type="checkbox"]').trigger('change')
    await flushPromises()

    expect(setTopup).toHaveBeenCalledWith(7, true)
    expect((wrapper.get('input[type="checkbox"]').element as HTMLInputElement).checked).toBe(true)
  })

  it('links the empty state to the canonical purchase route', async () => {
    getShared.mockResolvedValue([])
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('button').trigger('click')

    expect(push).toHaveBeenCalledWith({ path: '/purchase', query: { tab: 'subscription' } })
  })
})
