<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.upstreamRatioSync.tableTitle', { name: account?.name ?? '' })"
    width="wide"
    @close="handleClose"
  >
    <div class="space-y-4" data-testid="upstream-ratio-table-modal">
      <div v-if="snapshot" class="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-gray-600 dark:text-gray-300">
        <span>
          {{ t('admin.accounts.upstreamRatioSync.groupName', { group: snapshot.group ?? 'default' }) }}
        </span>
        <span>
          {{
            t('admin.accounts.upstreamRatioSync.groupRatio', {
              value: formatRatio(snapshot.group_ratio)
            })
          }}
        </span>
        <span>{{ t('admin.accounts.upstreamRatioSync.modelCount', { count: snapshot.model_count ?? 0 }) }}</span>
        <span>{{ t('admin.accounts.upstreamRatioSync.updatedAt', { value: formatDate(snapshot.received_at) }) }}</span>
      </div>

      <div class="flex items-center gap-2">
        <input
          v-model="search"
          type="text"
          class="input flex-1"
          :placeholder="t('admin.accounts.upstreamRatioSync.searchPlaceholder')"
          data-testid="upstream-ratio-search"
        />
        <button
          type="button"
          class="inline-flex h-9 flex-shrink-0 items-center gap-1.5 rounded-lg bg-primary-600 px-3 text-sm font-medium text-white transition-colors hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-50"
          :disabled="syncing"
          data-testid="upstream-ratio-sync-button"
          @click="handleSync"
        >
          <Icon name="refresh" size="xs" :class="{ 'animate-spin': syncing }" />
          {{ t('admin.accounts.upstreamRatioSync.manualSync') }}
        </button>
      </div>

      <div class="max-h-[24rem] overflow-y-auto rounded-lg border border-gray-200 dark:border-dark-700">
        <table class="w-full text-left text-sm">
          <thead class="sticky top-0 bg-gray-50 text-xs uppercase text-gray-500 dark:bg-dark-800 dark:text-gray-400">
            <tr>
              <th class="px-3 py-2">{{ t('admin.accounts.upstreamRatioSync.colModel') }}</th>
              <th class="px-3 py-2">{{ t('admin.accounts.upstreamRatioSync.colMode') }}</th>
              <th class="px-3 py-2 text-right">{{ t('admin.accounts.upstreamRatioSync.colInput') }}</th>
              <th class="px-3 py-2 text-right">{{ t('admin.accounts.upstreamRatioSync.colOutput') }}</th>
              <th class="px-3 py-2 text-right">{{ t('admin.accounts.upstreamRatioSync.colCache') }}</th>
              <th class="px-3 py-2 text-right">{{ t('admin.accounts.upstreamRatioSync.colPerCall') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="row in filteredRows" :key="row.model_name" class="text-gray-700 dark:text-gray-200">
              <td class="px-3 py-1.5 font-mono text-xs">{{ row.model_name }}</td>
              <td class="px-3 py-1.5">
                <span
                  class="rounded px-1.5 py-0.5 text-xs"
                  :class="row.quota_type === 1
                    ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400'
                    : 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'"
                >
                  {{ row.quota_type === 1 ? t('admin.accounts.upstreamRatioSync.perCall') : t('admin.accounts.upstreamRatioSync.perToken') }}
                </span>
              </td>
              <td class="px-3 py-1.5 text-right font-mono text-xs">{{ inputPrice(row) }}</td>
              <td class="px-3 py-1.5 text-right font-mono text-xs">{{ outputPrice(row) }}</td>
              <td class="px-3 py-1.5 text-right font-mono text-xs">{{ cachePrice(row) }}</td>
              <td class="px-3 py-1.5 text-right font-mono text-xs">{{ perCallPrice(row) }}</td>
            </tr>
            <tr v-if="filteredRows.length === 0">
              <td colspan="6" class="px-3 py-6 text-center text-gray-400 dark:text-gray-500">
                {{ loading ? t('common.loading') : t('admin.accounts.upstreamRatioSync.empty') }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="history.length" class="space-y-1">
        <p class="text-xs font-medium uppercase text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.upstreamRatioSync.historyTitle') }}
        </p>
        <ul class="space-y-0.5 text-xs text-gray-500 dark:text-gray-400" data-testid="upstream-ratio-history">
          <li v-for="entry in history" :key="entry.observed_at">
            {{ formatDate(entry.observed_at) }} ·
            {{ t('admin.accounts.upstreamRatioSync.modelCount', { count: entry.model_count }) }} ·
            {{ t('admin.accounts.upstreamRatioSync.groupRatio', { value: formatRatio(entry.group_ratio) }) }}
          </li>
        </ul>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  getUpstreamRatioSyncResult,
  syncUpstreamRatio
} from '@/api/admin/accounts'
import type { Account, UpstreamModelRatio, UpstreamRatioHistoryEntry, UpstreamRatioSyncSnapshot } from '@/types'

const props = defineProps<{
  show: boolean
  account: Account | null
}>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'synced'): void
}>()

