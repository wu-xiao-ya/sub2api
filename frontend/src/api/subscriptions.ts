/** User-facing Starlight shared subscription purchase API. */
import { apiClient } from './client'

export interface SharedSubscriptionGroup {
  id: number
  name: string
  platform: string
}

export interface SharedSubscription {
  id: number
  name: string
  tier_code: 'standard' | 'pro' | 'plus' | string
  starts_at: string
  expires_at: string
  status: string
  concurrency_entitlement: number
  lifetime_quota_usd: number
  daily_quota_usd: number
  weekly_quota_usd: number
  monthly_quota_usd: number
  lifetime_usage_usd: number
  daily_usage_usd: number
  weekly_usage_usd: number
  monthly_usage_usd: number
  balance_topup_enabled: boolean
  groups: SharedSubscriptionGroup[]
}

export async function getMySharedSubscriptions(): Promise<SharedSubscription[]> {
  const response = await apiClient.get<SharedSubscription[]>('/subscriptions/shared')
  return response.data
}

export async function setSharedBalanceTopup(
  purchaseId: number,
  enabled: boolean,
): Promise<Pick<SharedSubscription, 'id' | 'balance_topup_enabled'>> {
  const response = await apiClient.patch<Pick<SharedSubscription, 'id' | 'balance_topup_enabled'>>(
    `/subscriptions/shared/${purchaseId}/balance-topup`,
    { enabled },
  )
  return response.data
}

export default {
  getMySharedSubscriptions,
  setSharedBalanceTopup,
}
