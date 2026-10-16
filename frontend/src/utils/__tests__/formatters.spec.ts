import { describe, expect, it } from 'vitest'
import { formatMultiplier } from '@/utils/formatters'

describe('formatMultiplier', () => {
  it('preserves fractional promotion multipliers instead of rounding to two decimals', () => {
    expect(formatMultiplier(0.075)).toBe('0.075')
    expect(formatMultiplier(0.0725)).toBe('0.0725')
    expect(formatMultiplier(0.095)).toBe('0.095')
  })

  it('keeps the existing readable form for ordinary multipliers', () => {
    expect(formatMultiplier(1)).toBe('1.00')
    expect(formatMultiplier(0.1)).toBe('0.10')
    expect(formatMultiplier(2.5)).toBe('2.50')
  })

  it('trims insignificant trailing zeros while retaining precision', () => {
    expect(formatMultiplier(0.12345678)).toBe('0.12345678')
    expect(formatMultiplier(0.123456789)).toBe('0.12345679')
  })
})
