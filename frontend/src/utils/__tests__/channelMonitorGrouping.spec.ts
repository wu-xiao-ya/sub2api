import { describe, expect, it } from 'vitest'
import type { UserMonitorView } from '@/api/channelMonitor'
import {
  groupChannelMonitorViews,
  groupedChannelStatus,
} from '@/utils/channelMonitorGrouping'

function monitor(overrides: Partial<UserMonitorView> = {}): UserMonitorView {
  return {
    id: 1,
    name: 'Pro upstream',
    provider: 'openai',
    api_mode: 'responses',
    group_name: 'Pro',
    primary_model: 'gpt-5.6-sol',
    primary_status: 'operational',
    primary_latency_ms: 120,
    primary_ping_latency_ms: 30,
    availability_7d: 99.8,
    extra_models: [],
    timeline: [],
    ...overrides,
  }
}

describe('groupChannelMonitorViews', () => {
  it('combines different primary models from one named channel', () => {
    const groups = groupChannelMonitorViews([
      monitor(),
      monitor({
        id: 2,
        primary_model: 'gpt-5.5',
        primary_status: 'degraded',
        primary_latency_ms: 180,
        availability_7d: 98.4,
      }),
    ])

    expect(groups).toHaveLength(1)
    expect(groups[0].name).toBe('Pro')
    expect(groups[0].monitorIds).toEqual([1, 2])
    expect(groups[0].models.map(row => row.model)).toEqual(['gpt-5.5', 'gpt-5.6-sol'])
    expect(groupedChannelStatus(groups[0])).toBe('degraded')
  })

  it('keeps same-named channels separate when their protocol differs', () => {
    const groups = groupChannelMonitorViews([
      monitor(),
      monitor({
        id: 2,
        api_mode: 'chat_completions',
        primary_model: 'gpt-5.5',
      }),
    ])

    expect(groups).toHaveLength(2)
  })

  it('marks a partially healthy channel as degraded rather than failed', () => {
    const groups = groupChannelMonitorViews([
      monitor(),
      monitor({
        id: 2,
        primary_model: 'gpt-5.5',
        primary_status: 'failed',
      }),
    ])

    expect(groupedChannelStatus(groups[0])).toBe('degraded')
  })

  it('shows each configured extra model in the same channel panel', () => {
    const groups = groupChannelMonitorViews([
      monitor({
        extra_models: [{
          model: 'gpt-5.5',
          status: 'operational',
          latency_ms: 95,
          availability_7d: 99.2,
        }],
      }),
    ])

    expect(groups[0].models).toMatchObject([
      { model: 'gpt-5.6-sol', isPrimary: true },
      { model: 'gpt-5.5', isPrimary: false, availability_7d: 99.2 },
    ])
  })
})
