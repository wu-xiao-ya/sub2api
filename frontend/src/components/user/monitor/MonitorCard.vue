<template>
  <article
    class="flex min-w-0 flex-col overflow-hidden rounded-md border border-gray-200 bg-white transition-colors dark:border-dark-700 dark:bg-dark-800"
    :class="cardHeightClass"
  >
    <section class="flex items-center gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700">
      <div class="flex min-w-0 flex-1 items-center gap-2.5">
        <span
          class="flex h-10 w-1.5 flex-none items-end justify-center rounded-full bg-gray-100 dark:bg-dark-900"
          :title="channelHealthLabel"
        >
          <span
            class="w-full rounded-full transition-[height] duration-300"
            :class="[statusHeightClass, statusMarkerClass]"
          ></span>
        </span>
        <span
          class="grid h-8 w-8 flex-none place-items-center rounded-md border border-black/5 bg-gray-50 dark:border-white/10 dark:bg-dark-900"
          :class="providerTintClass"
        >
          <ProviderIcon :provider="item.provider" :size="17" />
        </span>
        <div class="min-w-0">
          <div class="truncate text-sm font-semibold text-gray-950 dark:text-white">
            {{ item.name }}
          </div>
          <div class="mt-1 flex min-w-0 flex-wrap items-center gap-1">
            <span
              class="inline-flex items-center rounded px-1.5 py-0.5 text-[9px] font-medium"
              :class="providerBadgeClass(item.provider)"
            >
              {{ providerLabel(item.provider) }}
            </span>
            <span
              v-if="item.groupName && item.groupName !== item.name"
              class="inline-flex max-w-[11rem] truncate rounded border border-gray-200 bg-gray-50 px-1.5 py-0.5 text-[9px] font-medium text-gray-600 dark:border-dark-600 dark:bg-dark-900 dark:text-gray-300"
            >
              {{ item.groupName }}
            </span>
          </div>
        </div>
      </div>

      <div class="flex flex-none items-center gap-3">
        <div class="text-right">
          <div class="flex items-center justify-end gap-1 text-[9px] font-medium text-gray-400">
            <Icon name="checkCircle" size="xs" />
            <span>{{ t('channelStatus.availableModels') }}</span>
          </div>
          <div class="mt-0.5 font-mono text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">
            {{ healthyModels }}<span class="ml-1 text-[10px] font-medium text-gray-400">/ {{ item.models.length }}</span>
          </div>
        </div>
        <div class="text-right">
          <div class="flex items-center justify-end gap-1 text-[9px] font-medium text-gray-400">
            <Icon name="bolt" size="xs" />
            <span>{{ t('channelStatus.leadLatency') }}</span>
          </div>
          <div class="mt-0.5 font-mono text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">
            {{ formatLatency(item.leadModel.latency_ms) }}<span class="ml-0.5 text-[9px] font-medium text-gray-400">ms</span>
          </div>
        </div>
        <span
          class="rounded-full px-2 py-1 text-[10px] font-semibold"
          :class="channelHealthBadgeClass"
        >
          {{ channelHealthLabel }}
        </span>
      </div>
    </section>

    <section
      v-if="item.imageMonitorId != null"
      class="border-b border-gray-100 px-4 py-2.5 dark:border-dark-700"
    >
      <div class="flex items-center justify-between gap-3">
        <span class="text-[10px] font-semibold text-gray-700 dark:text-gray-200">
          {{ t('channelStatus.latestImage') }}
        </span>
      </div>
      <div
        class="mt-2 aspect-[16/4] max-h-24 overflow-hidden rounded border border-gray-200 bg-gray-100 dark:border-dark-700 dark:bg-dark-900"
      >
        <img
          v-if="imageUrl"
          :src="imageUrl"
          :alt="t('channelStatus.latestImage')"
          class="h-full w-full object-cover"
        />
        <div
          v-else
          class="flex h-full items-center justify-center px-4 text-center text-[10px] text-gray-500 dark:text-gray-400"
        >
          {{ t('channelStatus.latestImageEmpty') }}
        </div>
      </div>
    </section>

    <section
      v-else
      class="border-b border-gray-100 px-4 py-2 dark:border-dark-700"
    >
      <MonitorTimeline
        :buckets="item.leadModel.timeline"
        :countdown-seconds="countdownSeconds"
        :length="20"
      />
    </section>

    <section class="min-w-0 flex-1 px-4 py-3">
      <div class="flex items-center justify-between gap-3">
        <div class="flex min-w-0 items-baseline gap-2">
          <div class="text-xs font-semibold text-gray-900 dark:text-gray-100">
            {{ t('channelStatus.modelMonitoring') }}
          </div>
          <div class="text-[10px] text-gray-500 dark:text-gray-400">
            {{ t('channelStatus.modelsCount', { n: item.models.length }) }}
          </div>
        </div>
        <button
          type="button"
          class="flex h-7 w-7 flex-none items-center justify-center rounded-md text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-800 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-gray-100"
          :title="t('channelStatus.viewDetail')"
          @click="emit('click')"
        >
          <Icon name="externalLink" size="sm" />
        </button>
      </div>

      <div class="mt-2 grid gap-x-4 sm:grid-cols-2">
        <div
          v-for="model in item.models"
          :key="`${model.monitorId}:${model.model}`"
          class="flex min-w-0 items-center gap-2 border-b border-gray-100 py-1.5 dark:border-dark-700/80"
        >
          <span
            class="h-1.5 w-1.5 flex-none rounded-full"
            :class="statusDotClass(model.status)"
            :title="statusLabel(model.status)"
          ></span>
          <span
            class="min-w-0 flex-1 truncate font-mono text-[11px] font-medium text-gray-800 dark:text-gray-200"
            :title="model.model"
          >
            {{ model.model }}
          </span>
          <span class="flex-none font-mono text-[10px] tabular-nums text-gray-500 dark:text-gray-400">
            {{ formatLatency(model.latency_ms) }}ms
          </span>
          <span
            class="flex-none font-mono text-[10px] font-semibold tabular-nums"
            :style="availabilityStyle(model)"
          >
            {{ formatAvailability(resolveAvailability(model)) }}
          </span>
        </div>
      </div>
    </section>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserMonitorDetail } from '@/api/channelMonitor'
