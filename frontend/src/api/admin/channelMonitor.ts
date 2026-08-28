/**
 * Admin Channel Monitor API endpoints
 * Handles channel monitor (uptime/health) management for administrators
 */

import { apiClient } from '../client'

export type Provider =
  | 'openai'
  | 'anthropic'
  | 'gemini'
  | 'grok'
  | 'antigravity'
  | 'deepseek'
  | 'kimi'
  | 'glm'
  | 'qwen'
  | 'minimax'
  | 'mimo'
  | 'hunyuan'
export type MonitorStatus = 'operational' | 'degraded' | 'failed' | 'error'
export type BodyOverrideMode = 'off' | 'merge' | 'replace'
export type APIMode = 'chat_completions' | 'responses' | 'models' | 'images'
export type MonitorSourceMode = 'direct_upstream' | 'internal_gateway'

export interface UsageLatencyBreakdown {
  first_response_ms?: number
  first_event_ms?: number
  first_output_ms?: number
  first_character_ms?: number
  total_duration_ms?: number
}

export interface ChannelMonitor {
  id: number
  name: string
  provider: Provider
  api_mode: APIMode
  endpoint: string
  api_key_masked: string
  source_mode: MonitorSourceMode
  internal_api_key_id?: number | null
  internal_group_id?: number | null
  /**
   * True when the stored encrypted API key cannot be decrypted (e.g. the
   * encryption key has changed). Admin must re-edit the monitor to provide
   * a fresh key. Backend skips checks for these monitors.
   */
  api_key_decrypt_failed?: boolean
  primary_model: string
  extra_models: string[]
  group_name: string
  /** Explicit account-management group used by adaptive account probes. */
  account_group_id?: number | null
  enabled: boolean
  interval_seconds: number
  /** ????? interval ??? ? [0, jitter] ?????????0 = ???? */
  jitter_seconds: number
  /** One upstream check's maximum wait time. Images can use a longer value. */
  request_timeout_seconds: number
  last_checked_at: string | null
  created_by: number
  created_at: string
  updated_at: string
  /** Latest status of the primary model (empty when no history yet) */
  primary_status: MonitorStatus | ''
  /** Latest latency of the primary model in ms (null when no history yet) */
  primary_latency_ms: number | null
  primary_latency_breakdown?: UsageLatencyBreakdown
  /** Admin-only metadata for the most recent primary-model probe. */
  primary_account_id?: number | null
  primary_account_name?: string
  primary_probe_mode?: 'static' | 'sticky' | 'confirm' | 'full' | 'traffic'
  primary_candidate_count?: number
  primary_healthy_count?: number
  primary_checked_at?: string | null
  /** Primary model 7-day availability percentage (0-100) */
  availability_7d: number
  /** Latest status per extra model (used for hover tooltip) */
  extra_models_status: ExtraModelStatus[]
  /** ??????????????? */
  template_id: number | null
  extra_headers: Record<string, string>
  body_override_mode: BodyOverrideMode
  body_override: Record<string, unknown> | null
}

export interface ExtraModelStatus {
  model: string
  status: MonitorStatus | ''
  latency_ms: number | null
  latency_breakdown?: UsageLatencyBreakdown
  availability_7d?: number
}

export interface ListParams {
  page?: number
  page_size?: number
  provider?: Provider
  enabled?: boolean
  search?: string
}