const { t } = useI18n()
const search = ref('')
const loading = ref(false)
const syncing = ref(false)
const snapshot = ref<UpstreamRatioSyncSnapshot | null>(null)
const rows = ref<UpstreamModelRatio[]>([])
const history = ref<UpstreamRatioHistoryEntry[]>([])

const filteredRows = computed(() => {
  const needle = search.value.trim().toLowerCase()
  if (!needle) return rows.value
  return rows.value.filter(row => row.model_name.toLowerCase().includes(needle))
})

const load = async () => {
  if (!props.account) return
  loading.value = true
  try {
    const result = await getUpstreamRatioSyncResult(props.account.id, { limit: 500 })
    snapshot.value = result.snapshot
    rows.value = result.models ?? []
    history.value = result.history ?? []
  } catch {
    rows.value = []
    history.value = []
  } finally {
    loading.value = false
  }
}

const handleSync = async () => {
  if (!props.account) return
  syncing.value = true
  try {
    await syncUpstreamRatio(props.account.id)
    await load()
    emit('synced')
  } finally {
    syncing.value = false
  }
}

const handleClose = () => emit('close')

watch(
  () => props.show,
  show => {
    if (show) {
      search.value = ''
      void load()
    }
  }
)

// new-api 倍率换算：1 倍率 = $2 / 1M tokens（QuotaPerUnit=500000）。
const ratioToUSDPerMillion = (ratio: number, groupRatio: number) => ratio * groupRatio * 2
const formatUSD = (value: number) => `$${Number(value.toPrecision(6))}`
const inputPrice = (row: UpstreamModelRatio) =>
  row.quota_type === 1 ? '-' : formatUSD(ratioToUSDPerMillion(row.model_ratio, row.group_ratio))
const outputPrice = (row: UpstreamModelRatio) => {
  if (row.quota_type === 1) return '-'
  const completion = row.completion_ratio > 0 ? row.completion_ratio : 1
  return formatUSD(ratioToUSDPerMillion(row.model_ratio * completion, row.group_ratio))
}
const cachePrice = (row: UpstreamModelRatio) => {
  if (row.quota_type === 1 || row.cache_ratio == null) return '-'
  return formatUSD(ratioToUSDPerMillion(row.model_ratio * row.cache_ratio, row.group_ratio))
}
const perCallPrice = (row: UpstreamModelRatio) =>
  row.quota_type === 1 ? formatUSD(row.model_price * row.group_ratio) : '-'
const formatRatio = (value?: number | null) =>
  typeof value === 'number' && Number.isFinite(value) ? Number(value.toPrecision(6)) : '-'
const formatDate = (value?: string | null) => value
  ? new Date(value).toLocaleString(undefined, {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit'
    })
  : '-'
</script>
