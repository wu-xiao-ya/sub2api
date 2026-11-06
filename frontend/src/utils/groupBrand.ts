import type { GroupPlatform, UpstreamProvider } from '@/types'
export const DOMESTIC_UPSTREAMS = ['qwen', 'hunyuan', 'mimo'] as const
export const UPSTREAM_LABELS: Record<string, string> = { qwen: 'Qwen / 千问', hunyuan: 'Hunyuan / 混元', mimo: 'MiMo' }
export function getGroupBrand(group: { platform?: GroupPlatform | string; upstream_provider?: string }) {
  return group.platform === 'openai' && DOMESTIC_UPSTREAMS.includes(group.upstream_provider as typeof DOMESTIC_UPSTREAMS[number]) ? group.upstream_provider! : group.platform || ''
}
export function groupBrandLabel(group: { platform?: GroupPlatform | string; upstream_provider?: string }) {
  const brand = getGroupBrand(group)
  return UPSTREAM_LABELS[brand] || brand
}
export function upstreamOptions(defaultLabel: string): { value: UpstreamProvider; label: string }[] {
  return [{ value: '', label: defaultLabel }, ...DOMESTIC_UPSTREAMS.map(value => ({ value, label: UPSTREAM_LABELS[value]! }))]
}
