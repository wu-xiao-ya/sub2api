<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="min-w-52 flex-1 sm:max-w-72">
            <input
              v-model="searchQuery"
              type="search"
              :placeholder="t('admin.groupPromotions.search')"
              class="input"
              @input="handleSearch"
            />
          </div>
          <Select
            v-model="filters.group_id"
            :options="groupFilterOptions"
            class="w-48"
            @change="reloadFromFirstPage"
          />
          <Select
            v-model="filters.enabled"
            :options="enabledFilterOptions"
            class="w-32"
            @change="reloadFromFirstPage"
          />
          <div class="flex flex-1 items-center justify-end gap-2">
            <button
              type="button"
              class="btn btn-secondary"
              :disabled="loading"
              :title="t('common.refresh')"
              @click="loadPromotions"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button type="button" class="btn btn-primary" @click="openCreateDialog">
              <Icon name="plus" size="md" class="mr-1" />
              {{ t('admin.groupPromotions.create') }}
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="promotions"
          :loading="loading"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          @sort="handleSort"
        >
          <template #cell-name="{ row }">
            <div class="min-w-0">
              <div class="truncate font-medium text-gray-900 dark:text-white">{{ row.name }}</div>
              <div v-if="row.description" class="mt-1 max-w-md truncate text-xs text-gray-500 dark:text-dark-400">
                {{ row.description }}
              </div>
            </div>
          </template>

          <template #cell-group="{ row }">
            <span class="inline-flex max-w-52 truncate rounded px-2 py-0.5 text-xs font-medium bg-indigo-100 text-indigo-800 dark:bg-indigo-900/50 dark:text-indigo-200">
              {{ groupName(row.group_id) }}
            </span>
          </template>

          <template #cell-offer="{ row }">
            <div class="text-sm text-gray-800 dark:text-gray-100">
              <span v-if="row.mode === 'discount_factor'" class="font-medium">
                {{ Math.round(row.value * 10000) / 100 }}%
              </span>
              <span v-else class="font-medium">{{ formatMultiplier(row.value) }}x</span>
              <div class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">{{ offerSummary(row) }}</div>
            </div>
          </template>

          <template #cell-schedule="{ row }">
            <div class="whitespace-nowrap text-sm text-gray-600 dark:text-gray-300">
              <div>{{ formatDateTimeToMinute(row.starts_at) }}</div>
              <div class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">{{ formatDateTimeToMinute(row.ends_at) }}</div>
            </div>
          </template>

          <template #cell-status="{ row }">
            <span class="badge" :class="statusClass(row.status)">
              {{ t(`admin.groupPromotions.status.${row.status}`) }}
            </span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <button
                type="button"
                class="rounded p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-800 dark:hover:bg-dark-700 dark:hover:text-gray-100"
                :title="t('common.edit')"
                @click="openEditDialog(row)"
              >
                <Icon name="edit" size="sm" />
              </button>
              <button
                type="button"
                class="rounded p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                :title="t('common.delete')"
                @click="openDeleteDialog(row)"
              >
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState :title="t('empty.noData')" :description="t('admin.groupPromotions.failedToLoad')" :action-text="t('admin.groupPromotions.create')" @action="openCreateDialog" />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <BaseDialog
      :show="showEditDialog"
      :title="editingPromotion ? t('admin.groupPromotions.edit') : t('admin.groupPromotions.create')"
      width="wide"
      @close="closeEditDialog"
    >
      <form id="group-promotion-form" class="space-y-5" @submit.prevent="savePromotion">
        <div class="grid grid-cols-1 gap-4 md:grid-cols-[minmax(0,1fr)_15rem]">
          <div>
            <label class="input-label">{{ t('admin.groupPromotions.activityName') }}</label>
            <input v-model.trim="form.name" class="input" maxlength="200" required />
          </div>
          <div>
            <label class="input-label">{{ t('admin.groupPromotions.group') }}</label>
            <Select v-model="form.group_id" :options="groupOptions" />
          </div>
        </div>

        <div>
          <label class="input-label">{{ t('admin.groupPromotions.descriptionLabel') }}</label>
          <textarea v-model="form.description" class="input min-h-20 resize-y" maxlength="4000"></textarea>
        </div>

        <div>
          <label class="input-label">{{ t('admin.groupPromotions.mode') }}</label>
          <div class="inline-flex overflow-hidden rounded-md border border-gray-300 dark:border-dark-600">
            <button
              type="button"
              class="px-3 py-2 text-sm font-medium transition-colors"
              :class="form.mode === 'discount_factor' ? 'bg-primary-600 text-white' : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-dark-800 dark:text-gray-200 dark:hover:bg-dark-700'"
              :aria-pressed="form.mode === 'discount_factor'"
              @click="form.mode = 'discount_factor'"
            >
              {{ t('admin.groupPromotions.discountFactor') }}
            </button>
            <button
              type="button"
              class="border-l border-gray-300 px-3 py-2 text-sm font-medium transition-colors dark:border-dark-600"
              :class="form.mode === 'fixed_multiplier' ? 'bg-primary-600 text-white' : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-dark-800 dark:text-gray-200 dark:hover:bg-dark-700'"
              :aria-pressed="form.mode === 'fixed_multiplier'"
              @click="form.mode = 'fixed_multiplier'"
            >
              {{ t('admin.groupPromotions.fixedMultiplier') }}
            </button>
          </div>
        </div>

        <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
          <div>
            <label class="input-label">
              {{ form.mode === 'discount_factor' ? t('admin.groupPromotions.percentage') : t('admin.groupPromotions.fixedRate') }}
            </label>
            <div class="relative">
              <input
                v-model.number="form.value"
                type="number"
                class="input pr-10"
                :min="0"
                :max="form.mode === 'discount_factor' ? 100 : 100"
                step="0.01"
                required
              />
              <span v-if="form.mode === 'discount_factor'" class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-sm text-gray-500">%</span>
              <span v-else class="pointer-events-none absolute inset-y-0 right-3 flex items-center text-sm text-gray-500">x</span>
            </div>
          </div>
          <div>
            <label class="input-label">{{ t('admin.groupPromotions.startsAt') }}</label>
            <input v-model="form.starts_at" type="datetime-local" class="input" required />
          </div>
          <div>
            <label class="input-label">{{ t('admin.groupPromotions.endsAt') }}</label>
            <input v-model="form.ends_at" type="datetime-local" class="input" required />
          </div>
        </div>

        <div class="flex items-start gap-3 border-t border-gray-200 pt-4 dark:border-dark-700">
          <input id="group-promotion-enabled" v-model="form.enabled" type="checkbox" class="mt-1 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800" />
          <label for="group-promotion-enabled" class="min-w-0">
            <span class="block text-sm font-medium text-gray-800 dark:text-gray-100">{{ t('admin.groupPromotions.enabled') }}</span>
            <span class="mt-1 block text-xs text-gray-500 dark:text-dark-400">
              {{ previewText }}
            </span>
          </label>
        </div>
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeEditDialog">{{ t('common.cancel') }}</button>
          <button type="submit" form="group-promotion-form" class="btn btn-primary" :disabled="saving">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('admin.groupPromotions.delete')"
      :message="t('admin.groupPromotions.deleteConfirm')"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { getPersistedPageSize, setPersistedPageSize } from '@/composables/usePersistedPageSize'
