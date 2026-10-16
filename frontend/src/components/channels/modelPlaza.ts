import type {
  UserAvailableChannel,
  UserAvailableGroup,
  UserSupportedModel,
} from '@/api/channels'

/**
 * A user-facing model card stays scoped to one channel/platform section.
 * Keeping the source channel in the key avoids silently merging model entries
 * that happen to share a public name but have different pricing or groups.
 */
export interface ModelPlazaItem {
  key: string
  channelName: string
  channelDescription: string
  platform: string
  groups: UserAvailableGroup[]
  model: UserSupportedModel
}

export function buildModelPlazaItems(channels: UserAvailableChannel[]): ModelPlazaItem[] {
  return channels.flatMap(channel =>
    channel.platforms.flatMap(section =>
      section.supported_models.map(model => ({
        key: `${channel.name}\u0000${section.platform}\u0000${model.name}`,
        channelName: channel.name,
        channelDescription: channel.description,
        platform: section.platform,
        groups: section.groups,
        model,
      })),
    ),
  )
}

export function effectiveGroupRate(
  group: UserAvailableGroup,
  userGroupRates: Record<number, number>,
): number {
  return userGroupRates[group.id] ?? group.rate_multiplier
}

export function lowestGroupRate(
  groups: UserAvailableGroup[],
  userGroupRates: Record<number, number>,
): number | null {
  if (groups.length === 0) return null
  return Math.min(...groups.map(group => effectiveGroupRate(group, userGroupRates)))
}
