<template>
  <BaseDialog
    :show="show"
    :title="title"
    width="wide"
    @close="$emit('close')"
  >
    <div v-if="sourceLabel" class="mb-3 flex flex-wrap items-center gap-2 text-xs">
      <span class="text-gray-500 dark:text-gray-400">{{ t('channelStatus.sourceLabel') }}</span>
      <span
        class="inline-flex items-center rounded px-2 py-0.5 font-medium"
        :class="sourceBadgeClass"
      >
        {{ sourceLabel }}
      </span>
    </div>
    <div v-if="loading" class="py-8 text-center text-sm text-gray-500">
      {{ t('common.loading') }}
    </div>
    <div v-else-if="models.length === 0" class="py-8 text-center text-sm text-gray-500">
      {{ t('channelStatus.detailLoadError') }}
    </div>
    <div v-else class="overflow-x-auto">
      <table class="w-full text-left text-sm">
        <thead class="border-b border-gray-200 dark:border-dark-700">
          <tr class="text-xs uppercase tracking-wider text-gray-500 dark:text-gray-400">
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.model') }}</th>
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.source') }}</th>
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.latestStatus') }}</th>
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.latestLatency') }}</th>
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.availability7d') }}</th>
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.availability15d') }}</th>
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.availability30d') }}</th>
            <th class="py-2 pr-3">{{ t('channelStatus.detailColumns.avgLatency7d') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="m in models"
            :key="m.model"
            class="border-b border-gray-100 dark:border-dark-800"
          >
            <td class="py-2 pr-3 font-medium text-gray-900 dark:text-gray-100">{{ m.model }}</td>
            <td class="py-2 pr-3">
              <span
                v-if="m.source"
                class="inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium"
                :class="sourceClass(m.source)"
              >
                {{ sourceText(m.source) }}
              </span>
              <span v-else class="text-gray-400">-</span>
            </td>
            <td class="py-2 pr-3">
              <span
                class="inline-flex items-center rounded-full px-2 py-0.5 text-[11px]"
                :class="statusBadgeClass(m.latest_status)"
              >
                {{ statusLabel(m.latest_status) }}
              </span>
            </td>
            <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">{{ formatLatency(m.latest_latency_ms) }}</td>
            <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">{{ formatPercent(m.availability_7d) }}</td>
            <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">{{ formatPercent(m.availability_15d) }}</td>
            <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">{{ formatPercent(m.availability_30d) }}</td>
            <td class="py-2 pr-3 text-gray-700 dark:text-gray-300">{{ formatLatency(m.avg_latency_7d_ms) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <section
      v-if="errorRows.length > 0"
      class="mt-5 border-t border-gray-100 pt-4 dark:border-dark-700"
    >
      <h3 class="text-xs font-semibold text-gray-800 dark:text-gray-100">
        {{ t('channelStatus.realTrafficErrors') }}
      </h3>
      <div class="mt-2 flex flex-wrap gap-2">
        <span
          v-for="row in errorRows"
          :key="row.category"
          class="inline-flex items-center rounded border border-amber-200 bg-amber-50 px-2 py-1 text-[11px] font-medium text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200"
        >
          {{ errorLabel(row.category) }} {{ formatPercent(row.rate) }}
        </span>
      </div>
    </section>

    <template #footer>
      <div class="flex justify-end">
        <button @click="$emit('close')" class="btn btn-secondary">
          {{ t('channelStatus.closeDetail') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  status as fetchChannelMonitorDetail,
  type UserMonitorDetail,
} from '@/api/channelMonitor'
import { getErrors, type MonitorErrorRow } from '@/api/channelMonitorV2'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { resolveChannelSourceGroup } from '@/utils/channelMonitorGrouping'

const props = defineProps<{
  show: boolean
  monitorIds: number[]
  accountGroupId: number | null
  title: string
}>()

defineEmits<{
  (e: 'close'): void
}>()

const { t } = useI18n()
const appStore = useAppStore()
const { statusLabel, statusBadgeClass, formatLatency, formatPercent } = useChannelMonitorFormat()

const sourceLabel = computed(() => {
  const source = resolveSourceGroup()
  if (!source) return ''
  if (source === 'mixed') return t('channelStatus.source.mixed')
  return source === 'traffic'
    ? t('channelStatus.source.traffic')
    : t('channelStatus.source.probe')
})

const sourceBadgeClass = computed(() => {
  const source = resolveSourceGroup()
  if (!source) {
    return 'border border-gray-200 bg-gray-50 text-gray-700 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-200'
  }
  switch (source) {
    case 'traffic':
      return 'border border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/20 dark:bg-sky-500/10 dark:text-sky-300'
    case 'probe':
      return 'border border-violet-200 bg-violet-50 text-violet-700 dark:border-violet-500/20 dark:bg-violet-500/10 dark:text-violet-300'
    default:
      return 'border border-gray-200 bg-gray-50 text-gray-700 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-200'
  }
})

const details = ref<UserMonitorDetail[]>([])
const errorRows = ref<MonitorErrorRow[]>([])
const loading = ref(false)

const models = computed(() => {
  const byModel = new Map<string, UserMonitorDetail['models'][number]>()
  for (const detail of details.value) {
    for (const model of detail.models) {
      const current = byModel.get(model.model)
      if (!current || isMoreUseful(model, current)) {
        byModel.set(model.model, model)
      }
    }
  }
  return [...byModel.values()].sort((a, b) => a.model.localeCompare(b.model))
})

function statusRank(status: string): number {
  if (status === 'operational') return 4
  if (status === 'degraded') return 3
  if (status === 'failed') return 2
  if (status === 'error') return 1
  return 0
}

function resolveSourceGroup() {
  return resolveChannelSourceGroup(models.value.map(model => model.source))
}

function sourceText(source: NonNullable<UserMonitorDetail['models'][number]['source']>): string {
  return source === 'traffic'
    ? t('channelStatus.source.traffic')
    : t('channelStatus.source.probe')
}

function sourceClass(source: NonNullable<UserMonitorDetail['models'][number]['source']>): string {
  return source === 'traffic'
    ? 'border border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-500/20 dark:bg-sky-500/10 dark:text-sky-300'
    : 'border border-violet-200 bg-violet-50 text-violet-700 dark:border-violet-500/20 dark:bg-violet-500/10 dark:text-violet-300'
}

function isMoreUseful(
  candidate: UserMonitorDetail['models'][number],
  current: UserMonitorDetail['models'][number],
): boolean {
  const statusDelta = statusRank(candidate.latest_status) - statusRank(current.latest_status)
  if (statusDelta !== 0) return statusDelta > 0
  const candidateLatency = candidate.latest_latency_ms ?? Number.POSITIVE_INFINITY
  const currentLatency = current.latest_latency_ms ?? Number.POSITIVE_INFINITY
  return candidateLatency < currentLatency
}

function errorLabel(category: string): string {
  return t(`channelMonitorV2.errorCategories.${category}`)
}

async function load(ids: number[]) {
  details.value = []
  loading.value = true
  try {
    details.value = await Promise.all(ids.map(id => fetchChannelMonitorDetail(id)))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.detailLoadError')))
  } finally {
    loading.value = false
  }
}

async function loadTrafficErrors(accountGroupId: number | null) {
  errorRows.value = []
  if (accountGroupId == null || accountGroupId <= 0) return
  try {
    const result = await getErrors(
      { range: '24h', platforms: [], groupIds: [accountGroupId], models: [] },
      false,
    )
    errorRows.value = (result.items || []).filter(row => row.count > 0 && !row.ignored)
  } catch {
    errorRows.value = []
  }
}

watch(
  () => [props.show, props.monitorIds, props.accountGroupId] as const,
  ([show, ids, accountGroupId]) => {
    if (!show) {
      details.value = []
      errorRows.value = []
      return
    }
    if (ids.length > 0) {
      void load(ids)
      void loadTrafficErrors(accountGroupId)
    }
  },
  { deep: true, immediate: true },
)
</script>
