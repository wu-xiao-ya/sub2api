import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import { vi } from 'vitest'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => ({
    'payment.days': 'days', 'payment.perMonth': 'month', 'payment.perYear': 'year',
    'payment.subscribeNow': 'Subscribe now', 'payment.planCard.quota': 'Quota', 'payment.planCard.unlimited': 'Unlimited',
    'payment.admin.includedGroups': 'Included groups', 'payment.admin.concurrency': 'Concurrency',
    'payment.admin.lifetimeQuota': 'Lifetime', 'payment.admin.dailyQuota': 'Daily',
    'payment.admin.weeklyQuota': 'Weekly', 'payment.admin.monthlyQuota': 'Monthly',
    'payment.admin.tierStandard': 'Standard', 'payment.admin.tierPro': 'Pro', 'payment.admin.tierPlus': 'Plus',
  } as Record<string, string>)[key] || key }),
}))
import type { SubscriptionPlan } from '@/types/payment'
import SubscriptionPlanCard from '../SubscriptionPlanCard.vue'

function mountPlanCard(overrides: Partial<SubscriptionPlan> = {}) {
  return mount(SubscriptionPlanCard, {
    props: {
      plan: {
        id: 1, group_id: 10, group_ids: [10, 11], group_platform: 'openai', tier_code: 'pro',
        name: 'Pro plan', description: '', price: 10, currency: '', features: [], validity_days: 30,
        validity_unit: 'day', for_sale: true, sort_order: 0, concurrency_entitlement: 4,
        lifetime_quota_usd: 100, daily_quota_usd: 10, weekly_quota_usd: 50, monthly_quota_usd: 100,
        ...overrides,
      },
    },
  })
}

describe('SubscriptionPlanCard', () => {
  it('shows Starlight tier, included groups, concurrency, and shared quotas', () => {
    const text = mountPlanCard().text()
    expect(text).toContain('Pro')
    expect(text).toContain('Included groups2')
    expect(text).toContain('Concurrency4')
    expect(text).toContain('Lifetime$100.00')
    expect(text).toContain('Daily$10.00')
  })

  it('falls back to the compatibility primary group when group_ids is missing', () => {
    expect(mountPlanCard({ group_ids: undefined }).text()).toContain('Included groups1')
  })

  it('uses the configured currency symbol while preserving USD defaults', () => {
    expect(mountPlanCard({ currency: 'CNY', original_price: 20 }).text()).toContain('¥10CNY')
    expect(mountPlanCard({ currency: 'USD' }).text()).toContain('$10USD')
    expect(mountPlanCard({ currency: '' }).text()).toContain('$10')
  })
})
