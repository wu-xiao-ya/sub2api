import { apiClient } from './client'
import type {
  Account,
  AccountUsageInfo,
  AdminDataAccount,
  AdminDataPayload,
  ContributorRewardLog,
  ContributorRewardSummary,
  PaginatedResponse,
  TempUnschedulableRule,
  WindowStats
} from '@/types'

export interface ContributionAuthURLRequest {
  proxy_id?: number | null
  redirect_uri?: string
}

export interface ContributionAuthURLResult {
  auth_url: string
  session_id: string
}

export interface SubmitOpenAIContributionRequest {
  session_id: string
  code: string
  state: string
  redirect_uri?: string
  proxy_id?: number | null
  proxy_url?: string
  name?: string
}

export interface SubmitOpenAIJSONContributionRequest {
  data?: Pick<AdminDataPayload, 'type' | 'version' | 'proxies' | 'accounts'>
  accounts?: AdminDataAccount[]
  proxy_id?: number | null
  proxy_url?: string
}

export interface ContributionImportItem {
  index: number
  name?: string
  account_id?: number
  action: 'created' | 'failed' | string
  message?: string
}

export interface ContributionImportError {
  index: number
  name?: string
  message: string
}

export interface ContributionImportResult {
  total: number
  created: number
  failed: number
  items?: ContributionImportItem[]
  errors?: ContributionImportError[]
}

export interface ContributionImportPreviewItem {
  index: number
  name?: string
  valid: boolean
  duplicate: boolean
  unsupported: boolean
  invalid: boolean
  identity_present: boolean
  message?: string
}

export interface ContributionImportPreview {
  total: number
  valid: number
  duplicate: number
  unsupported: number
  invalid: number
  items?: ContributionImportPreviewItem[]
}

export interface ContributionAccountConfigRequest {
  name?: string
  notes?: string | null
  concurrency?: number
  load_factor?: number | null
  expires_at?: number | null
  auto_pause_on_expired?: boolean
  temp_unschedulable_enabled?: boolean
  temp_unschedulable_rules?: TempUnschedulableRule[]
  auto_pause_5h_threshold?: number | null
  auto_pause_7d_threshold?: number | null
  auto_pause_5h_disabled?: boolean
  auto_pause_7d_disabled?: boolean
}

export interface ContributionBatchTodayStatsResponse {
  stats: Record<string, WindowStats>
}

export async function generateOpenAIContributionAuthURL(
  payload: ContributionAuthURLRequest = {}
): Promise<ContributionAuthURLResult> {
  const { data } = await apiClient.post<ContributionAuthURLResult>(
    '/account-contributions/openai/auth-url',
    payload
  )
  return data
}

export async function submitOpenAIContribution(
  payload: SubmitOpenAIContributionRequest
): Promise<Account> {
  const { data } = await apiClient.post<Account>(
    '/account-contributions/openai/exchange-code',
    payload
  )
  return data
}


export async function previewOpenAIJSONContribution(
  payload: SubmitOpenAIJSONContributionRequest
): Promise<ContributionImportPreview> {
  const { data } = await apiClient.post<ContributionImportPreview>(
    '/account-contributions/openai/import-json/preview',
    payload
  )
  return data
}

export async function submitOpenAIJSONContribution(
  payload: SubmitOpenAIJSONContributionRequest
): Promise<ContributionImportResult> {
  const { data } = await apiClient.post<ContributionImportResult>(
    '/account-contributions/openai/import-json',
    payload
  )
  return data
}

export async function listMyContributions(
  page = 1,
  pageSize = 20
): Promise<PaginatedResponse<Account>> {
  const { data } = await apiClient.get<PaginatedResponse<Account>>('/account-contributions', {
    params: { page, page_size: pageSize }
  })
  return data
}

export async function revokeContribution(id: number): Promise<Account> {
  const { data } = await apiClient.delete<Account>(`/account-contributions/${id}`)
  return data
}

export async function republishContribution(id: number): Promise<Account> {
  const { data } = await apiClient.post<Account>(`/account-contributions/${id}/republish`)
  return data
}

export async function updateContributionConfig(
  id: number,
  payload: ContributionAccountConfigRequest
): Promise<Account> {
  const { data } = await apiClient.put<Account>(`/account-contributions/${id}/config`, payload)
  return data
}

export async function getContributionUsage(
  id: number,
  source?: 'passive' | 'active',
  force?: boolean
): Promise<AccountUsageInfo> {
  const params: Record<string, string> = {}
  if (source) params.source = source
  if (force) params.force = 'true'
  const { data } = await apiClient.get<AccountUsageInfo>(`/account-contributions/${id}/usage`, {
    params: Object.keys(params).length > 0 ? params : undefined
  })
  return data
}

export async function getContributionTodayStats(id: number): Promise<WindowStats> {
  const { data } = await apiClient.get<WindowStats>(`/account-contributions/${id}/today-stats`)
  return data
}

export async function getContributionBatchTodayStats(
  accountIds: number[]
): Promise<ContributionBatchTodayStatsResponse> {
  const { data } = await apiClient.post<ContributionBatchTodayStatsResponse>(
    '/account-contributions/today-stats/batch',
    { account_ids: accountIds }
  )
  return data
}

export async function getContributionRewardSummary(): Promise<ContributorRewardSummary> {
  const { data } = await apiClient.get<ContributorRewardSummary>(
    '/account-contributions/rewards/summary'
  )
  return data
}

export async function listContributionRewards(
  page = 1,
  pageSize = 20
): Promise<PaginatedResponse<ContributorRewardLog>> {
  const { data } = await apiClient.get<PaginatedResponse<ContributorRewardLog>>(
    '/account-contributions/rewards',
    { params: { page, page_size: pageSize } }
  )
  return data
}

export const accountContributionsAPI = {
  generateOpenAIAuthURL: generateOpenAIContributionAuthURL,
  submitOpenAI: submitOpenAIContribution,
  previewOpenAIJSON: previewOpenAIJSONContribution,
  submitOpenAIJSON: submitOpenAIJSONContribution,
  listMine: listMyContributions,
  revoke: revokeContribution,
  republish: republishContribution,
  updateConfig: updateContributionConfig,
  getUsage: getContributionUsage,
  getTodayStats: getContributionTodayStats,
  getBatchTodayStats: getContributionBatchTodayStats,
  getRewardSummary: getContributionRewardSummary,
  listRewards: listContributionRewards
}

export default accountContributionsAPI
