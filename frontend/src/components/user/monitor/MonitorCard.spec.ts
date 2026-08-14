import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import MonitorCard from './MonitorCard.vue'
import type { GroupedChannelStatus } from '@/utils/channelMonitorGrouping'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => ({
      'channelStatus.source.traffic': '用户请求',
      'channelStatus.source.probe': '站内探测',
      'channelStatus.source.mixed': '混合来源',
      'channelStatus.latencyMetric.traffic': '用户请求首字中位数',
      'channelStatus.latencyMetric.probe': '站内探测完整响应耗时',
      'channelStatus.latencyKind.firstToken': '首字',
      'channelStatus.latencyKind.probe': '探测',
    })[key] ?? key,
  }),
}))

const item: GroupedChannelStatus = {
  key: 'openai\u0000responses\u0000plus-gpt',
  name: 'plus-gpt',
  groupName: 'plus-gpt',
  provider: 'openai',
  apiMode: 'responses',
  source: 'mixed',
  monitorIds: [1],
  imageMonitorId: null,
  models: [
    {
      monitorId: 1,
      model: 'gpt-5.6-sol',
      status: 'operational',
      latency_ms: 1280,
      source: 'traffic',
      availability_7d: 99.9,
      timeline: [],
      isPrimary: true,
    },
    {
      monitorId: 1,
      model: 'gpt-5.5',
      status: 'operational',
      latency_ms: 520,
      source: 'probe',
      availability_7d: 99.8,
      timeline: [],
      isPrimary: false,
    },
  ],
  leadModel: undefined as never,
}
item.leadModel = item.models[0]

describe('MonitorCard model source labels', () => {
  it('marks each model with its own traffic or probe source', () => {
    const wrapper = mount(MonitorCard, {
      props: {
        item,
        window: '7d',
        detailCache: {},
        countdownSeconds: 30,
      },
      global: {
        stubs: {
          Icon: true,
          ProviderIcon: true,
          MonitorTimeline: true,
        },
      },
    })

    const trafficBadge = wrapper.find('[title="用户请求首字中位数"]')
    const probeBadge = wrapper.find('[title="站内探测完整响应耗时"]')

    expect(trafficBadge.text()).toBe('用户请求')
    expect(probeBadge.text()).toBe('站内探测')
    expect(wrapper.text()).toContain('混合来源')
    expect(wrapper.find('[title="用户请求首字中位数"]').exists()).toBe(true)
    expect(wrapper.find('[title="站内探测完整响应耗时"]').exists()).toBe(true)
  })
})