import { formatDateTimeToMinute, formatDateTimeLocalInput, parseDateTimeLocalInput } from '@/utils/format'
import { formatMultiplier } from '@/utils/formatters'
import type { AdminGroup, GroupPromotion, GroupPromotionMode } from '@/types'
import type { Column } from '@/components/common/types'

import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()

const promotions = ref<GroupPromotion[]>([])
const groups = ref<AdminGroup[]>([])
const loading = ref(false)
const saving = ref(false)
const searchQuery = ref('')
const filters = reactive({
  group_id: null as number | null,
  enabled: null as boolean | null,
})
const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(20),
  total: 0,
})
const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc',
})

let searchTimer: number | null = null
let currentController: AbortController | null = null

const groupOptions = computed(() => groups.value.map(group => ({
  value: group.id,
  label: group.name,
})))
const groupFilterOptions = computed(() => [
  { value: null, label: t('admin.groupPromotions.group') },
  ...groupOptions.value,
])
const enabledFilterOptions = computed(() => [
  { value: null, label: t('admin.groupPromotions.state') },
  { value: true, label: t('common.enabled') },
  { value: false, label: t('common.disabled') },
])
const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.groupPromotions.activityName'), sortable: true },
  { key: 'group', label: t('admin.groupPromotions.group'), sortable: false },
  { key: 'offer', label: t('admin.groupPromotions.offer'), sortable: false },
  { key: 'schedule', label: t('admin.groupPromotions.schedule'), sortable: false },
  { key: 'status', label: t('admin.groupPromotions.state'), sortable: true },
  { key: 'actions', label: t('common.actions'), sortable: false },
])

