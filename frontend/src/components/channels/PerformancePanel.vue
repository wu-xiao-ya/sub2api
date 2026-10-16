<template>
  <BaseDialog :show="true" :title="t('availableChannels.performance.title') + ' · ' + item.model.name" width="extra-wide" @close="emit('close')">
    <div class="flex flex-col gap-4">
      <div class="flex flex-wrap items-end gap-3">
        <label class="min-w-32 flex-1 text-xs text-gray-500 dark:text-dark-400">{{ t('availableChannels.performance.range') }}
          <select v-model="range" :aria-label="t('availableChannels.performance.range')" class="input mt-1 w-full">
            <option v-for="value in ['90m', '24h', '7d', '30d']" :key="value" :value="value">{{ t('availableChannels.performance.ranges.' + value) }}</option>
          </select>
        </label>
        <label class="min-w-32 flex-1 text-xs text-gray-500 dark:text-dark-400">{{ t('availableChannels.groupsLabel') }}
          <select v-model="group" :aria-label="t('availableChannels.groupsLabel')" class="input mt-1 w-full">
            <option value="">{{ t('availableChannels.performance.all') }}</option>
            <option v-for="g in item.groups" :key="g.id" :value="String(g.id)">{{ g.name }}</option>
          </select>
        </label>
        <label class="min-w-32 flex-1 text-xs text-gray-500 dark:text-dark-400">{{ t('availableChannels.performance.tier') }}
          <select v-model="tier" :aria-label="t('availableChannels.performance.tier')" class="input mt-1 w-full"><option value="">{{ t('availableChannels.performance.all') }}</option>
            <option v-for="value in ['unknown', 'standard', 'default', 'priority', 'flex', 'scale', 'auto']" :key="value" :value="value">{{ value === 'unknown' ? t('availableChannels.performance.unknown') : value }}</option>
          </select>
        </label>
        <label class="min-w-32 flex-1 text-xs text-gray-500 dark:text-dark-400">{{ t('availableChannels.performance.effort') }}
          <select v-model="effort" :aria-label="t('availableChannels.performance.effort')" class="input mt-1 w-full"><option value="">{{ t('availableChannels.performance.all') }}</option>
            <option v-for="value in ['unknown', 'none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'ultra']" :key="value" :value="value">{{ value === 'unknown' ? t('availableChannels.performance.unknown') : value }}</option>
          </select>
        </label>
        <label class="min-w-32 flex-1 text-xs text-gray-500 dark:text-dark-400">{{ t('availableChannels.performance.requestType') }}
          <select v-model="stream" :aria-label="t('availableChannels.performance.requestType')" class="input mt-1 w-full">
            <option value="">{{ t('availableChannels.performance.all') }}</option>
            <option value="true">{{ t('availableChannels.performance.streaming') }}</option>
            <option value="false">{{ t('availableChannels.performance.nonStreaming') }}</option>
          </select>
        </label>
        <label class="min-w-28 text-xs text-gray-500 dark:text-dark-400">{{ t('availableChannels.performance.refresh') }}
          <select v-model.number="seconds" :aria-label="t('availableChannels.performance.refresh')" class="input mt-1 w-full">
            <option :value="0">{{ t('availableChannels.performance.off') }}</option>
            <option v-for="value in [5, 15, 30, 60]" :key="value" :value="value">{{ value }} s</option>
          </select>
        </label>
        <button type="button" class="btn btn-secondary h-10 w-10 !px-0" :disabled="loading" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="refresh"><Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" /></button>
      </div>
      <div class="flex flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-dark-400" aria-live="polite">
        <span>{{ t('availableChannels.performance.source') }}</span>
        <span>{{ t('availableChannels.performance.updated') }}: {{ timestamp(data?.updated_at) }}</span>
        <span>{{ t('availableChannels.performance.coverage') }}: {{ timestamp(data?.coverage_start) }} ~ {{ timestamp(data?.coverage_end) }}</span>
      </div>
      <p v-if="error" class="text-sm text-red-600 dark:text-red-400" role="alert">{{ t('availableChannels.performance.error') }}</p>
      <p v-else-if="loading && !data" class="text-sm text-gray-500" role="status">{{ t('common.loading') }}</p>
      <p v-if="data?.incomplete_since || metric?.coverage_status === 'partial'" class="text-xs text-amber-700 dark:text-amber-400">{{ t('availableChannels.performance.partial') }}</p>
      <p v-if="metric?.sample_quality === 'empty'" class="text-sm text-gray-500 dark:text-dark-400">{{ t('availableChannels.performance.empty') }}</p>
      <p v-else-if="metric?.sample_quality === 'low'" class="text-xs text-amber-700 dark:text-amber-400">{{ t('availableChannels.performance.low') }}</p>
      <div class="border-y border-gray-200 py-4 dark:border-dark-700"><PerformanceMetrics :metric="metric" /></div>
      <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('availableChannels.performance.method') }}</p>
      <div class="max-w-full overflow-x-auto">
        <table class="w-full min-w-[650px] text-left text-xs tabular-nums">
          <thead class="border-b border-gray-200 text-gray-500 dark:border-dark-700 dark:text-dark-400"><tr>
            <th class="py-2 pr-3">{{ t('availableChannels.groupsLabel') }}</th><th class="px-2 py-2">{{ t('availableChannels.performance.firstCharacter') }}</th>
            <th class="px-2 py-2">{{ t('availableChannels.performance.duration') }}</th><th class="px-2 py-2">{{ t('availableChannels.performance.tps') }}</th>
            <th class="px-2 py-2">{{ t('availableChannels.performance.success') }}</th><th class="px-2 py-2">{{ t('availableChannels.performance.timeline') }}</th>
          </tr></thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700"><tr v-for="g in metric?.groups ?? []" :key="g.id" class="text-gray-800 dark:text-dark-200">
            <td class="max-w-48 break-words py-3 pr-3">{{ g.name }}<span v-if="g.sample_quality === 'low'" class="block text-[10px] text-amber-600">{{ t('availableChannels.performance.low') }}</span></td>
            <td class="px-2">{{ formatPerformanceValue(g.first_character_ms, 'ms') }}</td><td class="px-2">{{ formatPerformanceValue(g.duration_ms, 'ms') }}</td>
            <td class="px-2">{{ formatPerformanceValue(g.output_tps, 'tps') }}</td><td class="px-2">{{ formatPerformanceValue(g.success_rate, 'percent') }}</td>
            <td class="w-44 px-2"><div class="flex h-6 w-40 gap-px">
              <span v-for="point in g.trend" :key="point.at" tabindex="0" class="min-w-0 flex-1 rounded-sm outline-offset-2 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500" :class="point.success_rate == null ? 'bg-gray-200 dark:bg-dark-600' : point.success_rate >= 99 ? 'bg-emerald-500' : point.success_rate >= 90 ? 'bg-amber-500' : 'bg-red-500'" :title="pointLabel(point)" :aria-label="pointLabel(point)"></span>
            </div></td>
          </tr></tbody>
        </table>
      </div>
      <div class="grid min-w-0 grid-cols-1 gap-x-5 lg:grid-cols-3">
        <PerformanceTrend :title="t('availableChannels.performance.firstCharacter')" :points="metric?.trend ?? []" field="first_character_ms" color="#0891b2" />
        <PerformanceTrend :title="t('availableChannels.performance.tps')" :points="metric?.trend ?? []" field="output_tps" color="#059669" />
        <PerformanceTrend :title="t('availableChannels.performance.success')" :points="metric?.trend ?? []" field="success_rate" color="#d97706" />
      </div>
    </div>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useChannelPerformance } from '@/composables/useChannelPerformance'
import { formatPerformanceValue, type PerformanceFilter, type PerformancePoint } from '@/api/channelPerformance'
import type { ModelPlazaItem } from './modelPlaza'
import PerformanceMetrics from './PerformanceMetrics.vue'
import PerformanceTrend from './PerformanceTrend.vue'
const props = defineProps<{ item: ModelPlazaItem }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const range = ref<PerformanceFilter['range']>('24h')
const group = ref('')
const tier = ref('')
const effort = ref('')
const stream = ref('')
const seconds = ref(30)
const filters = computed<PerformanceFilter>(() => ({ key: props.item.key, range: range.value,
  group_id: group.value ? Number(group.value) : undefined, service_tier: tier.value || undefined,
  reasoning_effort: effort.value || undefined, stream: stream.value === '' ? undefined : stream.value === 'true' }))
const { data, loading, error, refresh } = useChannelPerformance(filters, ref(true), seconds)
const metric = computed(() => data.value?.items.find(item => item.key === props.item.key))
const timestamp = (value?: string | null) => value ? new Date(value).toLocaleString() : '\u2014'
const pointLabel = (point: PerformancePoint) => timestamp(point.at) + ': ' + formatPerformanceValue(point.success_rate, 'percent')
</script>
