import { describe, expect, it } from 'vitest'

import { calculateCacheHitRate } from '../cacheMetrics'

describe('calculateCacheHitRate', () => {
  it('uses all prompt tokens as the denominator', () => {
    expect(calculateCacheHitRate(500, 0, 1500)).toBe(75)
    expect(calculateCacheHitRate(200, 300, 500)).toBe(50)
  })

  it('returns zero when there are no prompt tokens', () => {
    expect(calculateCacheHitRate(0, 0, 0)).toBe(0)
  })

  it('does not let invalid or negative counters distort the rate', () => {
    expect(calculateCacheHitRate(Number.NaN, -100, 25)).toBe(100)
  })
})
