import {
  durationSeverity,
  firstTokenSeverity,
  type LatencySeverity,
} from '@/utils/latencyHealth'

export interface LatencyMetricsInput {
  duration_ms: number | null | undefined
  first_token_ms: number | null | undefined
  output_tokens?: number | null
  is_image?: boolean
}

export interface LatencyMetrics {
  firstTokenMs: number | null
  durationMs: number | null
  generationMs: number | null
  outputSpeed: number | null
  firstTokenRatio: number | null
  generationRatio: number | null
  severity: LatencySeverity | null
}

const validMilliseconds = (value: number | null | undefined): number | null => {
  if (value == null || !Number.isFinite(value) || value < 0) return null
  return value
}

export const calculateLatencyMetrics = ({
  duration_ms,
  first_token_ms,
  output_tokens,
  is_image = false,
}: LatencyMetricsInput): LatencyMetrics => {
  const durationMs = validMilliseconds(duration_ms)
  const firstTokenMs = validMilliseconds(first_token_ms)
  const outputTokens = output_tokens != null && Number.isFinite(output_tokens) && output_tokens > 0
    ? output_tokens
    : null

  if (durationMs == null) {
    return {
      firstTokenMs,
      durationMs: null,
      generationMs: null,
      outputSpeed: null,
      firstTokenRatio: null,
      generationRatio: null,
      severity: null,
    }
  }

  const generationMs = firstTokenMs == null
    ? null
    : Math.max(durationMs - firstTokenMs, 0)
  const speedDurationMs = generationMs == null ? durationMs : generationMs
  const outputSpeed = is_image || outputTokens == null || speedDurationMs <= 0
    ? null
    : outputTokens / (speedDurationMs / 1000)

  return {
    firstTokenMs,
    durationMs,
    generationMs,
    outputSpeed: outputSpeed != null && Number.isFinite(outputSpeed) ? outputSpeed : null,
    firstTokenRatio: firstTokenMs == null || durationMs <= 0
      ? null
      : Math.min(firstTokenMs / durationMs, 1),
    generationRatio: generationMs == null || durationMs <= 0
      ? null
      : generationMs / durationMs,
    severity: firstTokenMs == null ? durationSeverity(durationMs) : firstTokenSeverity(firstTokenMs),
  }
}

export const formatOutputSpeed = (speed: number | null | undefined): string => {
  if (speed == null || !Number.isFinite(speed) || speed <= 0) return '-'
  return `${Number(speed.toFixed(2)).toString()} Token/s`
}

export const formatLatencyRatio = (ratio: number | null | undefined): string => {
  if (ratio == null || !Number.isFinite(ratio)) return '-'
  return `${Number((ratio * 100).toFixed(1)).toString()}%`
}
