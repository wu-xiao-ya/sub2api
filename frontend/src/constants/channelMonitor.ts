/**
 * Channel monitor shared constants.
 *
 * Single source of truth for provider/status string values used by both the
 * admin (`views/admin/ChannelMonitorView.vue`) and user-facing
 * (`views/user/ChannelStatusView.vue`) screens, plus the shared composable
 * `useChannelMonitorFormat`.
 */

import type { APIMode, Provider, MonitorStatus } from '@/api/admin/channelMonitor'

export const PROVIDER_OPENAI: Provider = 'openai'
export const PROVIDER_ANTHROPIC: Provider = 'anthropic'
export const PROVIDER_GEMINI: Provider = 'gemini'
export const PROVIDER_GROK: Provider = 'grok'
export const PROVIDER_ANTIGRAVITY: Provider = 'antigravity'
export const PROVIDER_DEEPSEEK: Provider = 'deepseek'
export const PROVIDER_KIMI: Provider = 'kimi'
export const PROVIDER_GLM: Provider = 'glm'
export const PROVIDER_QWEN: Provider = 'qwen'
export const PROVIDER_MINIMAX: Provider = 'minimax'
export const PROVIDER_MIMO: Provider = 'mimo'
export const PROVIDER_HUNYUAN: Provider = 'hunyuan'

export const DEFAULT_GROK_ENDPOINT = 'https://api.x.ai'
export const DEFAULT_GROK_MODEL = 'grok-4.5'

export const API_MODE_CHAT_COMPLETIONS: APIMode = 'chat_completions'
export const API_MODE_RESPONSES: APIMode = 'responses'
export const API_MODE_MODELS: APIMode = 'models'
export const API_MODE_IMAGES: APIMode = 'images'

export const PROVIDERS: readonly Provider[] = [
  PROVIDER_OPENAI,
  PROVIDER_ANTHROPIC,
  PROVIDER_GEMINI,
  PROVIDER_GROK,
  PROVIDER_ANTIGRAVITY,
  PROVIDER_DEEPSEEK,
  PROVIDER_KIMI,
  PROVIDER_GLM,
  PROVIDER_QWEN,
  PROVIDER_MINIMAX,
  PROVIDER_MIMO,
  PROVIDER_HUNYUAN,
]

/** Only OpenAI monitors expose the protocol-specific mode selector. */
export function supportsSelectableAPIMode(provider: Provider | string): boolean {
  return provider === PROVIDER_OPENAI
}

export const API_MODES: readonly APIMode[] = [
  API_MODE_CHAT_COMPLETIONS,
  API_MODE_RESPONSES,
  API_MODE_MODELS,
  API_MODE_IMAGES,
]

export const STATUS_OPERATIONAL: MonitorStatus = 'operational'
export const STATUS_DEGRADED: MonitorStatus = 'degraded'
export const STATUS_FAILED: MonitorStatus = 'failed'
export const STATUS_ERROR: MonitorStatus = 'error'

export const MONITOR_STATUSES: readonly MonitorStatus[] = [
  STATUS_OPERATIONAL,
  STATUS_DEGRADED,
  STATUS_FAILED,
  STATUS_ERROR,
]

/** Default polling interval (seconds) for new monitors. */
export const DEFAULT_INTERVAL_SECONDS = 60
