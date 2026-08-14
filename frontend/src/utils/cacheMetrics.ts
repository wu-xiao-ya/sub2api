export function calculateCacheHitRate(
  inputTokens: number,
  cacheCreationTokens: number,
  cacheReadTokens: number
): number {
  const input = Math.max(0, Number(inputTokens) || 0)
  const cacheCreation = Math.max(0, Number(cacheCreationTokens) || 0)
  const cacheRead = Math.max(0, Number(cacheReadTokens) || 0)
  const totalPromptTokens = input + cacheCreation + cacheRead

  return totalPromptTokens > 0 ? (cacheRead / totalPromptTokens) * 100 : 0
}
