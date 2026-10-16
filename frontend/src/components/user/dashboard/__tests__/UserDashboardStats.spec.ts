import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import type { UserDashboardStats } from '@/api/usage'
import UserDashboardStatsComponent from '../UserDashboardStats.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const createStats = (): UserDashboardStats => ({
  total_api_keys: 1,
  active_api_keys: 1,
  total_requests: 10,
  total_input_tokens: 200,
  total_output_tokens: 100,
  total_cache_creation_tokens: 300,
  total_cache_read_tokens: 500,
  total_tokens: 1100,
  total_cost: 1,
  total_actual_cost: 0.5,
  today_requests: 2,
  today_input_tokens: 500,
  today_output_tokens: 100,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 1500,
  today_tokens: 2100,
  today_cost: 0.2,
  today_actual_cost: 0.1,
  average_duration_ms: 120,
  rpm: 2,
  tpm: 100,
  by_platform: []
})

describe('UserDashboardStats', () => {
  it('shows separate today and historical cache hit rates using all prompt tokens', () => {
    const wrapper = mount(UserDashboardStatsComponent, {
      props: {
        stats: createStats(),
        balance: 0,
        isSimple: true,
        platformQuotas: []
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    expect(wrapper.get('[data-testid="user-cache-hit-rate"]').text()).toBe('75.0%')
    expect(wrapper.get('[data-testid="user-historical-cache-hit-rate"]').text()).toBe('50.0%')
  })
})
