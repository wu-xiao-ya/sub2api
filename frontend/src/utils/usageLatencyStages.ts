export interface UsageLatencyBreakdown {
  version?: number
  attempt_count?: number
  forward_start_ms?: number
  first_response_ms?: number
  first_event_ms?: number
  first_output_ms?: number
  first_character_ms?: number
  total_duration_ms?: number
}

export const latencyStageKeys = [
  'first_response_ms', 'first_event_ms', 'first_output_ms',
  'first_character_ms', 'total_duration_ms',
] as const

export const latencyStageLabels = [
  'latencyFirstResponse', 'latencyFirstEvent', 'latencyFirstOutput',
  'latencyFirstCharacter', 'latencyDuration',
] as const

export function usageLatencyStages(breakdown?: UsageLatencyBreakdown | null, legacyDuration?: number | null) {
  let previous: number | null = null
  let previousLabel: string | null = null
  return latencyStageKeys.map((key, index) => {
    const raw = breakdown?.[key] ?? (key === 'total_duration_ms' && breakdown?.version !== 2 ? legacyDuration : null)
    const value = raw != null && Number.isFinite(raw) && raw >= 0 ? raw : null
    const delta = value != null && previous != null && value >= previous ? value - previous : null
    const fromLabel = previousLabel
    if (value != null) {
      previous = value
      previousLabel = latencyStageLabels[index]
    }
    return { key, label: latencyStageLabels[index], value, delta, fromLabel }
  })
}

export function latencyTimingOrigin(breakdown?: UsageLatencyBreakdown | null, legacyDuration?: number | null) {
  if (!breakdown || !latencyStageKeys.some(key => breakdown[key] != null)) {
    return legacyDuration != null && Number.isFinite(legacyDuration) && legacyDuration >= 0 ? 'latencyOriginForward' : 'latencyOriginUnknown'
  }
  return breakdown.version === 2 ? 'latencyOriginIngress' : 'latencyOriginForward'
}

export function latencyCSVValues(breakdown?: UsageLatencyBreakdown | null, legacyDuration?: number | null) {
  const stages = usageLatencyStages(breakdown, legacyDuration)
  const version = breakdown?.version ?? (stages.some(stage => stage.value != null) ? 1 : '')
  return [version, ...stages.map(stage => stage.value ?? '')]
}