function groupName(groupID: number): string {
  return groups.value.find(group => group.id === groupID)?.name || `#${groupID}`
}

function offerSummary(promotion: GroupPromotion): string {
  if (promotion.mode === 'discount_factor') {
    return t('admin.groupPromotions.summaryDiscount', {
      value: Math.round(promotion.value * 10000) / 100,
    })
  }
  return t('admin.groupPromotions.summaryFixed', { value: formatMultiplier(promotion.value) })
}

function statusClass(status: GroupPromotion['status']): string {
  switch (status) {
    case 'active':
      return 'badge-success'
    case 'upcoming':
      return 'badge-warning'
    case 'ended':
      return 'badge-gray'
    default:
      return 'badge-danger'
  }
}

async function loadGroups() {
  try {
    groups.value = await adminAPI.groups.getAllIncludingInactive()
  } catch (error) {
    console.error('Failed to load groups for promotions:', error)
  }
}

async function loadPromotions() {
  currentController?.abort()
  const controller = new AbortController()
  currentController = controller
  loading.value = true
  try {
    const response = await adminAPI.groupPromotions.list(
      pagination.page,
      pagination.page_size,
      {
        group_id: filters.group_id ?? undefined,
        enabled: filters.enabled ?? undefined,
        search: searchQuery.value.trim() || undefined,
        sort_by: sortState.sort_by,
        sort_order: sortState.sort_order,
      },
      { signal: controller.signal },
    )
    if (currentController !== controller) return
    promotions.value = response.items || []
    pagination.total = response.total || 0
  } catch (error: any) {
    if (controller.signal.aborted || error?.name === 'AbortError' || error?.code === 'ERR_CANCELED') return
    console.error('Failed to load group promotions:', error)
    appStore.showError(error.response?.data?.detail || t('admin.groupPromotions.failedToLoad'))
  } finally {
    if (currentController === controller) {
      currentController = null
      loading.value = false
    }
  }
}

function reloadFromFirstPage() {
  pagination.page = 1
  void loadPromotions()
}

function handleSearch() {
  if (searchTimer) window.clearTimeout(searchTimer)
  searchTimer = window.setTimeout(reloadFromFirstPage, 280)
}

function handleSort(key: string, order: 'asc' | 'desc') {
  sortState.sort_by = key
  sortState.sort_order = order
  reloadFromFirstPage()
}

function handlePageChange(page: number) {
  pagination.page = page
  void loadPromotions()
}

function handlePageSizeChange(pageSize: number) {
  pagination.page_size = pageSize
  pagination.page = 1
  setPersistedPageSize(pageSize)
  void loadPromotions()
}

const showEditDialog = ref(false)
const showDeleteDialog = ref(false)
const editingPromotion = ref<GroupPromotion | null>(null)
const deletingPromotion = ref<GroupPromotion | null>(null)
const form = reactive({
  name: '',
  description: '',
  group_id: null as number | null,
  mode: 'discount_factor' as GroupPromotionMode,
  value: 95,
  starts_at: '',
  ends_at: '',
  enabled: true,
})

