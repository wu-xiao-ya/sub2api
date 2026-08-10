<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OpsUpstreamAttributionResponse, OpsUpstreamAttributionItem } from '@/api/admin/ops'
import { formatDateTime, formatNumber } from '@/utils/format'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import EmptyState from '@/components/common/EmptyState.vue'

interface Props {
  data: OpsUpstreamAttributionResponse | null
  loading: boolean
}

const props = defineProps<Props>()
const emit = defineEmits<{
  (e: 'selectGroup', groupId: number): void
}>()

const { t } = useI18n()

const items = computed(() => props.data?.items ?? [])
const total = computed(() => props.data?.total ?? 0)
const hasData = computed(() => items.value.length > 0)

const totals = computed(() => {
  return items.value.reduce(
    (acc, row) => {
      acc.total += row.total || 0
      acc.overload += row.overload || 0
      acc.rateLimit += row.rate_limit || 0
      acc.serverError += row.server_error || 0
      acc.transport += row.transport || 0
      acc.streamFailure += row.stream_failure || 0
      return acc
    },
    { total: 0, overload: 0, rateLimit: 0, serverError: 0, transport: 0, streamFailure: 0 }
  )
})

type CardState = 'ready' | 'loading' | 'empty'
const state = computed<CardState>(() => {
  if (hasData.value) return 'ready'
  if (props.loading) return 'loading'
  return 'empty'
})

function statusClass(code: number): string {
  if (code >= 500) return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
  if (code === 429) return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-300'
  if (code >= 400) return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
  return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-200'
}

function formatLatency(value: number | null | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return '-'
  return `${formatNumber(Math.round(value))} ms`
}

function formatRowSubtitle(row: OpsUpstreamAttributionItem): string {
  const parts: string[] = []
  if (row.last_endpoint) parts.push(row.last_endpoint)
  if (row.last_status_code) parts.push(String(row.last_status_code))
  return parts.join(' · ')
}
</script>

