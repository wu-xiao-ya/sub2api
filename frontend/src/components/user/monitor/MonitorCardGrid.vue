<template>
  <div>
    <div
      v-if="loading && items.length === 0"
      class="space-y-4"
    >
      <div
        v-for="i in 4"
        :key="i"
        class="min-h-[260px] animate-pulse rounded-lg border border-gray-200/80 bg-white/70 p-5 dark:border-dark-700/70 dark:bg-dark-800/60"
      >
        <div class="grid gap-5 xl:grid-cols-[18rem_minmax(0,1fr)]">
          <div class="space-y-3 border-b border-gray-100 pb-4 dark:border-dark-700 xl:border-b-0 xl:border-r xl:pb-0 xl:pr-5">
            <div class="h-5 w-2/3 rounded bg-gray-200 dark:bg-dark-700"></div>
            <div class="grid grid-cols-2 gap-2">
              <div class="h-16 rounded bg-gray-100 dark:bg-dark-900/40"></div>
              <div class="h-16 rounded bg-gray-100 dark:bg-dark-900/40"></div>
            </div>
            <div class="h-14 rounded bg-gray-100 dark:bg-dark-900/40"></div>
          </div>
          <div class="space-y-2">
            <div v-for="j in 3" :key="j" class="h-14 rounded bg-gray-100 dark:bg-dark-900/40"></div>
          </div>
        </div>
      </div>
    </div>

    <EmptyState
      v-else-if="items.length === 0"
      :title="t('channelStatus.empty.title')"
      :description="t('channelStatus.empty.description')"
    />

    <div v-else class="space-y-4">
      <MonitorCard
        v-for="item in items"
        :key="item.key"
        :item="item"
        :window="window"
        :countdown-seconds="countdownSeconds"
        :detail-cache="detailCache"
        :image-url="item.imageMonitorId == null ? undefined : imageUrls[item.imageMonitorId]"
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

defineProps<{
  items: GroupedChannelStatus[]
  window: '7d' | '15d' | '30d'
  countdownSeconds: number
  loading: boolean
  detailCache: Record<number, UserMonitorDetail>
  imageUrls: Record<number, string>
}>()

const emit = defineEmits<{
  (e: 'cardClick', item: GroupedChannelStatus): void
}>()

const { t } = useI18n()
</script>
