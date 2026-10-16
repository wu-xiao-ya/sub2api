<template>
  <div v-if="eligible" class="flex h-6 min-w-[6.5rem] items-center gap-1">
    <HelpTooltip class="-ml-1" width-class="w-max max-w-[calc(100vw-2rem)]" data-testid="upstream-ratio-details">
      <template #trigger>
        <span
          class="cursor-help border-b border-dotted text-sm font-medium"
          :class="hasSnapshot && snapshot?.status === 'ok' && !stale
            ? 'border-gray-300 font-mono text-gray-800 dark:border-dark-600 dark:text-gray-200'
            : statusClass"
          data-testid="upstream-ratio-state"
        >
          {{ primaryValue }}
        </span>
      </template>
      <div class="space-y-1">
        <template v-if="snapshot?.status === 'ok'">
          <p>{{ t('admin.accounts.upstreamRatioSync.groupName', { group: snapshot.group ?? 'default' }) }}</p>
          <p>
            {{
              t('admin.accounts.upstreamRatioSync.groupRatio', {
                value: formatRatio(snapshot.group_ratio)
              })
            }}
          </p>
          <p>{{ t('admin.accounts.upstreamRatioSync.modelCount', { count: snapshot.model_count ?? 0 }) }}</p>
          <p>{{ t('admin.accounts.upstreamRatioSync.updatedAt', { value: formatDate(snapshot.received_at) }) }}</p>
          <p v-if="stale" class="text-amber-400">{{ t('admin.accounts.upstreamRatioSync.staleHint') }}</p>
        </template>
        <template v-else-if="snapshot?.status === 'unsupported'">
          <p>{{ t('admin.accounts.upstreamRatioSync.unsupportedHint') }}</p>
        </template>
        <template v-else-if="snapshot?.status === 'failed'">
          <p>{{ failureHint }}</p>
          <p v-if="snapshot.http_status">
            {{ t('admin.accounts.upstreamRatioSync.httpStatus', { status: snapshot.http_status }) }}
          </p>
        </template>
        <p v-else>{{ t('admin.accounts.upstreamRatioSync.neverSynced') }}</p>
        <p
          v-if="syncEnabled && nextSyncAt"
          class="mt-2 border-t border-white/15 pt-2"
          data-testid="upstream-ratio-next-sync"
        >
          {{ t('admin.accounts.upstreamRatioSync.nextSyncAt', { value: formatDate(nextSyncAt) }) }}
        </p>
        <p
          v-if="globalSyncEnabled === false"
          class="mt-1"
          data-testid="upstream-ratio-global-state"
        >
          {{ t('admin.accounts.upstreamRatioSync.globalSyncState') }}
          <span class="text-red-400">{{ t('admin.accounts.upstreamRatioSync.disabled') }}</span>
        </p>
      </div>
    </HelpTooltip>
    <button
      type="button"
      class="inline-flex h-6 w-6 flex-shrink-0 items-center justify-center rounded text-blue-600 transition-colors hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
      :disabled="syncing"
      :aria-label="t('admin.accounts.upstreamRatioSync.manualSync')"
      :title="t('admin.accounts.upstreamRatioSync.manualSync')"
      data-testid="upstream-ratio-sync"
      @click="$emit('sync')"
    >
      <Icon name="refresh" size="xs" :class="{ 'animate-spin': syncing }" />
    </button>
    <button
      type="button"
      class="inline-flex h-6 w-6 flex-shrink-0 items-center justify-center rounded text-gray-500 transition-colors hover:bg-gray-100 disabled:cursor-not-allowed disabled:opacity-50 dark:text-dark-400 dark:hover:bg-dark-800"
      :disabled="!hasSnapshot"
      :aria-label="t('admin.accounts.upstreamRatioSync.openTable')"
      :title="t('admin.accounts.upstreamRatioSync.openTable')"
      data-testid="upstream-ratio-table"
      @click="$emit('open')"
    >
      <Icon name="grid" size="xs" />
    </button>
  </div>
  <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Account, UpstreamRatioSyncSnapshot } from '@/types'

const props = withDefaults(defineProps<{
  account: Account
  now: number
  syncing?: boolean
  globalSyncEnabled?: boolean
}>(), {
  globalSyncEnabled: true
})

defineEmits<{
  (event: 'sync'): void
  (event: 'open'): void
}>()

const { t } = useI18n()

// 前端响应里 base_url 不脱敏，其余敏感键被移除；有自定义 base_url 的
// apikey 账号即视为可同步（new-api 形状由首次同步探测决定）。
const eligible = computed(() => {
  if (props.account.type !== 'apikey') return false
  const baseURL = props.account.credentials?.base_url
  return typeof baseURL === 'string' && baseURL.trim() !== ''
})

const snapshot = computed<UpstreamRatioSyncSnapshot | undefined>(() => props.account.extra?.upstream_ratio_sync)
const syncEnabled = computed(() => props.account.extra?.upstream_ratio_sync_enabled === true)
const hasSnapshot = computed(() => snapshot.value != null)
const nextSyncAt = computed(() => {
  const value = snapshot.value?.next_sync_at
  return typeof value === 'string' && Number.isFinite(Date.parse(value)) ? value : ''
})
const receivedAt = computed(() => {
  const value = snapshot.value?.received_at
  return typeof value === 'string' && Number.isFinite(Date.parse(value)) ? Date.parse(value) : Number.NaN
})
const stale = computed(() => {
  if (!snapshot.value || snapshot.value.status !== 'ok' || !Number.isFinite(receivedAt.value)) return false
  return props.now > receivedAt.value + 2 * 60 * 60 * 1000
})
const statusLabel = computed(() => {
  if (!snapshot.value) return t('admin.accounts.upstreamRatioSync.neverSynced')
  if (snapshot.value.status === 'unsupported') return t('admin.accounts.upstreamRatioSync.unsupported')
  if (snapshot.value.status === 'failed') return t('admin.accounts.upstreamRatioSync.failed')
  if (stale.value) return t('admin.accounts.upstreamRatioSync.stale')
  return t('admin.accounts.upstreamRatioSync.ok')
})
const failureHint = computed(() => {
  if (snapshot.value?.last_error === 'unauthorized') {
    return t('admin.accounts.upstreamRatioSync.unauthorizedHint')
  }
  return t('admin.accounts.upstreamRatioSync.failedHint')
})
const statusClass = computed(() => {
  if (!snapshot.value) return 'text-gray-400 dark:text-gray-500'
  if (snapshot.value.status === 'unsupported') return 'text-gray-500 dark:text-gray-400'
  if (snapshot.value.status === 'failed') return 'text-red-600 dark:text-red-400'
  if (stale.value) return 'text-amber-600 dark:text-amber-400'
  return 'text-emerald-600 dark:text-emerald-400'
})
const primaryValue = computed(() => {
  if (snapshot.value?.status === 'ok' && !stale.value && snapshot.value.group_ratio != null) {
    return `${formatRatio(snapshot.value.group_ratio)}×`
  }
  return statusLabel.value
})
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
