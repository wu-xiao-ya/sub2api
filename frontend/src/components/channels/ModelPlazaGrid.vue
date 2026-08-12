<template>
  <div
    v-if="loading && items.length === 0"
    class="grid grid-cols-1 gap-4 sm:grid-cols-2 2xl:grid-cols-3 min-[1900px]:grid-cols-4"
  >
    <div
      v-for="index in 9"
      :key="index"
      class="min-h-[20rem] animate-pulse rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800"
    >
      <div class="flex gap-3">
        <div class="h-9 w-9 rounded-md bg-gray-100 dark:bg-dark-700"></div>
        <div class="flex-1 space-y-2">
          <div class="h-4 w-2/3 rounded bg-gray-200 dark:bg-dark-700"></div>
          <div class="h-3 w-1/2 rounded bg-gray-100 dark:bg-dark-900"></div>
        </div>
      </div>
      <div class="mt-4 h-9 rounded bg-gray-100 dark:bg-dark-900"></div>
      <div class="mt-4 h-24 rounded-md bg-gray-100 dark:bg-dark-900"></div>
      <div class="mt-4 h-12 rounded bg-gray-100 dark:bg-dark-900"></div>
    </div>
  </div>

  <EmptyState
    v-else-if="items.length === 0"
    :title="emptyLabel"
    :description="t('availableChannels.emptyDescription')"
  />

  <div v-else class="grid grid-cols-1 gap-4 sm:grid-cols-2 2xl:grid-cols-3 min-[1900px]:grid-cols-4">
    <ModelPlazaCard
      v-for="item in items"
      :key="item.key"
      :item="item"
      :user-group-rates="userGroupRates"
      :token-scale="tokenScale"
      @toggle-group="emit('toggleGroup', $event)"
    />
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import EmptyState from '@/components/common/EmptyState.vue'
import ModelPlazaCard from './ModelPlazaCard.vue'
import type { ModelPlazaItem } from './modelPlaza'

defineProps<{
  items: ModelPlazaItem[]
  loading: boolean
  emptyLabel: string
  userGroupRates: Record<number, number>
  tokenScale: number
}>()

const emit = defineEmits<{
  toggleGroup: [groupID: number]
}>()

const { t } = useI18n()
</script>
