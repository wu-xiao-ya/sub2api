<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.poolGroupManager.title')"
    width="extra-wide"
    @close="handleClose"
  >
    <div class="grid gap-5 lg:grid-cols-[minmax(0,1.1fr)_minmax(22rem,0.9fr)]">
      <div class="min-h-[18rem] overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700">
        <div class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700">
          <div>
            <p class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.accounts.poolGroupManager.listTitle') }}
            </p>
            <p class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.poolGroupManager.listHint') }}
            </p>
          </div>
          <button type="button" class="btn btn-secondary px-3 py-1.5 text-sm" @click="startCreate">
            <Icon name="plus" size="sm" />
            {{ t('common.create') }}
          </button>
        </div>

        <div v-if="groups.length === 0" class="flex h-48 items-center justify-center px-4 text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.poolGroupManager.empty') }}
        </div>
        <div v-else class="max-h-[28rem] overflow-y-auto divide-y divide-gray-100 dark:divide-dark-700">
          <button
            v-for="group in groups"
            :key="group.id"
            type="button"
            :class="[
              'flex w-full items-start justify-between gap-3 px-4 py-3 text-left transition-colors hover:bg-gray-50 dark:hover:bg-dark-700/60',
              editingId === group.id && 'bg-primary-50 dark:bg-primary-900/20'
            ]"
            @click="startEdit(group)"
          >
            <div class="min-w-0">
              <div class="flex min-w-0 items-center gap-2">
                <span class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ group.name }}</span>
                <span
                  :class="[
                    'shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium',
                    group.status === 'active'
                      ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                      : 'bg-gray-100 text-gray-500 dark:bg-dark-600 dark:text-gray-300'
                  ]"
                >
                  {{ group.status === 'active' ? t('common.active') : t('common.inactive') }}
                </span>
              </div>
              <p class="mt-1 truncate text-xs text-gray-500 dark:text-gray-400">
                {{ group.upstream_key || t('admin.accounts.poolGroupManager.noUpstreamKey') }}
              </p>
              <p v-if="group.description" class="mt-1 line-clamp-2 text-xs text-gray-500 dark:text-gray-400">
                {{ group.description }}
              </p>
            </div>
            <div class="shrink-0 text-xs text-gray-400 dark:text-dark-400">#{{ group.id }}</div>
          </button>
        </div>
      </div>

      <form class="space-y-4" @submit.prevent="save">
        <div>
          <label class="input-label">{{ t('admin.accounts.poolGroupManager.name') }}</label>
          <input
            v-model="form.name"
            type="text"
            required
            class="input"
            :placeholder="t('admin.accounts.poolGroupManager.namePlaceholder')"
          />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.poolGroupManager.upstreamKey') }}</label>
          <input
            v-model="form.upstream_key"
            type="text"
            class="input"
            :placeholder="t('admin.accounts.poolGroupManager.upstreamKeyPlaceholder')"
          />
          <p class="input-hint">{{ t('admin.accounts.poolGroupManager.upstreamKeyHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.poolGroupManager.description') }}</label>
          <textarea
            v-model="form.description"
            rows="3"
            class="input"
            :placeholder="t('admin.accounts.poolGroupManager.descriptionPlaceholder')"
          ></textarea>
        </div>
        <div class="grid grid-cols-2 gap-4">
          <div>
            <label class="input-label">{{ t('admin.accounts.poolGroupManager.sortOrder') }}</label>
            <input v-model.number="form.sort_order" type="number" class="input" />
          </div>
          <div>
            <label class="input-label">{{ t('common.status') }}</label>
            <Select v-model="form.status" :options="statusOptions" />
          </div>
        </div>

        <div class="flex flex-wrap justify-between gap-3 border-t border-gray-100 pt-4 dark:border-dark-700">
          <button
            v-if="editingId"
            type="button"
            class="btn btn-danger"
            :disabled="submitting"
            @click="deleteCurrent"
          >
            <Icon name="trash" size="sm" />
            {{ t('common.delete') }}
          </button>
          <span v-else></span>
          <div class="flex gap-3">
            <button type="button" class="btn btn-secondary" :disabled="submitting" @click="startCreate">
              {{ t('common.reset') }}
            </button>
            <button type="submit" class="btn btn-primary" :disabled="submitting">
              <Icon v-if="!submitting" name="check" size="sm" />
              {{ submitting ? t('common.saving') : (editingId ? t('common.save') : t('common.create')) }}
            </button>
          </div>
        </div>
      </form>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { AccountPoolGroup } from '@/types'

const props = defineProps<{
  show: boolean
  groups: AccountPoolGroup[]
}>()

const emit = defineEmits<{
  close: []
  changed: []
}>()

const { t } = useI18n()
const appStore = useAppStore()
const submitting = ref(false)
const editingId = ref<number | null>(null)
const form = reactive({
  name: '',
  upstream_key: '',
  description: '',
  sort_order: 0,
  status: 'active' as 'active' | 'inactive'
})

const statusOptions = computed(() => [
  { value: 'active', label: t('common.active') },
  { value: 'inactive', label: t('common.inactive') }
])

const startCreate = () => {
  editingId.value = null
  form.name = ''
  form.upstream_key = ''
  form.description = ''
  form.sort_order = 0
  form.status = 'active'
}

const startEdit = (group: AccountPoolGroup) => {
  editingId.value = group.id
  form.name = group.name
  form.upstream_key = group.upstream_key || ''
  form.description = group.description || ''
  form.sort_order = group.sort_order || 0
  form.status = group.status === 'inactive' ? 'inactive' : 'active'
}

const save = async () => {
  if (!form.name.trim()) {
    appStore.showError(t('admin.accounts.poolGroupManager.nameRequired'))
    return
  }
  submitting.value = true
  try {
    const payload = {
      name: form.name.trim(),
      upstream_key: form.upstream_key.trim(),
      description: form.description.trim(),
      sort_order: Number.isFinite(form.sort_order) ? Math.trunc(form.sort_order) : 0,
      status: form.status
    }
    if (editingId.value) {
      await adminAPI.accounts.updatePoolGroup(editingId.value, payload)
      appStore.showSuccess(t('admin.accounts.poolGroupManager.updated'))
    } else {
      await adminAPI.accounts.createPoolGroup(payload)
      appStore.showSuccess(t('admin.accounts.poolGroupManager.created'))
      startCreate()
    }
    emit('changed')
  } catch (error: any) {
    appStore.showError(error.message || t('admin.accounts.poolGroupManager.failed'))
  } finally {
    submitting.value = false
  }
}

const deleteCurrent = async () => {
  if (!editingId.value) return
  if (!confirm(t('admin.accounts.poolGroupManager.deleteConfirm', { name: form.name }))) return
  submitting.value = true
  try {
    await adminAPI.accounts.deletePoolGroup(editingId.value)
    appStore.showSuccess(t('admin.accounts.poolGroupManager.deleted'))
    startCreate()
    emit('changed')
  } catch (error: any) {
    appStore.showError(error.message || t('admin.accounts.poolGroupManager.failed'))
  } finally {
    submitting.value = false
  }
}

const handleClose = () => {
  emit('close')
}

watch(
  () => props.show,
  (show) => {
    if (show) startCreate()
  }
)
</script>
