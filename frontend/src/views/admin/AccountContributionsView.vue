<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('admin.accountContributions.pendingTitle') }}
            </h3>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
              {{ t('admin.accountContributions.pendingDescription') }}
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <select v-model="selectedStatus" class="input w-44" @change="handleStatusChange">
              <option v-for="option in statusOptions" :key="option.value" :value="option.value">
                {{ option.label }}
              </option>
            </select>
            <button class="btn btn-secondary" :disabled="loading" @click="loadAll">
              <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
              <span>{{ t('common.refresh') }}</span>
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="accounts" :loading="loading" row-key="id">
          <template #cell-id="{ value }">
            <span class="font-mono text-xs text-gray-500 dark:text-dark-400">#{{ value }}</span>
          </template>
          <template #cell-name="{ row }">
            <div>
              <p class="font-medium text-gray-900 dark:text-white">{{ row.name || '-' }}</p>
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ row.platform }} / {{ row.type }}</p>
            </div>
          </template>
          <template #cell-owner_user_id="{ value }">
            <span class="font-mono text-xs text-gray-500 dark:text-dark-400">#{{ value || '-' }}</span>
          </template>
          <template #cell-plan="{ row }">
            <div class="space-y-1">
              <span :class="['badge', contributionPlanBadgeClass(row)]">
                {{ contributionPlanLabel(row) }}
              </span>
              <p v-if="contributionAccountEmail(row)" class="max-w-[220px] truncate text-xs text-gray-500 dark:text-dark-400">
                {{ contributionAccountEmail(row) }}
              </p>
              <p v-if="contributionChatGPTAccountID(row)" class="max-w-[220px] truncate font-mono text-xs text-gray-400 dark:text-dark-500">
                {{ contributionChatGPTAccountID(row) }}
              </p>
            </div>
          </template>
          <template #cell-status="{ row }">
            <div class="flex flex-wrap gap-1">
              <span :class="['badge', row.status === 'active' ? 'badge-success' : 'badge-gray']">
                {{ row.status }}
              </span>
              <span :class="['badge', row.schedulable ? 'badge-success' : 'badge-gray']">
                {{ row.schedulable ? t('admin.accountContributions.schedulable') : t('admin.accountContributions.notSchedulable') }}
              </span>
              <span :class="['badge', contributionStatusClass(row.contribution_status)]">
                {{ contributionStatusLabel(row.contribution_status) }}
              </span>
            </div>
          </template>
          <template #cell-group_ids="{ row }">
            <div class="max-w-[260px] text-sm text-gray-600 dark:text-gray-300">
              {{ formatGroupNames(row.group_ids) }}
            </div>
          </template>
          <template #cell-contribution_submitted_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) || '-' }}</span>
          </template>
          <template #cell-actions="{ row }">
            <div v-if="row.contribution_status === 'pending'" class="flex items-center gap-2">
              <button class="btn btn-primary btn-sm" @click="openApproveDialog(row)">
                <Icon name="check" size="sm" />
                <span>{{ t('admin.accountContributions.approve') }}</span>
              </button>
              <button
                class="btn btn-secondary btn-sm text-red-600 hover:bg-red-50 hover:text-red-700 dark:text-red-400 dark:hover:bg-red-900/20"
                :disabled="rejectingId === row.id"
                @click="reject(row.id)"
              >
                <Icon v-if="rejectingId === row.id" name="refresh" size="sm" class="animate-spin" />
                <Icon v-else name="x" size="sm" />
                <span>{{ t('admin.accountContributions.reject') }}</span>
              </button>
            </div>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
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
      :show="showApproveDialog"
      :title="t('admin.accountContributions.approveDialog.title')"
      width="normal"
      @close="closeApproveDialog"
    >
      <form id="approve-contribution-form" class="space-y-5" @submit.prevent="approve">
        <div v-if="approvingAccount" class="rounded-xl border border-gray-200 bg-gray-50 p-3 text-sm dark:border-dark-700 dark:bg-dark-900">
          <p class="font-medium text-gray-900 dark:text-white">{{ approvingAccount.name || '-' }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
            #{{ approvingAccount.id }} · {{ approvingAccount.platform }} / {{ approvingAccount.type }}
          </p>
          <div class="mt-3 grid gap-2 text-xs text-gray-600 dark:text-dark-300 sm:grid-cols-2">
            <div>
              <span class="text-gray-400 dark:text-dark-500">{{ t('admin.accountContributions.planInfo.plan') }}</span>
              <span :class="['badge ml-2', contributionPlanBadgeClass(approvingAccount)]">
                {{ contributionPlanLabel(approvingAccount) }}
              </span>
            </div>
            <div v-if="contributionAccountEmail(approvingAccount)" class="truncate">
              <span class="text-gray-400 dark:text-dark-500">{{ t('admin.accountContributions.planInfo.email') }}</span>
              <span class="ml-2">{{ contributionAccountEmail(approvingAccount) }}</span>
            </div>
            <div v-if="contributionChatGPTAccountID(approvingAccount)" class="truncate">
              <span class="text-gray-400 dark:text-dark-500">{{ t('admin.accountContributions.planInfo.chatgptAccountId') }}</span>
              <span class="ml-2 font-mono">{{ contributionChatGPTAccountID(approvingAccount) }}</span>
            </div>
            <div v-if="contributionSubscriptionExpiresAt(approvingAccount)" class="truncate">
              <span class="text-gray-400 dark:text-dark-500">{{ t('admin.accountContributions.planInfo.expiresAt') }}</span>
              <span class="ml-2">{{ contributionSubscriptionExpiresAt(approvingAccount) }}</span>
            </div>
          </div>
        </div>

        <div>
          <label class="input-label">{{ t('admin.accountContributions.approveDialog.groups') }}</label>
          <div v-if="openAIGroups.length === 0" class="rounded-xl border border-dashed border-gray-300 p-4 text-sm text-gray-500 dark:border-dark-700 dark:text-dark-400">
            {{ t('admin.accountContributions.approveDialog.noGroups') }}
          </div>
          <div v-else class="max-h-72 space-y-2 overflow-y-auto rounded-xl border border-gray-200 p-3 dark:border-dark-700">
            <label
              v-for="group in openAIGroups"
              :key="group.id"
              class="flex cursor-pointer items-start gap-3 rounded-lg p-2 transition-colors hover:bg-gray-50 dark:hover:bg-dark-800"
            >
              <input
                type="checkbox"
                class="mt-1 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                :checked="approveForm.group_ids.includes(group.id)"
                @change="toggleGroup(group.id)"
              />
              <span class="min-w-0 flex-1">
                <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ group.name }}</span>
                <span class="block text-xs text-gray-500 dark:text-dark-400">
                  #{{ group.id }} · x{{ formatMultiplier(group.contributor_reward_multiplier || 0) }}
                </span>
              </span>
            </label>
          </div>
        </div>

        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.accountContributions.approveDialog.concurrency') }}</label>
            <input v-model.number="approveForm.concurrency" type="number" min="1" class="input" />
          </div>
          <div>
            <label class="input-label">{{ t('admin.accountContributions.approveDialog.priority') }}</label>
            <input v-model.number="approveForm.priority" type="number" min="0" class="input" />
          </div>
        </div>
      </form>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeApproveDialog">
            {{ t('common.cancel') }}
          </button>
          <button
            type="submit"
            form="approve-contribution-form"
            class="btn btn-primary"
            :disabled="approving || approveForm.group_ids.length === 0"
          >
            {{ approving ? t('common.saving') : t('admin.accountContributions.approve') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { adminAPI } from '@/api/admin'
import type { ContributionListStatus } from '@/api/admin/accountContributions'
import type { Account, AdminGroup } from '@/types'
import { useAppStore } from '@/stores/app'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

const { t } = useI18n()
const appStore = useAppStore()

const accounts = ref<Account[]>([])
const groups = ref<AdminGroup[]>([])
const loading = ref(false)
const groupsLoading = ref(false)
const approving = ref(false)
const rejectingId = ref<number | null>(null)
const showApproveDialog = ref(false)
const approvingAccount = ref<Account | null>(null)
const selectedStatus = ref<ContributionListStatus>('pending')

const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const approveForm = reactive({
  group_ids: [] as number[],
  concurrency: 1,
  priority: 100
})

const openAIGroups = computed(() => groups.value.filter(group => group.platform === 'openai'))
const statusOptions = computed(() => [
  { value: 'pending' as const, label: t('accountContributions.status.pending') },
  { value: 'approved' as const, label: t('accountContributions.status.approved') },
  { value: 'rejected' as const, label: t('accountContributions.status.rejected') },
  { value: 'revoked' as const, label: t('accountContributions.status.revoked') },
  { value: 'all' as const, label: t('admin.accountContributions.statusAll') }
])

const columns = computed<Column[]>(() => [
  { key: 'id', label: t('admin.accountContributions.columns.id') },
  { key: 'name', label: t('admin.accountContributions.columns.account') },
  { key: 'owner_user_id', label: t('admin.accountContributions.columns.owner') },
  { key: 'plan', label: t('admin.accountContributions.columns.plan') },
  { key: 'status', label: t('admin.accountContributions.columns.status') },
  { key: 'group_ids', label: t('admin.accountContributions.columns.groups') },
  { key: 'contribution_submitted_at', label: t('admin.accountContributions.columns.submittedAt') },
  { key: 'actions', label: t('common.actions') }
])

function contributionStatusLabel(status: Account['contribution_status']): string {
  if (status === 'pending') return t('accountContributions.status.pending')
  if (status === 'approved') return t('accountContributions.status.approved')
  if (status === 'rejected') return t('accountContributions.status.rejected')
  if (status === 'revoked') return t('accountContributions.status.revoked')
  return '-'
}

function contributionStatusClass(status: Account['contribution_status']): string {
  if (status === 'approved') return 'badge-success'
  if (status === 'pending') return 'badge-warning'
  if (status === 'rejected') return 'badge-danger'
  if (status === 'revoked') return 'badge-gray'
  return 'badge-gray'
}

function formatMultiplier(value: number): string {
  return Number(value || 0).toFixed(2).replace(/\.?0+$/, '')
}

function formatGroupNames(ids?: number[]): string {
  if (!ids || ids.length === 0) return '-'
  return ids.map(id => groups.value.find(group => group.id === id)?.name || `#${id}`).join(', ')
}

function contributionMetaValue(account: Account | null | undefined, keys: string[]): string {
  if (!account) return ''
  for (const source of [account.credentials, account.extra]) {
    if (!source) continue
    for (const key of keys) {
      const value = source[key]
      if (typeof value === 'string' && value.trim() !== '') return value.trim()
      if (typeof value === 'number' && Number.isFinite(value)) return String(value)
    }
  }
  return ''
}

function contributionPlanType(account: Account | null | undefined): string {
  return contributionMetaValue(account, [
    'plan_type',
    'chatgpt_plan_type',
    'account_plan',
    'subscription_plan',
    'plan'
  ]).toLowerCase()
}

function contributionPlanLabel(account: Account | null | undefined): string {
  const plan = contributionPlanType(account)
  if (!plan) return t('admin.accountContributions.planInfo.unknown')
  if (plan.includes('pro')) return 'PRO'
  if (plan.includes('team') || plan.includes('business')) return 'TEAM'
  if (plan.includes('plus')) return 'PLUS'
  if (plan.includes('free')) return 'FREE'
  if (plan.includes('enterprise')) return 'ENTERPRISE'
  return plan.replace(/_/g, ' ').toUpperCase()
}

function contributionPlanBadgeClass(account: Account | null | undefined): string {
  const plan = contributionPlanType(account)
  if (plan.includes('pro')) return 'badge-primary'
  if (plan.includes('team') || plan.includes('business') || plan.includes('enterprise')) return 'badge-success'
  if (plan.includes('plus')) return 'badge-warning'
  if (plan.includes('free')) return 'badge-gray'
  return 'badge-gray'
}

function contributionAccountEmail(account: Account | null | undefined): string {
  return contributionMetaValue(account, ['email', 'account_email'])
}

function contributionChatGPTAccountID(account: Account | null | undefined): string {
  return contributionMetaValue(account, ['chatgpt_account_id', 'organization_id'])
}

function contributionSubscriptionExpiresAt(account: Account | null | undefined): string {
  return contributionMetaValue(account, ['subscription_expires_at'])
}

function toggleGroup(id: number): void {
  const index = approveForm.group_ids.indexOf(id)
  if (index >= 0) {
    approveForm.group_ids.splice(index, 1)
  } else {
    approveForm.group_ids.push(id)
  }
}

async function loadPending(): Promise<void> {
  loading.value = true
  try {
    const response = await adminAPI.accountContributions.list(
      pagination.page,
      pagination.page_size,
      selectedStatus.value
    )
    accounts.value = response.items
    pagination.total = response.total
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountContributions.errors.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function loadGroups(): Promise<void> {
  groupsLoading.value = true
  try {
    groups.value = await adminAPI.groups.getAll('openai')
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountContributions.errors.loadGroupsFailed')))
  } finally {
    groupsLoading.value = false
  }
}

async function loadAll(): Promise<void> {
  await Promise.all([loadPending(), loadGroups()])
}

function openApproveDialog(account: Account): void {
  approvingAccount.value = account
  approveForm.group_ids = [...(account.group_ids || [])]
  approveForm.concurrency = account.concurrency || 1
  approveForm.priority = account.priority || 100
  showApproveDialog.value = true
  if (!groupsLoading.value && groups.value.length === 0) {
    void loadGroups()
  }
}

function closeApproveDialog(): void {
  showApproveDialog.value = false
  approvingAccount.value = null
}

async function approve(): Promise<void> {
  if (!approvingAccount.value || approveForm.group_ids.length === 0) return
  approving.value = true
  try {
    await adminAPI.accountContributions.approve(approvingAccount.value.id, {
      group_ids: approveForm.group_ids,
      concurrency: approveForm.concurrency,
      priority: approveForm.priority
    })
    appStore.showSuccess(t('admin.accountContributions.approved'))
    closeApproveDialog()
    await loadPending()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountContributions.errors.approveFailed')))
  } finally {
    approving.value = false
  }
}

async function reject(id: number): Promise<void> {
  if (rejectingId.value !== null) return
  if (!window.confirm(t('admin.accountContributions.rejectConfirm'))) return
  rejectingId.value = id
  try {
    await adminAPI.accountContributions.reject(id)
    appStore.showSuccess(t('admin.accountContributions.rejected'))
    await loadPending()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountContributions.errors.rejectFailed')))
  } finally {
    rejectingId.value = null
  }
}

function handleStatusChange(): void {
  pagination.page = 1
  void loadPending()
}

function handlePageChange(page: number): void {
  pagination.page = page
  void loadPending()
}

function handlePageSizeChange(pageSize: number): void {
  pagination.page_size = pageSize
  pagination.page = 1
  void loadPending()
}

onMounted(() => {
  void loadAll()
})
</script>
