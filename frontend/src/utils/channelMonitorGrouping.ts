import type {
  MonitorStatus,
  UserMonitorExtraModel,
  UserMonitorView,
} from '@/api/channelMonitor'
import {
  STATUS_DEGRADED,
  STATUS_ERROR,
  STATUS_FAILED,
  STATUS_OPERATIONAL,
} from '@/constants/channelMonitor'

export interface GroupedChannelModel {
  monitorId: number
  model: string
  status: MonitorStatus | ''
  latency_ms: number | null
  source: ChannelMonitorSource | null
  availability_7d: number | null
  timeline: UserMonitorView['timeline']
  isPrimary: boolean
}

export interface GroupedChannelStatus {
  key: string
  name: string
  groupName: string
  provider: UserMonitorView['provider']
  apiMode: UserMonitorView['api_mode']
  source: ChannelSourceGroup
  monitorIds: number[]
  models: GroupedChannelModel[]
  leadModel: GroupedChannelModel
  imageMonitorId: number | null
}

export type GroupedChannelHealth =
  | 'operational'
  | 'slow_response'
  | 'partial'
  | 'unavailable'

interface MutableGroupedChannelStatus
  extends Omit<GroupedChannelStatus, 'models' | 'leadModel'> {
  modelsByName: Map<string, GroupedChannelModel>
}

export type ChannelMonitorSource = NonNullable<UserMonitorView['primary_source']>
export type ChannelSourceGroup = ChannelMonitorSource | 'mixed' | null

function channelKey(item: UserMonitorView): string {
  const displayName = item.group_name.trim() || item.name.trim() || `monitor-${item.id}`
  return [item.provider, item.api_mode, displayName].join('\u0000')
}

function normalizeSource(source?: ChannelMonitorSource): ChannelMonitorSource | null {
  if (source === 'traffic') return 'traffic'
  if (source === 'probe') return 'probe'
  return null
}

function mergeSources(sources: Set<ChannelMonitorSource>): ChannelSourceGroup {
  if (sources.size === 0) return null
  if (sources.size === 1) return [...sources][0]
  return 'mixed'
}

function statusRank(status: MonitorStatus | ''): number {
  switch (status) {
    case STATUS_OPERATIONAL:
      return 4
    case STATUS_DEGRADED:
      return 3
    case STATUS_FAILED:
      return 2
    case STATUS_ERROR:
      return 1
    default:
      return 0
  }
}

function latencyRank(latency: number | null): number {
  return latency == null ? Number.POSITIVE_INFINITY : latency
}

function shouldReplaceModel(
  existing: GroupedChannelModel | undefined,
  candidate: GroupedChannelModel,
): boolean {
  if (!existing) return true
  if (candidate.isPrimary !== existing.isPrimary) return candidate.isPrimary

  const statusDelta = statusRank(candidate.status) - statusRank(existing.status)
  if (statusDelta !== 0) return statusDelta > 0
  return latencyRank(candidate.latency_ms) < latencyRank(existing.latency_ms)
}

function extraModelRow(item: UserMonitorView, extra: UserMonitorExtraModel): GroupedChannelModel {
  return {
    monitorId: item.id,
    model: extra.model,
    status: extra.status,
    latency_ms: extra.latency_ms,
    source: normalizeSource(extra.source),
    availability_7d: extra.availability_7d ?? null,
    timeline: [],
    isPrimary: false,
  }
}

function primaryModelRow(item: UserMonitorView): GroupedChannelModel {
  return {
    monitorId: item.id,
    model: item.primary_model,
    status: item.primary_status,
    latency_ms: item.primary_latency_ms,
    source: normalizeSource(item.primary_source),
    availability_7d: item.availability_7d ?? null,
    timeline: item.timeline ?? [],
    isPrimary: true,
  }
}

/**
 * The scheduled monitor runner keeps one best line per
 * `(group_name, provider, api_mode, primary_model)`. This UI grouping restores
 * the operator-facing view: different models from the same named channel sit
 * in one status panel without changing the probing or routing behavior.
 */
