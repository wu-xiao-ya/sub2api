import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, patch } = vi.hoisted(() => ({
  get: vi.fn(),
  patch: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, patch },
}))

import subscriptionsAPI from '@/api/subscriptions'

describe('shared subscriptions api', () => {
  beforeEach(() => {
    get.mockReset()
    patch.mockReset()
  })

  it('lists shared purchase snapshots without calling legacy endpoints', async () => {
    const purchases = [{ id: 8, name: 'Pro', groups: [] }]
    get.mockResolvedValue({ data: purchases })

    await expect(subscriptionsAPI.getMySharedSubscriptions()).resolves.toEqual(purchases)
    expect(get).toHaveBeenCalledOnce()
    expect(get).toHaveBeenCalledWith('/subscriptions/shared')
  })

  it('reads and updates the global balance top-up preference', async () => {
    get.mockResolvedValue({ data: { balance_topup_enabled: false } })
    await expect(subscriptionsAPI.getSubscriptionPreferences()).resolves.toEqual({
      balance_topup_enabled: false,
    })
    expect(get).toHaveBeenCalledWith('/subscriptions/preferences')

    patch.mockResolvedValue({ data: { balance_topup_enabled: true } })
    await expect(subscriptionsAPI.setSubscriptionBalanceTopupPreference(true)).resolves.toEqual({
      balance_topup_enabled: true,
    })
    expect(patch).toHaveBeenCalledWith('/subscriptions/preferences/balance-topup', { enabled: true })
  })

  it('updates shared balance top-up by purchase id', async () => {
    patch.mockResolvedValue({ data: { id: 8, balance_topup_enabled: true } })

    await expect(subscriptionsAPI.setSharedBalanceTopup(8, true)).resolves.toEqual({
      id: 8,
      balance_topup_enabled: true,
    })
    expect(patch).toHaveBeenCalledWith('/subscriptions/shared/8/balance-topup', { enabled: true })
  })
})
