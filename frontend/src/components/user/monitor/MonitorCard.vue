<template>
  <article
    class="overflow-hidden rounded-lg border border-gray-200 bg-white shadow-card transition-colors dark:border-dark-700 dark:bg-dark-800"
  >
    <section class="flex flex-col gap-4 border-b border-gray-100 px-5 py-4 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
      <div class="flex min-w-0 items-start gap-3">
        <span
          class="grid h-10 w-10 flex-none place-items-center rounded-lg border border-black/5 bg-gray-50 dark:border-white/10 dark:bg-dark-900"
          :class="providerTintClass"
        >
          <ProviderIcon :provider="item.provider" :size="20" />
        </span>
        <div class="min-w-0">
          <div class="truncate text-base font-semibold text-gray-950 dark:text-white">
            {{ item.name }}
          </div>
          <div class="mt-1 flex flex-wrap items-center gap-1.5">
            <span
              class="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium"
              :class="providerBadgeClass(item.provider)"
            >
              {{ providerLabel(item.provider) }}
            </span>
            <span
              v-if="item.groupName && item.groupName !== item.name"
              class="inline-flex items-center rounded border border-gray-200 bg-gray-50 px-1.5 py-0.5 text-[10px] font-medium text-gray-600 dark:border-dark-600 dark:bg-dark-900 dark:text-gray-300"
            >
              {{ item.groupName }}
            </span>
          </div>
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-x-5 gap-y-2 sm:flex-none">
        <div class="border-l border-gray-200 pl-4 dark:border-dark-700">
          <div class="flex items-center gap-1 text-[10px] font-medium text-gray-400">
            <Icon name="checkCircle" size="xs" />
            <span>{{ t('channelStatus.availableModels') }}</span>
          </div>
          <div class="mt-0.5 font-mono text-lg font-semibold tabular-nums text-gray-900 dark:text-gray-100">
            {{ healthyModels }}<span class="ml-1 text-xs font-medium text-gray-400">/ {{ item.models.length }}</span>
          </div>
        </div>
        <div class="border-l border-gray-200 pl-4 dark:border-dark-700">
          <div class="flex items-center gap-1 text-[10px] font-medium text-gray-400">
            <Icon name="bolt" size="xs" />
            <span>{{ t('channelStatus.leadLatency') }}</span>
          </div>
          <div class="mt-0.5 font-mono text-lg font-semibold tabular-nums text-gray-900 dark:text-gray-100">
            {{ formatLatency(item.leadModel.latency_ms) }}<span class="ml-0.5 text-[10px] font-medium text-gray-400">ms</span>
          </div>
        </div>
        <span
          class="rounded-full px-2.5 py-1 text-[11px] font-semibold"
          :class="statusBadgeClass(status)"
        >
          {{ statusLabel(status) }}
        </span>
      </div>
    </section>

    <section
      v-if="item.imageMonitorId != null"
      class="border-b border-gray-100 px-5 py-4 dark:border-dark-700"
    >
      <div class="flex items-center justify-between gap-3">
        <span class="text-xs font-semibold text-gray-700 dark:text-gray-200">
          {{ t('channelStatus.latestImage') }}
        </span>
      </div>
      <div
        class="mt-3 aspect-[16/5] overflow-hidden rounded-md border border-gray-200 bg-gray-100 dark:border-dark-700 dark:bg-dark-900"
      >
        <img
          v-if="imageUrl"
          :src="imageUrl"
          :alt="t('channelStatus.latestImage')"
          class="h-full w-full object-cover"
        />
        <div
          v-else
          class="flex h-full items-center justify-center px-4 text-center text-xs text-gray-500 dark:text-gray-400"
        >
          {{ t('channelStatus.latestImageEmpty') }}
        </div>
      </div>
    </section>

    <section
      v-else
      class="border-b border-gray-100 px-5 py-3 dark:border-dark-700"
    >
      <MonitorTimeline
        :buckets="item.leadModel.timeline"
        :countdown-seconds="countdownSeconds"
        :length="20"
      />
    </section>

    <section class="min-w-0 p-5">
        <div class="flex items-center justify-between gap-3">
          <div class="flex min-w-0 items-baseline gap-2">
            <div class="text-sm font-semibold text-gray-900 dark:text-gray-100">
              {{ t('channelStatus.modelMonitoring') }}
            </div>
            <div class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('channelStatus.modelsCount', { n: item.models.length }) }}
            </div>
          </div>
          <button
            type="button"
            class="flex h-8 w-8 flex-none items-center justify-center rounded-md text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-800 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-gray-100"
            :title="t('channelStatus.viewDetail')"
            @click="emit('click')"
          >
            <Icon name="externalLink" size="sm" />
          </button>
        </div>

        <div
          class="mt-4 hidden grid-cols-[minmax(0,1fr)_7rem_6.5rem_6rem] gap-3 border-b border-gray-100 px-3 pb-2 text-[10px] font-semibold text-gray-400 dark:border-dark-700 sm:grid"
        >
          <span>{{ t('channelStatus.detailColumns.model') }}</span>
          <span>{{ t('channelStatus.detailColumns.latestStatus') }}</span>
          <span class="text-right">{{ t('channelStatus.responseLatency') }}</span>
          <span class="text-right">{{ availabilityHeading }}</span>
        </div>

        <div class="mt-1 divide-y divide-gray-100 dark:divide-dark-700/80">
          <div
            v-for="model in item.models"
            :key="`${model.monitorId}:${model.model}`"
            class="grid gap-x-3 gap-y-2 px-3 py-3 sm:grid-cols-[minmax(0,1fr)_7rem_6.5rem_6rem] sm:items-center"
          >
            <div class="flex min-w-0 items-center gap-2">
              <span class="truncate font-mono text-sm font-medium text-gray-900 dark:text-gray-100">
                {{ model.model }}
              </span>
            </div>

            <div class="flex items-center gap-2 sm:block">
              <span class="text-[10px] font-medium text-gray-400 sm:hidden">
                {{ t('channelStatus.detailColumns.latestStatus') }}
              </span>
              <span
                class="inline-flex rounded-full px-2 py-0.5 text-[11px] font-semibold"
                :class="statusBadgeClass(model.status)"
              >
                {{ statusLabel(model.status) }}
              </span>
            </div>

            <div class="flex items-center justify-between gap-2 font-mono text-sm tabular-nums text-gray-700 dark:text-gray-300 sm:justify-end">
              <span class="text-[10px] font-medium text-gray-400 sm:hidden">
                {{ t('channelStatus.responseLatency') }}
              </span>
              <span>{{ formatLatency(model.latency_ms) }}<span class="ml-0.5 text-[10px] text-gray-400">ms</span></span>
            </div>

            <div
              class="flex items-center justify-between gap-2 font-mono text-sm font-semibold tabular-nums sm:justify-end"
              :style="availabilityStyle(model)"
            >
              <span class="text-[10px] font-medium text-gray-400 sm:hidden">
                {{ availabilityHeading }}
              </span>
              <span>{{ formatAvailability(resolveAvailability(model)) }}</span>
            </div>
          </div>
        </div>
    </section>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserMonitorDetail } from '@/api/channelMonitor'
import type { GroupedChannelModel, GroupedChannelStatus } from '@/utils/channelMonitorGrouping'
import { groupedChannelStatus } from '@/utils/channelMonitorGrouping'
import {
  hslForPct,
  useChannelMonitorFormat,
} from '@/composables/useChannelMonitorFormat'
import {
  STATUS_DEGRADED,
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
  statusBadgeClass,
  providerLabel,
  providerBadgeClass,
  formatLatency,
  formatPercent,
} = useChannelMonitorFormat()

const status = computed(() => groupedChannelStatus(props.item))
const providerTintClass = computed(() =>
  PROVIDER_TINT[props.item.provider] ?? 'text-gray-500 dark:text-gray-300'
)
const healthyModels = computed(() =>
  props.item.models.filter(model =>
    model.status === STATUS_OPERATIONAL || model.status === STATUS_DEGRADED
  ).length
)
const availabilityHeading = computed(() =>
  t('channelStatus.windowAvailability', {
    window: t(`channelStatus.windowTab.${props.window}`),
  })
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
</script>