export function groupChannelMonitorViews(items: UserMonitorView[]): GroupedChannelStatus[] {
  const grouped = new Map<string, MutableGroupedChannelStatus>()

  for (const item of items) {
    const key = channelKey(item)
    let group = grouped.get(key)
    if (!group) {
      const displayName = item.group_name.trim() || item.name.trim() || `Monitor ${item.id}`
      group = {
        key,
        name: displayName,
        groupName: item.group_name.trim(),
        provider: item.provider,
        apiMode: item.api_mode,
        source: null,
        monitorIds: [],
        imageMonitorId: null,
        modelsByName: new Map(),
      }
      grouped.set(key, group)
    }

    group.monitorIds.push(item.id)
    if (item.api_mode === 'images' && group.imageMonitorId == null) {
      group.imageMonitorId = item.id
    }

    const rows = [
      primaryModelRow(item),
      ...(item.extra_models ?? []).map(extra => extraModelRow(item, extra)),
    ]
    for (const row of rows) {
      const existing = group.modelsByName.get(row.model)
      if (shouldReplaceModel(existing, row)) {
        group.modelsByName.set(row.model, row)
      }
    }
  }

  return [...grouped.values()]
    .map(group => {
      const models = [...group.modelsByName.values()].sort((a, b) => {
        if (a.isPrimary !== b.isPrimary) return a.isPrimary ? -1 : 1
        return a.model.localeCompare(b.model)
      })
      const sources = new Set(
        models
          .map(model => model.source)
          .filter((source): source is ChannelMonitorSource => source != null),
      )
      return {
        key: group.key,
        name: group.name,
        groupName: group.groupName,
        provider: group.provider,
        apiMode: group.apiMode,
        source: mergeSources(sources),
        monitorIds: group.monitorIds,
        imageMonitorId: group.imageMonitorId,
        models,
        leadModel: models[0],
      }
    })
    .sort((a, b) => a.name.localeCompare(b.name))
}

export function groupedChannelStatus(
  group: Pick<GroupedChannelStatus, 'models'>,
): MonitorStatus | '' {
  if (group.models.length === 0) return ''
  const hasHealthyModel = group.models.some(
    model => model.status === STATUS_OPERATIONAL || model.status === STATUS_DEGRADED,
  )
  if (!hasHealthyModel) {
    if (group.models.some(model => model.status === STATUS_FAILED)) {
      return STATUS_FAILED
    }
    if (group.models.some(model => model.status === STATUS_ERROR)) {
      return STATUS_ERROR
    }
  }
  if (group.models.some(model => model.status !== STATUS_OPERATIONAL)) {
    return STATUS_DEGRADED
  }
  return STATUS_OPERATIONAL
}

export function resolveChannelSourceGroup(
  sources: Array<ChannelMonitorSource | null | undefined>,
): ChannelSourceGroup {
  const normalized = new Set<ChannelMonitorSource>()
  for (const source of sources) {
    if (!source) continue
    const value = normalizeSource(source)
    if (value) normalized.add(value)
  }
  return mergeSources(normalized)
}

/**
 * Keep user-facing channel health more specific than the persisted monitor
 * enum. A raw `degraded` probe has succeeded but took too long, whereas a
 * mixture of usable and failed models should be described as a partial issue.
 */
export function groupedChannelHealth(
  group: Pick<GroupedChannelStatus, 'models'>,
): GroupedChannelHealth {
  const hasAvailableModel = group.models.some(
    model => model.status === STATUS_OPERATIONAL || model.status === STATUS_DEGRADED,
  )
  if (!hasAvailableModel) return 'unavailable'

  const hasUnavailableModel = group.models.some(
    model => model.status !== STATUS_OPERATIONAL && model.status !== STATUS_DEGRADED,
  )
  if (hasUnavailableModel) return 'partial'

  if (group.models.some(model => model.status === STATUS_DEGRADED)) {
    return 'slow_response'
  }
  return 'operational'
}
