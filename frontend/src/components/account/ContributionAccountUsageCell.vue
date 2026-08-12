<template>
  <div class="min-w-[180px]">
    <div v-if="!canShowUsage" class="text-xs text-gray-400">-</div>

    <div v-else-if="loading && !usageInfo" class="space-y-1.5">
      <div v-for="index in 2" :key="index" class="flex items-center gap-1">
        <div class="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
        <div class="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700"></div>
        <div class="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
      </div>
    </div>

    <div v-else-if="error && !usageInfo" class="space-y-1">
      <div class="max-w-[220px] truncate text-xs text-red-500" :title="error">{{ error }}</div>
      <button type="button" class="inline-flex items-center gap-1 text-xs text-blue-600 dark:text-blue-400" @click="loadUsage(true)">
        <Icon name="refresh" size="xs" />
        <span>{{ t('common.refresh') }}</span>
      </button>
    </div>

    <div v-else-if="usageInfo" class="space-y-1">
      <div
        v-if="usageInfo.error"
        class="max-w-[220px] truncate text-xs text-amber-600 dark:text-amber-400"
        :title="usageInfo.error"
      >
        {{ usageInfo.error }}
      </div>
      <UsageProgressBar
        v-if="usageInfo.five_hour"
        label="5h"
        :utilization="usageInfo.five_hour.utilization"
        :resets-at="usageInfo.five_hour.resets_at"
        :window-stats="usageInfo.five_hour.window_stats"
        :show-now-when-idle="true"
        color="indigo"
      />
      <UsageProgressBar
        v-if="usageInfo.seven_day"
        label="7d"
        :utilization="usageInfo.seven_day.utilization"
        :resets-at="usageInfo.seven_day.resets_at"
        :window-stats="usageInfo.seven_day.window_stats"
        :show-now-when-idle="true"
        color="emerald"
      />
      <div class="mt-0.5 flex items-center gap-1.5">
        <span v-if="usageInfo.updated_at" class="text-[10px] text-gray-400">
          {{ formatRelativeTime(usageInfo.updated_at) }}
        </span>
        <button
          type="button"
          class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-blue-600 transition-colors hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
          :disabled="refreshing"
          @click="loadUsage(true)"
        >
          <Icon name="refresh" size="xs" :class="{ 'animate-spin': refreshing }" />
          <span>{{ t('common.refresh') }}</span>
        </button>
      </div>
    </div>

    <div v-else class="space-y-1">
      <div class="text-xs text-gray-400">-</div>
      <button type="button" class="inline-flex items-center gap-1 text-xs text-blue-600 dark:text-blue-400" @click="loadUsage(true)">
        <Icon name="refresh" size="xs" />
        <span>{{ t('common.refresh') }}</span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import accountContributionsAPI from '@/api/accountContributions'
import type { Account, AccountUsageInfo } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatRelativeTime } from '@/utils/format'
import Icon from '@/components/icons/Icon.vue'
import UsageProgressBar from './UsageProgressBar.vue'

const usageCache = new Map<number, { data: AccountUsageInfo; ts: number }>()
const USAGE_CACHE_TTL = 5 * 60 * 1000

const props = defineProps<{
  account: Account
  refreshToken?: number
}>()

const { t } = useI18n()

const loading = ref(false)
const refreshing = ref(false)
const error = ref<string | null>(null)
const usageInfo = ref<AccountUsageInfo | null>(null)

const canShowUsage = computed(() => {
  return props.account.type === 'oauth' && ['openai', 'anthropic', 'gemini', 'antigravity', 'grok'].includes(props.account.platform)
})

async function loadUsage(force = false): Promise<void> {
  if (!canShowUsage.value) return
  const cached = usageCache.get(props.account.id)
  if (!force && cached && Date.now() - cached.ts < USAGE_CACHE_TTL) {
    usageInfo.value = cached.data
    return
  }

  loading.value = true
  refreshing.value = force
  error.value = null
  try {
    const source = props.account.platform === 'anthropic' ? 'passive' : undefined
    const usage = await accountContributionsAPI.getUsage(props.account.id, source, force)
    usageInfo.value = usage
    usageCache.set(props.account.id, { data: usage, ts: Date.now() })
  } catch (err) {
    error.value = extractApiErrorMessage(err, t('accountContributions.errors.loadUsageFailed'))
  } finally {
    loading.value = false
    refreshing.value = false
  }
}

onMounted(() => {
  void loadUsage(false)
})

watch(
  () => props.account.id,
  () => {
    usageInfo.value = null
    void loadUsage(false)
  }
)

watch(
  () => props.refreshToken,
  (next, prev) => {
    if (next === prev) return
    usageCache.delete(props.account.id)
    void loadUsage(true)
  }
)
</script>
