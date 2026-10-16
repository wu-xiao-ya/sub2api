/**
 * 格式化缓存 token 数量（1K/1M 缩写）
 */
export function formatCacheTokens(tokens: number): string {
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`
  return tokens.toLocaleString()
}

/**
 * 自适应精度格式化倍率（确保小数值如 0.001 不被截断）
 */
export function formatMultiplier(val: number): string {
  if (!Number.isFinite(val)) return '0.00'

  const fixed = val.toFixed(8)
  const trimmed = fixed.replace(/(\.\d*?[1-9])0+$|\.0+$/, '$1')
  const [integerPart, fractionPart = ''] = trimmed.split('.')

  if (fractionPart.length === 0) return `${integerPart}.00`
  if (fractionPart.length === 1) return `${integerPart}.${fractionPart}0`
  return trimmed
}
