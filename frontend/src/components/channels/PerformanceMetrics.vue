<template>
  <dl class="grid min-w-0 grid-cols-3 gap-2 tabular-nums">
    <div v-for="entry in values" :key="entry.label" class="min-w-0">
      <dt class="break-words text-[11px] text-gray-500 dark:text-dark-400">{{ entry.label }}</dt>
      <dd class="mt-1 break-words text-xs font-semibold text-gray-900 dark:text-dark-100">{{ entry.value }}</dd>
    </div>
  </dl>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatPerformanceValue, type PerformanceMetric } from '@/api/channelPerformance'
const props = defineProps<{ metric?: PerformanceMetric | null }>()
const { t } = useI18n()
const values = computed(() => [
  { label: t('availableChannels.performance.firstCharacter'), value: formatPerformanceValue(props.metric?.first_character_ms, 'ms') },
  { label: t('availableChannels.performance.tps'), value: formatPerformanceValue(props.metric?.output_tps, 'tps') },
  { label: t('availableChannels.performance.success'), value: formatPerformanceValue(props.metric?.success_rate, 'percent') }
])
</script>
