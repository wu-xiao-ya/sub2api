<template>
  <div class="space-y-1 text-xs">
    <div class="font-medium text-gray-700 dark:text-gray-200">
      {{ t('admin.accounts.pluginCredits.remaining') }} {{ credits ? format(credits.remaining) : '—' }}
      <template v-if="credits?.limit != null"> / {{ format(credits.limit) }}</template>
    </div>
    <div v-if="credits?.cycle_reset_time" class="text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.pluginCredits.reset', { time: credits.cycle_reset_time }) }}
    </div>
    <button
      type="button"
      class="rounded px-1.5 py-0.5 font-medium text-blue-600 hover:bg-blue-50 disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
      :disabled="loading"
      @click="query"
    >
      {{ loading ? t('common.loading') : t('admin.accounts.pluginCredits.query') }}
    </button>
    <div v-if="failed" role="status" class="text-red-600 dark:text-red-400">
      {{ t('admin.accounts.pluginCredits.unavailable') }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getAccountCredits, type PluginAccountCredits } from '@/api/admin/plugins'

const props = defineProps<{ accountId: number }>()
const { t, locale } = useI18n()
const credits = ref<PluginAccountCredits>()
const loading = ref(false)
const failed = ref(false)

function format(value: number): string {
  return new Intl.NumberFormat(locale.value, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(value)
}

async function query() {
  loading.value = true
  failed.value = false
  try {
    credits.value = await getAccountCredits(props.accountId)
  } catch {
    credits.value = undefined
    failed.value = true
  } finally {
    loading.value = false
  }
}
</script>
