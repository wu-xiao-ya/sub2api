<template>
  <section class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
    <div class="flex items-center justify-between gap-2">
      <div>
        <h3 class="text-sm font-medium">{{ t('admin.accounts.pluginCredits.modelSelection') }}</h3>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.pluginCredits.modelSelectionHint') }}</p>
      </div>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="load">
        {{ t('admin.accounts.pluginCredits.refreshModels') }}
      </button>
    </div>
    <template v-if="loaded">
      <div class="max-h-48 space-y-1 overflow-y-auto">
        <label v-for="model in models" :key="model" class="flex items-center gap-2 text-sm">
          <input v-model="selection" type="checkbox" :value="model" :disabled="busy" />
          <span class="break-all">{{ model }}</span>
        </label>
      </div>
      <button type="button" class="btn btn-primary btn-sm" :disabled="busy" @click="save">
        {{ t('admin.accounts.pluginCredits.saveModels') }}
      </button>
    </template>
    <p v-if="feedback" role="status" class="text-xs" :class="failed ? 'text-red-600 dark:text-red-400' : 'text-gray-500 dark:text-gray-400'">
      {{ feedback }}
    </p>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { apiClient } from '@/api/client'

const props = defineProps<{ accountId: number }>()
const { t } = useI18n()
const models = ref<string[]>([])
const selection = ref<string[]>([])
const busy = ref(false)
const loaded = ref(false)
const failed = ref(false)
const feedback = ref('')

interface Catalog {
  models: string[]
  enabled_models: string[]
}

async function update(method: 'post' | 'put') {
  busy.value = true
  feedback.value = ''
  failed.value = false
  try {
    const path = `/admin/accounts/${props.accountId}/workbuddy-models`
    const { data } = method === 'post'
      ? await apiClient.post<Catalog>(path)
      : await apiClient.put<Catalog>(path, { models: selection.value })
    models.value = data.models
    selection.value = data.enabled_models.filter(model => data.models.includes(model))
    loaded.value = true
    if (method === 'put') feedback.value = t('common.saved')
  } catch {
    failed.value = true
    feedback.value = t('admin.accounts.pluginCredits.modelSelectionFailed')
  } finally {
    busy.value = false
  }
}

const load = () => update('post')
const save = () => update('put')
</script>
