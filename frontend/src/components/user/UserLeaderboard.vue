<template>
  <section class="min-w-0" :aria-busy="loading">
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 py-3 dark:border-dark-700">
      <div role="tablist" :aria-label="t('leaderboard.title')" class="inline-flex flex-wrap gap-1 rounded-md bg-gray-100 p-1 dark:bg-dark-800">
        <button
          v-for="option in periods"
          :key="option.value"
          type="button"
          role="tab"
          :data-period="option.value"
          :aria-selected="period === option.value"
          class="rounded px-3 py-1.5 text-sm font-medium transition-colors"
          :class="period === option.value
            ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white'
            : 'text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'"
          @click="changePeriod(option.value)"
        >
          {{ t(option.label) }}
        </button>
      </div>
      <button
        type="button"
        class="btn btn-secondary btn-icon h-9 w-9"
        :disabled="loading"
        :title="t('common.refresh')"
        :aria-label="t('common.refresh')"
        @click="load"
      >
        <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
      </button>
    </div>

    <div v-if="loading" class="flex min-h-64 items-center justify-center" role="status">
      <LoadingSpinner />
    </div>
    <div v-else-if="failed" class="flex min-h-64 flex-col items-center justify-center gap-3" role="alert">
      <p class="text-sm text-red-600 dark:text-red-400">{{ t('leaderboard.errorLoading') }}</p>
      <button type="button" class="btn btn-secondary" @click="load">{{ t('common.refresh') }}</button>
    </div>
    <div v-else-if="entries.length === 0" class="flex min-h-64 items-center justify-center text-sm text-gray-500">
      {{ t('leaderboard.empty') }}
    </div>
    <div v-else class="overflow-x-auto">
      <table class="w-full min-w-[560px] table-fixed text-sm">
        <thead class="border-b border-gray-200 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
          <tr>
            <th scope="col" class="w-16 py-3 pr-3 text-left font-medium">{{ t('leaderboard.rank') }}</th>
            <th scope="col" class="w-48 px-3 py-3 text-left font-medium">{{ t('leaderboard.user') }}</th>
            <th scope="col" class="px-3 py-3 text-right font-medium">{{ t('leaderboard.tokens') }}</th>
            <th scope="col" class="w-24 px-3 py-3 text-right font-medium">{{ t('leaderboard.requests') }}</th>
            <th scope="col" class="w-28 py-3 pl-3 text-right font-medium">{{ t('leaderboard.cost') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
          <tr
            v-for="entry in entries"
            :key="entry.user_id"
            :data-current-user="entry.is_current_user"
            :class="entry.is_current_user ? 'bg-primary-50 dark:bg-primary-900/20' : ''"
          >
            <td class="py-4 pr-3 tabular-nums text-gray-500">{{ entry.rank }}</td>
            <td class="break-all px-3 py-4 font-medium text-gray-900 dark:text-gray-100">
              {{ entry.display_name }}
              <span v-if="entry.is_current_user" class="ml-1 whitespace-nowrap text-xs text-primary-600 dark:text-primary-400">
                ({{ t('leaderboard.you') }})
              </span>
            </td>
            <td class="px-3 py-4 text-right tabular-nums text-gray-700 dark:text-gray-200" :title="entry.total_tokens.toLocaleString()">
              {{ formatCompactNumber(entry.total_tokens) }}
            </td>
            <td class="px-3 py-4 text-right tabular-nums text-gray-500">{{ entry.requests.toLocaleString() }}</td>
            <td class="py-4 pl-3 text-right tabular-nums text-emerald-700 dark:text-emerald-400">
              ${{ formatCostFixed(entry.cost, 4) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getLeaderboard, type LeaderboardPeriod, type LeaderboardEntry } from '@/api/leaderboard'
import { formatCompactNumber, formatCostFixed } from '@/utils/format'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{ startDate?: string; endDate?: string }>(), {
  startDate: '',
  endDate: '',
})
const { t } = useI18n()
const periods: { value: LeaderboardPeriod; label: string }[] = [
  { value: 'today', label: 'leaderboard.period.today' },
  { value: 'week', label: 'leaderboard.period.week' },
  { value: 'month', label: 'leaderboard.period.month' },
  { value: 'all', label: 'leaderboard.period.all' },
]
const period = ref<LeaderboardPeriod>('today')
const entries = ref<LeaderboardEntry[]>([])
const loading = ref(false)
const failed = ref(false)
let generation = 0

function changePeriod(value: LeaderboardPeriod) {
  if (period.value === value) return
  period.value = value
  void load()
}

async function load() {
  const request = ++generation
  loading.value = true
  failed.value = false
  try {
    const result = await getLeaderboard({
      period: period.value,
      limit: 20,
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    })
    if (request !== generation) return
    entries.value = result.leaderboard || []
  } catch {
    if (request === generation) failed.value = true
  } finally {
    if (request === generation) loading.value = false
  }
}

watch(() => [props.startDate, props.endDate], () => {
  if (period.value === 'all') void load()
})
onBeforeUnmount(() => { generation++ })
void load()
defineExpose({ reload: load })
</script>