import type {
  GroupedChannelModel,
  GroupedChannelStatus,
} from '@/utils/channelMonitorGrouping'
import { groupedChannelHealth } from '@/utils/channelMonitorGrouping'
import {
  hslForPct,
  useChannelMonitorFormat,
} from '@/composables/useChannelMonitorFormat'
import {
  STATUS_ERROR,
  STATUS_DEGRADED,
  STATUS_FAILED,
  STATUS_OPERATIONAL,
} from '@/constants/channelMonitor'
import Icon from '@/components/icons/Icon.vue'
import ProviderIcon from './ProviderIcon.vue'
import MonitorTimeline from './MonitorTimeline.vue'

const PROVIDER_TINT: Record<string, string> = {
  openai: 'text-emerald-600 dark:text-emerald-300',
  anthropic: 'text-orange-600 dark:text-orange-300',
  gemini: 'text-sky-600 dark:text-sky-300',
  grok: 'text-zinc-700 dark:text-zinc-200',
}

const props = defineProps<{
  item: GroupedChannelStatus
  window: '7d' | '15d' | '30d'
  detailCache: Record<number, UserMonitorDetail>
  countdownSeconds: number
  imageUrl?: string
}>()

const emit = defineEmits<{
  (e: 'click'): void
}>()

const { t } = useI18n()
const {
  statusLabel,
  providerLabel,
  providerBadgeClass,
  formatLatency,
  formatPercent,
} = useChannelMonitorFormat()

const channelHealth = computed(() => groupedChannelHealth(props.item))
const channelHealthLabel = computed(() =>
  t(`channelStatus.health.${channelHealth.value}`),
)
const channelHealthBadgeClass = computed(() => {
  switch (channelHealth.value) {
    case 'operational':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
    case 'slow_response':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300'
    case 'partial':
      return 'bg-orange-100 text-orange-700 dark:bg-orange-500/15 dark:text-orange-300'
    case 'unavailable':
      return 'bg-red-100 text-red-700 dark:bg-red-500/15 dark:text-red-300'
    default:
      return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-dark-300'
  }
})
const cardHeightClass = computed(() => {
  switch (channelHealth.value) {
    case STATUS_OPERATIONAL:
      return 'min-h-[300px]'
    case 'slow_response':
      return 'min-h-[276px]'
    case 'partial':
      return 'min-h-[252px]'
    case 'unavailable':
      return 'min-h-[228px]'
    default:
      return 'min-h-[240px]'
  }
})
const statusHeightClass = computed(() => {
  switch (channelHealth.value) {
    case STATUS_OPERATIONAL:
      return 'h-8'
    case 'slow_response':
      return 'h-6'
    case 'partial':
      return 'h-4'
    case 'unavailable':
      return 'h-2'
    default:
      return 'h-1'
  }
})
const statusMarkerClass = computed(() => {
  switch (channelHealth.value) {
    case STATUS_OPERATIONAL:
      return 'bg-emerald-500'
    case 'slow_response':
      return 'bg-amber-500'
    case 'partial':
      return 'bg-orange-500'
    case 'unavailable':
      return 'bg-red-500'
    default:
      return 'bg-gray-300 dark:bg-dark-600'
  }
})
const providerTintClass = computed(() =>
  PROVIDER_TINT[props.item.provider] ?? 'text-gray-500 dark:text-gray-300'
)
const healthyModels = computed(() =>
  props.item.models.filter(model =>
    model.status === STATUS_OPERATIONAL || model.status === STATUS_DEGRADED
  ).length
)
function resolveAvailability(model: GroupedChannelModel): number | null {
  if (props.window === '7d') return model.availability_7d
  const detail = props.detailCache[model.monitorId]
  const detailModel = detail?.models.find(entry => entry.model === model.model)
  if (!detailModel) return null
  return props.window === '15d'
    ? detailModel.availability_15d ?? null
    : detailModel.availability_30d ?? null
}

function formatAvailability(value: number | null): string {
  return formatPercent(value)
}

function availabilityStyle(model: GroupedChannelModel): Record<string, string> {
  const color = hslForPct(resolveAvailability(model))
  return { color: color ?? 'rgb(107 114 128)' }
}

function statusDotClass(statusValue: GroupedChannelModel['status']): string {
  switch (statusValue) {
    case STATUS_OPERATIONAL:
      return 'bg-emerald-500'
    case STATUS_DEGRADED:
      return 'bg-amber-500'
    case STATUS_FAILED:
      return 'bg-red-500'
    case STATUS_ERROR:
      return 'bg-gray-500'
    default:
      return 'bg-gray-300 dark:bg-dark-600'
  }
}
</script>