const previewText = computed(() => {
  if (form.mode === 'discount_factor') {
    const value = Number.isFinite(form.value) ? form.value : 0
    return t('admin.groupPromotions.summaryDiscount', { value })
  }
  return t('admin.groupPromotions.summaryFixed', { value: formatMultiplier(Number(form.value) || 0) })
})

function resetForm() {
  form.name = ''
  form.description = ''
  form.group_id = groups.value[0]?.id ?? null
  form.mode = 'discount_factor'
  form.value = 95
  form.starts_at = ''
  form.ends_at = ''
  form.enabled = true
}

function openCreateDialog() {
  editingPromotion.value = null
  resetForm()
  showEditDialog.value = true
}

function openEditDialog(promotion: GroupPromotion) {
  editingPromotion.value = promotion
  form.name = promotion.name
  form.description = promotion.description || ''
  form.group_id = promotion.group_id
  form.mode = promotion.mode
  form.value = promotion.mode === 'discount_factor' ? promotion.value * 100 : promotion.value
  form.starts_at = formatDateTimeLocalInput(Math.floor(new Date(promotion.starts_at).getTime() / 1000))
  form.ends_at = formatDateTimeLocalInput(Math.floor(new Date(promotion.ends_at).getTime() / 1000))
  form.enabled = promotion.enabled
  showEditDialog.value = true
}

function closeEditDialog() {
  showEditDialog.value = false
  editingPromotion.value = null
}

function buildPayload() {
  const startsAt = parseDateTimeLocalInput(form.starts_at)
  const endsAt = parseDateTimeLocalInput(form.ends_at)
  if (!form.group_id) {
    appStore.showError(t('admin.groupPromotions.noGroup'))
    return null
  }
  if (startsAt == null || endsAt == null || startsAt >= endsAt) {
    appStore.showError(t('admin.groupPromotions.invalidTime'))
    return null
  }
  return {
    name: form.name.trim(),
    description: form.description.trim() || null,
    group_id: form.group_id,
    mode: form.mode,
    value: form.mode === 'discount_factor' ? form.value / 100 : form.value,
    starts_at: startsAt,
    ends_at: endsAt,
    enabled: form.enabled,
  }
}

async function savePromotion() {
  const payload = buildPayload()
  if (!payload) return
  saving.value = true
  try {
    if (editingPromotion.value) {
      await adminAPI.groupPromotions.update(editingPromotion.value.id, payload)
    } else {
      await adminAPI.groupPromotions.create(payload)
    }
    appStore.showSuccess(t('admin.groupPromotions.saved'))
    closeEditDialog()
    await loadPromotions()
  } catch (error: any) {
    console.error('Failed to save group promotion:', error)
    appStore.showError(error.response?.data?.detail || t('admin.groupPromotions.failedToSave'))
  } finally {
    saving.value = false
  }
}

function openDeleteDialog(promotion: GroupPromotion) {
  deletingPromotion.value = promotion
  showDeleteDialog.value = true
}

async function confirmDelete() {
  if (!deletingPromotion.value) return
  try {
    await adminAPI.groupPromotions.delete(deletingPromotion.value.id)
    appStore.showSuccess(t('admin.groupPromotions.deleted'))
    showDeleteDialog.value = false
    deletingPromotion.value = null
    if (promotions.value.length === 1 && pagination.page > 1) pagination.page -= 1
    await loadPromotions()
  } catch (error: any) {
    console.error('Failed to delete group promotion:', error)
    appStore.showError(error.response?.data?.detail || t('admin.groupPromotions.failedToDelete'))
  }
}

onMounted(async () => {
  await loadGroups()
  await loadPromotions()
})

onUnmounted(() => {
  if (searchTimer) window.clearTimeout(searchTimer)
  currentController?.abort()
})
</script>
