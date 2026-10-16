<template>
  <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
    <span class="whitespace-nowrap text-[11px] font-medium text-gray-500 dark:text-gray-400">
      {{ t('monitorCommon.history60pts', { n: length }) }}
    </span>
    <div
      v-if="maintenance"
      class="flex h-2 min-w-[9rem] flex-1 rounded-full border border-dashed border-gray-300 dark:border-dark-600"
      :title="t('monitorCommon.maintenancePaused')"
    >
      <span class="sr-only">{{ t('monitorCommon.maintenancePaused') }}</span>
    </div>
    <div v-else class="flex h-2 min-w-[9rem] flex-1 items-stretch gap-[3px]">
      <div
        v-for="(bar, idx) in displayBars"
        :key="idx"
        class="min-w-[2px] flex-1 rounded-sm transition-opacity hover:opacity-80"
        :class="bar.colorClass"
        :title="bar.title"
      >
        <span class="sr-only">{{ bar.title }}</span>
      </div>
    </div>

    <div class="flex items-center gap-x-3 text-[11px] text-gray-500 dark:text-gray-400">
      <span
        v-for="summary in summaries"
        :key="summary.status"
        class="inline-flex items-center gap-1.5"
      >
        <span class="h-1.5 w-1.5 rounded-full" :class="summary.dotClass"></span>
        <span>{{ summary.label }}</span>
        <strong class="font-mono font-semibold tabular-nums text-gray-700 dark:text-gray-200">{{ summary.count }}</strong>
      </span>
    </div>
    <span class="whitespace-nowrap font-mono text-[10px] tabular-nums text-gray-400">
      {{ t('monitorCommon.nextUpdateIn', { n: countdownSeconds }) }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MonitorTimelinePoint } from '@/api/channelMonitor'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'

const props = withDefaults(defineProps<{
  buckets?: MonitorTimelinePoint[]
  countdownSeconds: number
  length?: number
  maintenance?: boolean
}>(), {
  buckets: () => [],
  length: 60,
  maintenance: false,
})

const { t } = useI18n()
const { statusLabel, formatLatency, formatRelativeTime } = useChannelMonitorFormat()

interface Bar {
  colorClass: string
  title: string
}

const STATUS_COLOR: Record<string, string> = {
  operational: 'bg-emerald-500',
  degraded: 'bg-amber-500',
  failed: 'bg-red-500',
  error: 'bg-red-500',
  empty: 'bg-gray-200 dark:bg-dark-700',
}

const displayBars = computed<Bar[]>(() => {
  const real = [...(props.buckets ?? [])]
    .slice(0, props.length)
    .reverse()

  const padCount = Math.max(0, props.length - real.length)
  const bars: Bar[] = []

  for (let i = 0; i < padCount; i += 1) {
    bars.push({
      colorClass: STATUS_COLOR.empty,
      title: '',
    })
  }

  for (const point of real) {
    const status = point.status as keyof typeof STATUS_COLOR
    const colorClass = STATUS_COLOR[status] ?? STATUS_COLOR.empty
    const latency = formatLatency(point.latency_ms)
    const relative = formatRelativeTime(point.checked_at)
    const label = statusLabel(point.status)
    bars.push({
      colorClass,
      title: `${relative} · ${label} · ${latency}ms`,
    })
  }

  return bars
})

type SummaryStatus = 'operational' | 'degraded' | 'failed' | 'error'

const SUMMARY_STYLES: Record<SummaryStatus, { dotClass: string }> = {
  operational: { dotClass: 'bg-emerald-500' },
  degraded: { dotClass: 'bg-amber-500' },
  failed: { dotClass: 'bg-red-500' },
  error: { dotClass: 'bg-red-500' },
}

const summaries = computed(() => {
  const counts: Record<SummaryStatus, number> = {
    operational: 0,
    degraded: 0,
    failed: 0,
    error: 0,
  }

  for (const point of (props.buckets ?? []).slice(0, props.length)) {
    const status = point.status as SummaryStatus
    if (status in counts) counts[status] += 1
  }

  return (Object.keys(counts) as SummaryStatus[])
    .filter(status => counts[status] > 0)
    .map(status => ({
      status,
      label: statusLabel(status),
      count: counts[status],
      dotClass: SUMMARY_STYLES[status].dotClass,
    }))
})
</script>
