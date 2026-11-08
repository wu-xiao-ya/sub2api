<template>
  <div v-if="visible" class="space-y-1">
    <!-- Balance value row: static display (snapshot or probe result) -->
    <div class="flex flex-wrap items-center gap-1.5">
      <span
        data-test="cn-provider-balance-value"
        :class="['text-[10px] font-medium leading-4', platformTextClass(account.platform)]"
        :title="probeTitle"
      >
        {{ balanceLabel }}
      </span>

      <!-- Low balance badge (reactive 402/429 marker or probe-detected) -->
      <span
        v-if="balanceLow"
        class="inline-flex items-center rounded bg-red-100 px-1 py-0.5 text-[10px] font-medium text-red-700 dark:bg-red-900/30 dark:text-red-300"
      >
        {{ t('admin.accounts.cnProviders.balanceLow') }}
      </span>
    </div>
    <div v-if="workBuddy && credits?.cycle_reset_time" class="text-[10px] text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.pluginCredits.reset', { time: credits.cycle_reset_time }) }}
    </div>

    <!-- Explicit refresh action (aligned with the OpenAI "Query" / Grok "Probe"
         buttons): the old chip doubled as the data display and a hidden click
         target, which users could not discover. The verb label makes the
         affordance explicit. -->
    <div class="flex flex-wrap items-center gap-1.5">
      <button
        type="button"
        data-test="cn-provider-balance-probe"
        class="inline-flex items-center gap-0.5 whitespace-nowrap rounded px-1.5 py-0.5 text-[10px] font-medium leading-4 text-blue-600 transition-colors hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
        :disabled="loading"
        :title="probeTitle"
        @click="handleProbe"
      >
        <svg
          class="h-2.5 w-2.5"
          :class="{ 'animate-spin': loading }"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
          />
        </svg>
        {{ t(workBuddy ? 'admin.accounts.pluginCredits.query' : 'admin.accounts.cnProviders.probe') }}
      </button>
    </div>

    <div v-if="error" class="truncate text-[10px] text-red-600 dark:text-red-400" :title="error">
      {{ truncatedError }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { CNProviderBalanceEntry, CNProviderBalanceResult } from '@/api/admin/cnProviders'
import { getAccountCredits, type PluginAccountCredits } from '@/api/admin/plugins'
import type { Account } from '@/types'
import { platformTextClass } from '@/utils/platformColors'
import { cnBalanceCellVisible } from './credentialsBuilder'

const props = defineProps<{
  account: Account
}>()

const { t, locale } = useI18n()

const workBuddy = computed(() => props.account.type === 'plugin' && props.account.platform === 'deepseek')
const visible = computed(() => cnBalanceCellVisible(props.account))
const probeTitle = computed(() => t(workBuddy.value
  ? 'admin.accounts.pluginCredits.query'
  : 'admin.accounts.cnProviders.balanceProbeTooltip'))

const loading = ref(false)
const error = ref<string | null>(null)
const data = ref<CNProviderBalanceResult | null>(null)
const credits = ref<PluginAccountCredits | null>(null)

const extraKey = (suffix: string) => `${props.account.platform}_${suffix}`

// 落库快照（后端周期探测/响应式写入 account.Extra）。
const snapshotBalance = computed(() => {
  const v = props.account.extra?.[extraKey('balance')]
  return typeof v === 'number' ? v : null
})
const snapshotCurrency = computed(() => {
  const v = props.account.extra?.[extraKey('balance_currency')]
  return typeof v === 'string' ? v : ''
})
// 多币种快照（后端写 <platform>_balances：[{currency, balance}]，deepseek CNY+USD）。
const snapshotBalances = computed<CNProviderBalanceEntry[]>(() => {
  const v = props.account.extra?.[extraKey('balances')]
  if (!Array.isArray(v)) return []
  return v.flatMap((item): CNProviderBalanceEntry[] => {
    if (!item || typeof item !== 'object') return []
    const { currency, balance } = item as Record<string, unknown>
    if (typeof currency !== 'string' || typeof balance !== 'number') return []
    return [{ currency, balance }]
  })
})
const balanceLow = computed(() => !workBuddy.value && props.account.extra?.[extraKey('balance_low')] === true)

// 优先用探测结果，其次落库快照。多币种返回全部明细，否则主币种单条。
const currentEntries = computed<CNProviderBalanceEntry[]>(() => {
  if (data.value && data.value.success) {
    if (data.value.balances && data.value.balances.length > 0) return data.value.balances
    return [{ currency: data.value.currency || '', balance: data.value.balance }]
  }
  if (snapshotBalances.value.length > 0) return snapshotBalances.value
  if (snapshotBalance.value != null) {
    return [{ currency: snapshotCurrency.value, balance: snapshotBalance.value }]
  }
  return []
})

const formatEntry = (entry: CNProviderBalanceEntry): string => {
  const fixed = entry.balance >= 100 ? entry.balance.toFixed(0) : entry.balance.toFixed(2)
  return `${entry.currency || '¥'} ${fixed}`
}

const balanceLabel = computed(() => {
  if (workBuddy.value) {
    const format = new Intl.NumberFormat(locale.value, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
    const value = credits.value
    const remaining = value ? format.format(value.remaining) : '—'
    const limit = value?.limit == null ? '' : ` / ${format.format(value.limit)}`
    return `${t('admin.accounts.pluginCredits.remaining')} ${remaining}${limit}`
  }
  if (currentEntries.value.length === 0) {
    // 修复:此前误引 admin.accounts.grokBalance(实际嵌套在 usageWindow 下),
    // 未命中时渲染原始 key。CN 供应商使用自己的占位键。
    return t('admin.accounts.cnProviders.balance')
  }
  return currentEntries.value.map(formatEntry).join(' · ')
})

const extractErrorMessage = (e: unknown): string => {
  const err = e as {
    message?: string
    reason?: string
    response?: { data?: { message?: string; error?: string } }
  }
  return (
    err?.message ||
    err?.reason ||
    err?.response?.data?.message ||
    err?.response?.data?.error ||
    t('common.error')
  )
}

const truncatedError = computed(() => {
  if (!error.value) return ''
  return error.value.length > 80 ? `${error.value.slice(0, 80)}...` : error.value
})

const handleProbe = async () => {
  if (loading.value) return
  const id = props.account.id
  const queryCredits = workBuddy.value
  loading.value = true
  error.value = null
  try {
    if (queryCredits) {
      const result = await getAccountCredits(id)
      if (props.account.id === id) credits.value = result
      return
    }
    const result = await adminAPI.cnProviders.queryBalance(id)
    if (props.account.id !== id) return
    // 失败时保留快照展示（仅显示错误行），成功才覆盖。
    if (result.success) {
      data.value = result
    } else {
      error.value = result.error || t('common.error')
    }
  } catch (e) {
    if (props.account.id === id) {
      if (queryCredits) credits.value = null
      error.value = queryCredits ? t('admin.accounts.pluginCredits.unavailable') : extractErrorMessage(e)
    }
  } finally {
    if (props.account.id === id) loading.value = false
  }
}

watch(
  () => props.account.id,
  () => {
    data.value = null
    credits.value = null
    error.value = null
    loading.value = false
  }
)
</script>
