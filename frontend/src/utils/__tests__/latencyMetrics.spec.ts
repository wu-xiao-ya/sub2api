import { describe, expect, it } from 'vitest'

import {
  calculateLatencyMetrics,
  formatLatencyRatio,
  formatOutputSpeed,
} from '../latencyMetrics'

describe('latency metrics', () => {
  it('calculates generation duration and output speed from first response', () => {
    const metrics = calculateLatencyMetrics({
      duration_ms: 1800,
      first_token_ms: 1600,
      output_tokens: 31,
    })

    expect(metrics.generationMs).toBe(200)
    expect(metrics.outputSpeed).toBe(155)
    expect(metrics.firstTokenRatio).toBeCloseTo(1600 / 1800)
    expect(metrics.generationRatio).toBeCloseTo(200 / 1800)
    expect(metrics.severity).toBe('good')
  })

  it('falls back to total duration when first response is missing', () => {
    const metrics = calculateLatencyMetrics({
      duration_ms: 2000,
      first_token_ms: null,
      output_tokens: 10,
    })

    expect(metrics.generationMs).toBeNull()
    expect(metrics.outputSpeed).toBe(5)
    expect(metrics.firstTokenRatio).toBeNull()
    expect(metrics.generationRatio).toBeNull()
  })

  it('clamps generation duration when first response exceeds total duration', () => {
    const metrics = calculateLatencyMetrics({
      duration_ms: 1000,
      first_token_ms: 1200,
      output_tokens: 10,
    })

    expect(metrics.generationMs).toBe(0)
    expect(metrics.outputSpeed).toBeNull()
    expect(metrics.firstTokenRatio).toBe(1)
    expect(metrics.generationRatio).toBe(0)
  })

  it('does not calculate text speed for image requests or zero output', () => {
    expect(calculateLatencyMetrics({
      duration_ms: 1000,
      first_token_ms: 200,
      output_tokens: 20,
      is_image: true,
    }).outputSpeed).toBeNull()

    expect(calculateLatencyMetrics({
      duration_ms: 1000,
      first_token_ms: 200,
      output_tokens: 0,
    }).outputSpeed).toBeNull()
  })

  it('rejects invalid duration values', () => {
    expect(calculateLatencyMetrics({
      duration_ms: -1,
      first_token_ms: 100,
      output_tokens: 10,
    })).toEqual({
      firstTokenMs: 100,
      durationMs: null,
      generationMs: null,
      outputSpeed: null,
      firstTokenRatio: null,
      generationRatio: null,
      severity: null,
    })
  })

  it('does not produce invalid ratios for zero-duration requests', () => {
    const metrics = calculateLatencyMetrics({
      duration_ms: 0,
      first_token_ms: 0,
      output_tokens: 10,
    })

    expect(metrics.generationMs).toBe(0)
    expect(metrics.outputSpeed).toBeNull()
    expect(metrics.firstTokenRatio).toBeNull()
    expect(metrics.generationRatio).toBeNull()
  })

  it('formats speed and ratios without noisy trailing zeroes', () => {
    expect(formatOutputSpeed(155)).toBe('155 Token/s')
    expect(formatOutputSpeed(32.9)).toBe('32.9 Token/s')
    expect(formatOutputSpeed(303.3033)).toBe('303.3 Token/s')
    expect(formatOutputSpeed(null)).toBe('-')
    expect(formatLatencyRatio(0.5)).toBe('50%')
    expect(formatLatencyRatio(null)).toBe('-')
  })
})