export interface ListResponse {
  items: ChannelMonitor[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface CreateParams {
  name: string
  provider: Provider
  api_mode?: APIMode
  endpoint: string
  api_key: string
  source_mode?: MonitorSourceMode
  internal_api_key_id?: number | null
  internal_group_id?: number | null
  primary_model: string
  extra_models?: string[]
  group_name?: string
  account_group_id?: number | null
  enabled?: boolean
  interval_seconds: number
  jitter_seconds?: number
  request_timeout_seconds?: number
  template_id?: number | null
  extra_headers?: Record<string, string>
  body_override_mode?: BodyOverrideMode
  body_override?: Record<string, unknown> | null
}

// Update request: api_key ?? = ????clear_template=true ?? template_id ??
export type UpdateParams = Partial<CreateParams> & {
  clear_template?: boolean
  clear_account_group?: boolean
}

export interface CheckResult {
  model: string
  status: MonitorStatus
  latency_ms: number | null
  latency_breakdown?: UsageLatencyBreakdown
  ping_latency_ms: number | null
  account_id?: number | null
  account_name?: string
  probe_mode?: 'static' | 'sticky' | 'confirm' | 'full' | 'traffic'
  candidate_count?: number
  healthy_count?: number
  message: string
  checked_at: string
}

export interface RunNowResponse {
  results: CheckResult[]
}

export interface HistoryItem {
  id: number
  model: string
  status: MonitorStatus
  latency_ms: number | null
  latency_breakdown?: UsageLatencyBreakdown
  ping_latency_ms: number | null
  account_id?: number | null
  account_name?: string
  probe_mode?: 'static' | 'sticky' | 'confirm' | 'full' | 'traffic'
  candidate_count?: number
  healthy_count?: number
  message: string
  checked_at: string
}

export interface HistoryParams {
  model?: string
  limit?: number
}

export interface HistoryResponse {
  items: HistoryItem[]
}

export interface InternalMonitorKey {
  id: number
  name: string
  user_id: number
  group_id: number
  group_name: string
  provider: Provider
  status: string
  expires_at?: string | null
}

export interface EnsureInternalMonitorKeysResponse {
  user_id: number
  user_email: string
  items: Array<InternalMonitorKey & {
    plain_key?: string
    created?: boolean
  }>
}

export async function listInternalKeys(): Promise<{ items: InternalMonitorKey[] }> {
  const { data } = await apiClient.get<{ items: InternalMonitorKey[] }>(
    '/admin/channel-monitors/internal-keys'
  )
  return data
}

export async function ensureInternalKeys(
  groupIds: number[],
): Promise<EnsureInternalMonitorKeysResponse> {
  const { data } = await apiClient.post<EnsureInternalMonitorKeysResponse>(
    '/admin/channel-monitors/internal-keys/ensure',
    { group_ids: groupIds },
  )
  return data
}

/**
 * List channel monitors with pagination and filters
 */
export async function list(
  params: ListParams = {},
  options?: { signal?: AbortSignal }
): Promise<ListResponse> {
  const { data } = await apiClient.get<ListResponse>('/admin/channel-monitors', {
    params,
    signal: options?.signal,
  })
  return data
}

/**
 * Get a channel monitor by ID
 */
export async function get(id: number): Promise<ChannelMonitor> {
  const { data } = await apiClient.get<ChannelMonitor>(`/admin/channel-monitors/${id}`)
  return data
}

/**
 * Create a new channel monitor
 */
export async function create(params: CreateParams): Promise<ChannelMonitor> {
  const { data } = await apiClient.post<ChannelMonitor>('/admin/channel-monitors', params)
  return data
}

/**
 * Duplicate a monitor without exposing its stored API key to the browser.
 * Keep the operation key after ambiguous failures so a retry replays the
 * original server-side operation instead of creating another monitor.
 */
const duplicateOperationKeys = new Map<string, string>()

interface DuplicateOperationScope {
  adminID: string
  key: string
}

function getCurrentAdminID(): string | null {
  try {
    const rawUser = globalThis.localStorage?.getItem('auth_user')
    if (!rawUser) return null

    const user: unknown = JSON.parse(rawUser)
    if (typeof user !== 'object' || user === null) return null

    const id = (user as { id?: unknown }).id
    if (typeof id !== 'number' || !Number.isSafeInteger(id) || id <= 0) return null
    return String(id)
  } catch {
    return null
  }
}

function duplicateOperationScope(id: number): DuplicateOperationScope | null {
  const adminID = getCurrentAdminID()
  if (!adminID) return null

  return {
    adminID,
    key: `sub2api:admin:channel-monitor-duplicate:${adminID}:${id}`,
  }
}

function getStoredDuplicateOperationKey(storageKey: string): string | null {
  try {
    return globalThis.sessionStorage?.getItem(storageKey) ?? null
  } catch {
    return null
  }
}

function storeDuplicateOperationKey(storageKey: string, key: string | null): void {
  try {
    if (key) globalThis.sessionStorage?.setItem(storageKey, key)
    else globalThis.sessionStorage?.removeItem(storageKey)
  } catch {
    // In-memory retry protection still works when browser storage is unavailable.
  }
}

export async function duplicate(id: number): Promise<ChannelMonitor> {
  const scope = duplicateOperationScope(id)
  let idempotencyKey = scope
    ? duplicateOperationKeys.get(scope.key) ?? getStoredDuplicateOperationKey(scope.key)
    : null
  if (!idempotencyKey) {
    const requestID = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
    idempotencyKey = `channel-monitor-duplicate-${scope?.adminID ?? 'unknown-admin'}-${id}-${requestID}`
  }
  if (scope) {
    duplicateOperationKeys.set(scope.key, idempotencyKey)
    storeDuplicateOperationKey(scope.key, idempotencyKey)
  }

  const { data } = await apiClient.post<ChannelMonitor>(
    `/admin/channel-monitors/${id}/duplicate`,
    undefined,
    { headers: { 'Idempotency-Key': idempotencyKey } }
  )

  if (scope) {
    duplicateOperationKeys.delete(scope.key)
    storeDuplicateOperationKey(scope.key, null)
  }
  return data
}

/**
 * Update an existing channel monitor.
 * api_key field: empty string means "do not modify".
 */
export async function update(id: number, params: UpdateParams): Promise<ChannelMonitor> {
  const { data } = await apiClient.put<ChannelMonitor>(`/admin/channel-monitors/${id}`, params)
  return data
}

/**
 * Delete a channel monitor
 */
export async function del(id: number): Promise<void> {
  await apiClient.delete(`/admin/channel-monitors/${id}`)
}

/**
 * Trigger an immediate manual check for a channel monitor.
 * Returns the latest check results for primary + extra models.
 */
export async function runNow(id: number, options: { forceFull?: boolean } = {}): Promise<RunNowResponse> {
  // Image monitors may legitimately wait up to the configured 15-minute cap.
  const { data } = await apiClient.post<RunNowResponse>(
    `/admin/channel-monitors/${id}/run`,
    options.forceFull ? { force_full: true } : undefined,
    { timeout: 930000 }
  )
  return data
}

/**
 * List historical check results for a monitor.
 */
export async function listHistory(
  id: number,
  params: HistoryParams = {}
): Promise<HistoryResponse> {
  const { data } = await apiClient.get<HistoryResponse>(
    `/admin/channel-monitors/${id}/history`,
    { params }
  )
  return data
}

/**
 * Fetch the one most recent successful generated image for a monitor.
 */
export async function getLatestImage(id: number): Promise<Blob> {
  const { data } = await apiClient.get<Blob>(`/admin/channel-monitors/${id}/image`, {
    responseType: 'blob',
  })
  return data
}

export const channelMonitorAPI = {
  list,
  get,
  create,
  duplicate,
  update,
  del,
  runNow,
  listHistory,
  listInternalKeys,
  ensureInternalKeys,
  getLatestImage,
}

export default channelMonitorAPI
