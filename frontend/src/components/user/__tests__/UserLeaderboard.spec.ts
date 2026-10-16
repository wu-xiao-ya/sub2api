import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getLeaderboard, type LeaderboardResponse } from '@/api/leaderboard'
import UserLeaderboard from '../UserLeaderboard.vue'

vi.mock('@/api/leaderboard', () => ({ getLeaderboard: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const response = (name = 'Visible user'): LeaderboardResponse => ({
  period: 'today', start_date: '2026-10-16', end_date: '2026-10-16',
  leaderboard: Array.from({ length: 4 }, (_, index) => ({
    rank: index + 1, user_id: index + 10, display_name: `${name} ${index}`,
    total_tokens: 1000 - index, requests: 4, cost: 0.01, is_current_user: index === 0,
  })),
})
const render = () => mount(UserLeaderboard, {
  global: { stubs: { Icon: true, LoadingSpinner: true } },
})

describe('UserLeaderboard', () => {
  beforeEach(() => vi.resetAllMocks())

  it('shows every rank, including the current user and the first three request counts', async () => {
    vi.mocked(getLeaderboard).mockResolvedValue(response())
    const wrapper = render()
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(4)
    expect(wrapper.find('[data-current-user="true"]').text()).toContain('leaderboard.you')
    expect(wrapper.find('tbody tr').text()).toContain('4')
    expect(getLeaderboard).toHaveBeenCalledWith({
      period: 'today', limit: 20, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    })
    wrapper.unmount()
  })

  it('distinguishes a request failure from an empty ranking and supports retry', async () => {
    vi.mocked(getLeaderboard).mockRejectedValueOnce(new Error('unavailable')).mockResolvedValue(response())
    const wrapper = render()
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('leaderboard.errorLoading')
    expect(wrapper.text()).not.toContain('leaderboard.empty')
    await wrapper.find('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(4)
    wrapper.unmount()
  })

  it('displays the empty state only after a successful empty response', async () => {
    vi.mocked(getLeaderboard).mockResolvedValue({ ...response(), leaderboard: [] })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('leaderboard.empty')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('does not let a late response replace a newer period', async () => {
    let resolveWeek!: (result: LeaderboardResponse) => void
    vi.mocked(getLeaderboard).mockResolvedValueOnce(response())
    const wrapper = render()
    await flushPromises()
    vi.mocked(getLeaderboard)
      .mockImplementationOnce(() => new Promise(resolve => { resolveWeek = resolve }))
      .mockResolvedValueOnce(response('Month'))
    await wrapper.find('[data-period="week"]').trigger('click')
    await wrapper.find('[data-period="month"]').trigger('click')
    await flushPromises()
    resolveWeek(response('Stale week'))
    await flushPromises()
    expect(wrapper.text()).toContain('Month')
    expect(wrapper.text()).not.toContain('Stale week')
    expect(wrapper.find('[data-period="month"]').attributes('aria-selected')).toBe('true')
    wrapper.unmount()
  })
})
