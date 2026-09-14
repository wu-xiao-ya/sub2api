import { apiClient } from './client'

export interface PerformanceMetric {
  first_character_ms: number | null
  duration_ms: number | null
  output_tps: number | null
  success_rate: number | null
  sample_quality: 'empty' | 'low' | 'adequate'
  coverage_status: 'empty' | 'partial' | 'complete'
}
export interface PerformancePoint extends PerformanceMetric { at: string }
export interface PerformanceGroup extends PerformanceMetric {
  id: number
  name: string
  trend: PerformancePoint[]
}
export interface PerformanceItem extends PerformanceMetric {
  key: string
  model: string
  platform: string
  groups?: PerformanceGroup[]
  trend?: PerformancePoint[]
}
export interface PerformanceResult {
  items: PerformanceItem[]
  version: number
  source: 'user_requests'
  start: string
  end: string
  updated_at: string | null
  coverage_start: string | null
  coverage_end: string | null
  incomplete_since: string | null
}
export interface PerformanceFilter {
  key?: string
  range?: '90m' | '24h' | '7d' | '30d'
  group_id?: number
  service_tier?: string
  reasoning_effort?: string
  stream?: boolean
}
export async function getChannelPerformance(params: PerformanceFilter, signal?: AbortSignal) {
  const { data } = await apiClient.get<PerformanceResult>(
    params.key ? '/channels/performance/detail' : '/channels/performance', { params, signal }
  )
  return data
}

export function formatPerformanceValue(value: number | null | undefined, unit: 'ms' | 'tps' | 'percent') {
  if (value == null || !Number.isFinite(value)) return '\u2014'
  if (unit === 'ms') return (value / 1000).toFixed(2) + ' s'
  return value.toFixed(1) + (unit === 'percent' ? '%' : ' tok/s')
}
