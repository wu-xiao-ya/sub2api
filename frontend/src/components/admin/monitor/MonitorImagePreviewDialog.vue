<template>
  <BaseDialog
    :show="show"
    :title="t('admin.channelMonitor.latestImageTitle', { name: monitorName })"
    width="wide"
    @close="$emit('close')"
  >
    <div class="flex min-h-64 items-center justify-center rounded-lg bg-gray-50 p-4 dark:bg-dark-900">
      <div v-if="loading" class="text-sm text-gray-500 dark:text-gray-400">
        {{ t('common.loading') }}
      </div>
      <div v-else-if="!imageUrl" class="text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.channelMonitor.noLatestImage') }}
      </div>
      <img
        v-else
        :src="imageUrl"
        :alt="t('admin.channelMonitor.latestImageAlt')"
        class="max-h-[70vh] max-w-full object-contain"
      />
    </div>
    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-primary" @click="$emit('close')">
          {{ t('common.close') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'

defineProps<{
  show: boolean
  loading: boolean
  imageUrl: string | null
  monitorName: string
}>()

defineEmits<{
  (e: 'close'): void
}>()

const { t } = useI18n()
</script>