<template>
  <div class="flex h-full flex-col rounded-3xl bg-white p-6 shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-2">
        <h3 class="flex items-center gap-2 text-sm font-bold text-gray-900 dark:text-white">
          <svg class="h-4 w-4 text-violet-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 19h16M4 15h8M4 11h12M4 7h16" />
          </svg>
          {{ t('admin.ops.upstreamAttribution') }}
          <HelpTooltip :content="t('admin.ops.tooltips.upstreamAttribution')" />
        </h3>
        <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('common.total') }}: {{ formatNumber(total) }}</span>
      </div>
      <div class="flex flex-wrap items-center gap-2 text-[11px] font-semibold">
        <span class="rounded-full bg-gray-100 px-2.5 py-1 text-gray-700 dark:bg-dark-700 dark:text-gray-200">
          {{ t('admin.ops.overload') }} {{ formatNumber(totals.overload) }}
        </span>
        <span class="rounded-full bg-gray-100 px-2.5 py-1 text-gray-700 dark:bg-dark-700 dark:text-gray-200">
          {{ t('admin.ops.rateLimit') }} {{ formatNumber(totals.rateLimit) }}
        </span>
        <span class="rounded-full bg-gray-100 px-2.5 py-1 text-gray-700 dark:bg-dark-700 dark:text-gray-200">
          {{ t('admin.ops.serverError') }} {{ formatNumber(totals.serverError) }}
        </span>
        <span class="rounded-full bg-gray-100 px-2.5 py-1 text-gray-700 dark:bg-dark-700 dark:text-gray-200">
          {{ t('admin.ops.transport') }} {{ formatNumber(totals.transport) }}
        </span>
        <span class="rounded-full bg-gray-100 px-2.5 py-1 text-gray-700 dark:bg-dark-700 dark:text-gray-200">
          {{ t('admin.ops.streamFailure') }} {{ formatNumber(totals.streamFailure) }}
        </span>
      </div>
    </div>

    <div class="mb-4 text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.ops.upstreamAttributionHint') }}
    </div>

    <div class="relative min-h-0 flex-1">
      <div v-if="state === 'ready'" class="h-full overflow-auto rounded-2xl border border-gray-100 dark:border-dark-700">
        <table class="min-w-full divide-y divide-gray-100 text-left text-sm dark:divide-dark-700">
          <thead class="sticky top-0 z-10 bg-gray-50 text-xs font-semibold uppercase tracking-wide text-gray-500 dark:bg-dark-850 dark:text-gray-400">
            <tr>
              <th class="px-4 py-3">{{ t('admin.usage.group') }}</th>
              <th class="px-4 py-3 text-right">{{ t('common.total') }}</th>
              <th class="px-4 py-3 text-right">{{ t('admin.ops.overload') }}</th>
              <th class="px-4 py-3 text-right">{{ t('admin.ops.rateLimit') }}</th>
              <th class="px-4 py-3 text-right">{{ t('admin.ops.serverError') }}</th>
              <th class="px-4 py-3 text-right">{{ t('admin.ops.transport') }}</th>
              <th class="px-4 py-3 text-right">{{ t('admin.ops.streamFailure') }}</th>
              <th class="px-4 py-3 text-right">{{ t('admin.ops.averageLatency') }}</th>
              <th class="px-4 py-3">{{ t('admin.ops.upstreamAttributionLastError') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="row in items" :key="`${row.group_id ?? 'g'}-${row.account_id ?? 'a'}-${row.last_error_at}`" class="align-top">
              <td class="px-4 py-3">
                <div class="space-y-1">
                  <button
                    v-if="row.group_id"
                    type="button"
                    class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium bg-indigo-100 text-indigo-800 hover:bg-indigo-200 dark:bg-indigo-900/30 dark:text-indigo-200 dark:hover:bg-indigo-900/50"
                    :title="`${t('admin.usage.group')} #${row.group_id}`"
                    @click="emit('selectGroup', row.group_id)"
                  >
                    {{ row.group_name || `#${row.group_id}` }}
                  </button>
                  <div class="text-xs text-gray-500 dark:text-gray-400">
                    {{ row.account_name || '-' }}
                    <span v-if="row.account_id" class="ml-1 font-mono">#{{ row.account_id }}</span>
                  </div>
                </div>
              </td>
              <td class="px-4 py-3 text-right font-medium text-gray-900 dark:text-white">{{ formatNumber(row.total) }}</td>
              <td class="px-4 py-3 text-right text-gray-700 dark:text-gray-300">{{ formatNumber(row.overload) }}</td>
              <td class="px-4 py-3 text-right text-gray-700 dark:text-gray-300">{{ formatNumber(row.rate_limit) }}</td>
              <td class="px-4 py-3 text-right text-gray-700 dark:text-gray-300">{{ formatNumber(row.server_error) }}</td>
              <td class="px-4 py-3 text-right text-gray-700 dark:text-gray-300">{{ formatNumber(row.transport) }}</td>
              <td class="px-4 py-3 text-right text-gray-700 dark:text-gray-300">{{ formatNumber(row.stream_failure) }}</td>
              <td class="px-4 py-3 text-right text-gray-700 dark:text-gray-300">{{ formatLatency(row.average_upstream_latency_ms) }}</td>
              <td class="px-4 py-3">
                <div class="space-y-1">
                  <div class="flex items-center gap-2">
                    <span class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium" :class="statusClass(row.last_status_code)">
                      {{ row.last_status_code }}
                    </span>
                    <span class="text-xs text-gray-500 dark:text-gray-400">{{ formatDateTime(row.last_error_at) }}</span>
                  </div>
                  <div class="max-w-[28rem] truncate text-xs text-gray-700 dark:text-gray-300" :title="row.last_message || '-'">
                    {{ row.last_message || '-' }}
                  </div>
                  <div class="max-w-[28rem] truncate text-[11px] text-gray-400 dark:text-gray-500" :title="formatRowSubtitle(row)">
                    {{ formatRowSubtitle(row) || '-' }}
                  </div>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="flex h-full items-center justify-center">
        <div v-if="state === 'loading'" class="animate-pulse text-sm text-gray-400">{{ t('common.loading') }}</div>
        <EmptyState v-else :title="t('common.noData')" :description="t('admin.ops.upstreamAttributionEmpty')" />
      </div>
    </div>
  </div>
</template>
