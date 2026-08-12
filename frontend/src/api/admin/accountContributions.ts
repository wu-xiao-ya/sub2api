import { apiClient } from '../client'
import type { Account, PaginatedResponse } from '@/types'

export type ContributionListStatus = 'pending' | 'approved' | 'rejected' | 'revoked' | 'all'

export interface ApproveContributionRequest {
  group_ids: number[]
  concurrency?: number
  priority?: number
}

export async function listContributions(
  page = 1,
  pageSize = 20,
  status: ContributionListStatus = 'pending'
): Promise<PaginatedResponse<Account>> {
  const { data } = await apiClient.get<PaginatedResponse<Account>>('/admin/account-contributions', {
    params: { page, page_size: pageSize, status }
  })
  return data
}

export async function listPendingContributions(
  page = 1,
  pageSize = 20
): Promise<PaginatedResponse<Account>> {
  return listContributions(page, pageSize, 'pending')
}

export async function approveContribution(
  id: number,
  payload: ApproveContributionRequest
): Promise<Account> {
  const { data } = await apiClient.post<Account>(
    `/admin/account-contributions/${id}/approve`,
    payload
  )
  return data
}

export async function rejectContribution(id: number): Promise<Account> {
  const { data } = await apiClient.post<Account>(`/admin/account-contributions/${id}/reject`)
  return data
}

export const adminAccountContributionsAPI = {
  list: listContributions,
  listPending: listPendingContributions,
  approve: approveContribution,
  reject: rejectContribution
}

export default adminAccountContributionsAPI
