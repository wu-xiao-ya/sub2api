import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

import type { DashboardStats } from '@/types'
import DashboardView from '../DashboardView.vue'

const { getSnapshotV2, getUserUsageTrend, getUserSpendingRanking } = vi.hoisted(() => ({
  getSnapshotV2: vi.fn(),
  getUserUsageTrend: vi.fn(),
  getUserSpendingRanking: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    dashboard: {
      getSnapshotV2,
      getUserUsageTrend,
      getUserSpendingRanking
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const createDashboardStats = (overrides: Partial<DashboardStats> = {}): DashboardStats => ({
  total_users: 0,
  today_new_users: 0,
  active_users: 0,
  hourly_active_users: 0,
  stats_updated_at: '',
  stats_stale: false,
  total_api_keys: 0,
  active_api_keys: 0,
  total_accounts: 0,
  normal_accounts: 0,
  error_accounts: 0,
  ratelimit_accounts: 0,
  overload_accounts: 0,
  total_requests: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 0,
  total_cost: 0,
  total_actual_cost: 0,
  today_requests: 0,
  today_input_tokens: 0,
  today_output_tokens: 0,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 0,
  today_cost: 0,
  today_actual_cost: 0,
  average_duration_ms: 0,
  uptime: 0,
  rpm: 0,
  tpm: 0,
  ...overrides
})

describe('admin DashboardView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    window.localStorage.clear()

    getSnapshotV2.mockReset()
    getUserUsageTrend.mockReset()
    getUserSpendingRanking.mockReset()

    getSnapshotV2.mockResolvedValue({
      stats: createDashboardStats(),
      trend: [],
      models: []
    })
    getUserUsageTrend.mockResolvedValue({
      trend: [],
      start_date: '',
      end_date: '',
      granularity: 'hour'
    })
    getUserSpendingRanking.mockResolvedValue({
      ranking: [],
      total_actual_cost: 0,
      total_requests: 0,
      total_tokens: 0,
      start_date: '',
      end_date: ''
    })
  })

  it('uses last 24 hours as default dashboard range', async () => {
    mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
          DateRangePicker: true,
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true,
          Line: true
        }
      }
    })

    await flushPromises()

    const now = new Date()
    const yesterday = new Date(now.getTime() - 24 * 60 * 60 * 1000)

    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      start_date: formatLocalDate(yesterday),
      end_date: formatLocalDate(now),
      granularity: 'hour',
      include_group_stats: true
    }))
  })

  it('shows separate today and historical cache hit rates using all prompt tokens', async () => {
    getSnapshotV2.mockResolvedValue({
      stats: createDashboardStats({
        today_input_tokens: 500,
        today_cache_creation_tokens: 0,
        today_cache_read_tokens: 1500,
        total_input_tokens: 200,
        total_cache_creation_tokens: 300,
        total_cache_read_tokens: 500,
      }),
      trend: [],
      models: []
    })

    const wrapper = mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
          DateRangePicker: true,
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true,
          Line: true
        }
      }
    })

    await flushPromises()

    expect(wrapper.get('[data-testid="admin-cache-hit-rate"]').text()).toBe('75.0%')
    expect(wrapper.get('[data-testid="admin-historical-cache-hit-rate"]').text()).toBe('50.0%')
  })

  it('renders time-range cost and profit metrics by group', async () => {
    getSnapshotV2.mockResolvedValue({
      stats: createDashboardStats(),
      trend: [],
      models: [],
      groups: [
        {
          group_id: 1,
          group_name: 'gpt-pro',
          requests: 10,
          total_tokens: 1000,
          cost: 20,
          actual_cost: 4,
          account_cost: 2.4,
          upstream_cost: 2.4,
          upstream_multiplier: 0.12,
          profit: 1.6,
          profit_margin: 0.4
        }
      ],
      cost_profit: {
        requests: 10,
        total_tokens: 1000,
        standard_cost: 20,
        actual_cost: 4,
        upstream_cost: 2.4,
        upstream_multiplier: 0.12,
        profit: 1.6,
        profit_margin: 0.4
      }
    })

    const wrapper = mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
          DateRangePicker: true,
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true,
          Line: true
        }
      }
    })

    await flushPromises()

    expect(wrapper.text()).toContain('gpt-pro')
    expect(wrapper.text()).toContain('$4.00')
    expect(wrapper.text()).toContain('$2.40')
    expect(wrapper.text()).toContain('$1.60')
    expect(wrapper.text()).toContain('40.0%')
    expect(wrapper.text()).toContain('0.1200x')
  })

  it('sends the saved report blacklist with the dashboard request', async () => {
    window.localStorage.setItem(
      'sub2api.admin.dashboard.costProfitExcludedUsers',
      '42, blocked@example.com'
    )

    mount(DashboardView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          LoadingSpinner: true,
          Icon: true,
          DateRangePicker: true,
          Select: true,
          ModelDistributionChart: true,
          TokenUsageTrend: true,
          Line: true
        }
      }
    })

    await flushPromises()

    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      exclude_users: '42, blocked@example.com'
    }))
  })
})
