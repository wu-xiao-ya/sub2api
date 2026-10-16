import { apiClient } from '../client'

export interface AdminPurchaseGroup {
  id: number
  name: string
  platform: string
}

export interface AdminPurchaseRecord {
  id: number
  user_id: number
  user_email: string
  username: string
  plan_id?: number
  name: string
  tier_code: string
  price: number
  currency: string
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
  billing_priority: 'subscription' | 'balance'
  source: string
  source_id?: number
  notes: string
  groups: AdminPurchaseGroup[]
}

export interface AdminPurchaseListResponse {
  items: AdminPurchaseRecord[]
  total: number
  page: number
  page_size: number
}

export interface AdminPurchaseBulkResult {
  success_count: number
  failed_count: number
  items: AdminPurchaseRecord[]
  errors: string[]
}

const adminSubscriptionPurchasesAPI = {
  list(params?: {
    page?: number
    page_size?: number
    user_id?: number
    plan_id?: number
    status?: string
    platform?: string
    keyword?: string
  }) {
    return apiClient.get<AdminPurchaseListResponse>('/admin/subscription-purchases', { params })
  },
  get(id: number) {
    return apiClient.get<AdminPurchaseRecord>(`/admin/subscription-purchases/${id}`)
  },
  grant(data: { user_id: number; plan_id: number; notes?: string }) {
    return apiClient.post<AdminPurchaseRecord>('/admin/subscription-purchases/grant', data)
  },
  bulkGrant(data: { user_ids: number[]; plan_id: number; notes?: string }) {
    return apiClient.post<AdminPurchaseBulkResult>('/admin/subscription-purchases/bulk-grant', data)
  },
  extend(id: number, days: number) {
    return apiClient.post<AdminPurchaseRecord>(`/admin/subscription-purchases/${id}/extend`, { days })
  },
  revoke(id: number) {
    return apiClient.post<AdminPurchaseRecord>(`/admin/subscription-purchases/${id}/revoke`)
  },
  restore(id: number) {
    return apiClient.post<AdminPurchaseRecord>(`/admin/subscription-purchases/${id}/restore`)
  },
  resetQuota(id: number, data: { daily: boolean; weekly: boolean; monthly: boolean }) {
    return apiClient.post<AdminPurchaseRecord>(`/admin/subscription-purchases/${id}/reset-quota`, data)
  },
}

export default adminSubscriptionPurchasesAPI
