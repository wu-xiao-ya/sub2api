<template>
  <span class="text-xs text-sky-600 dark:text-sky-400" :title="t('usage.cacheRateHint')" data-testid="cache-hit-rate">
    {{ t('usage.cacheHitRate') }}: {{ denominator > 0 ? `${(100 * numerator / denominator).toFixed(1)}%` : '—' }}
  </span>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
defineProps<{ numerator: number; denominator: number }>()
const { t } = useI18n()
</script>
