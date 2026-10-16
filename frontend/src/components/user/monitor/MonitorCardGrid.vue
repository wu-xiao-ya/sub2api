<template>
  <div>
    <div
      v-if="loading && items.length === 0"
      class="grid grid-cols-1 items-start gap-3 lg:grid-cols-2"
    >
      <div
        v-for="i in 4"
        :key="i"
        class="min-h-[230px] animate-pulse rounded-md border border-gray-200/80 bg-white/70 p-4 dark:border-dark-700/70 dark:bg-dark-800/60"
      >
        <div class="flex items-center gap-3">
          <div class="h-10 w-1.5 rounded-full bg-gray-200 dark:bg-dark-700"></div>
          <div class="min-w-0 flex-1 space-y-2">
            <div class="h-4 w-2/3 rounded bg-gray-200 dark:bg-dark-700"></div>
            <div class="h-3 w-1/3 rounded bg-gray-100 dark:bg-dark-900/40"></div>
          </div>
          <div class="h-8 w-16 rounded bg-gray-100 dark:bg-dark-900/40"></div>
        </div>
        <div class="mt-4 h-2 rounded bg-gray-100 dark:bg-dark-900/40"></div>
        <div class="mt-4 grid grid-cols-2 gap-x-4 gap-y-2">
          <div v-for="j in 4" :key="j" class="h-7 rounded border-b border-gray-100 bg-gray-50 dark:border-dark-700 dark:bg-dark-900/40"></div>
        </div>
      </div>
    </div>

    <EmptyState
      v-else-if="items.length === 0"
      :title="t('channelStatus.empty.title')"
      :description="t('channelStatus.empty.description')"
    />

    <div v-else class="grid grid-cols-1 items-start gap-3 lg:grid-cols-2">
      <MonitorCard
        v-for="item in items"
        :key="item.key"
        :item="item"
        :window="window"
        :countdown-seconds="countdownSeconds"
        :detail-cache="detailCache"
        :image-url="item.imageMonitorId == null ? undefined : imageUrls[item.imageMonitorId]"
        :traffic-metrics="props.trafficMetrics[item.key] || {}"
        @click="emit('cardClick', item)"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { UserMonitorDetail } from '@/api/channelMonitor'
import type { GroupedChannelStatus } from '@/utils/channelMonitorGrouping'
import EmptyState from '@/components/common/EmptyState.vue'
import MonitorCard from './MonitorCard.vue'

const props = withDefaults(defineProps<{
  items: GroupedChannelStatus[]
  window: '7d' | '15d' | '30d'
  countdownSeconds: number
  loading: boolean
  detailCache: Record<number, UserMonitorDetail>
  imageUrls: Record<number, string>
  trafficMetrics?: Record<string, Record<string, {
    successRate: number
    ttftP50Ms: number | null
    cacheRate: number
  }>>
}>(), {
  trafficMetrics: () => ({}),
})

const emit = defineEmits<{
  (e: 'cardClick', item: GroupedChannelStatus): void
}>()

const { t } = useI18n()
</script>
