<template>
  <AppLayout>
    <div class="space-y-4">
      <div class="card p-4">
        <div class="flex flex-wrap items-center gap-3">
          <div class="flex items-center gap-2">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.timeRange') }}:</span>
            <DateRangePicker
              v-model:start-date="filters.start_date"
              v-model:end-date="filters.end_date"
              @change="handleDateRangeChange"
            />
          </div>
          <input
            v-model="filters.user"
            class="input min-w-48 flex-1"
            :placeholder="t('finance.userPlaceholder')"
            @keyup.enter="applyFilters"
          />
          <input
            v-model="filters.exclude_users"
            class="input min-w-56 flex-1 border-amber-300 dark:border-amber-700"
            :placeholder="t('finance.excludeUsersPlaceholder')"
            :title="t('finance.excludeUsersHint')"
            @keyup.enter="applyFilters"
          />
          <input
            v-model="filters.keyword"
            class="input min-w-48 flex-1"
            :placeholder="t('finance.keywordPlaceholder')"
            @keyup.enter="applyFilters"
          />
          <Select v-model="filters.source" class="w-44" :options="sourceOptions" @change="applyFilters" />
          <Select v-model="filters.direction" class="w-36" :options="directionOptions" @change="applyFilters" />
          <Select v-model="filters.payment_type" class="w-40" :options="paymentTypeOptions" @change="applyFilters" />
          <button type="button" class="btn btn-secondary" :disabled="loading" :title="t('common.refresh')" @click="loadLedger">
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
          <button type="button" class="btn btn-primary" :disabled="exporting" @click="exportCsv">
            <Icon name="download" size="sm" />
            {{ t('finance.exportCsv') }}
          </button>
        </div>
      </div>

      <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <div v-for="card in summaryCards" :key="card.key" class="card p-4">
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ card.label }}</p>
          <p :class="['mt-1 text-xl font-bold', card.className]">{{ card.value }}</p>
        </div>
      </div>

      <div class="card overflow-hidden">
        <div v-if="loading" class="flex items-center justify-center py-16">
          <LoadingSpinner />
        </div>
        <div v-else-if="items.length === 0" class="py-16 text-center text-sm text-gray-500 dark:text-gray-400">
          {{ t('finance.noRecords') }}
        </div>
        <template v-else>
          <div class="overflow-x-auto">
            <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
              <thead class="bg-gray-50 dark:bg-dark-800/70">
                <tr>
                  <th v-for="heading in headings" :key="heading.key" class="whitespace-nowrap px-4 py-3 text-left text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
                    {{ heading.label }}
                  </th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-900">
                <tr v-for="item in items" :key="item.id" class="hover:bg-gray-50 dark:hover:bg-dark-800/60">
                  <td class="whitespace-nowrap px-4 py-3 text-sm text-gray-600 dark:text-gray-300">{{ formatDateTime(item.occurred_at) }}</td>
                  <td class="px-4 py-3">
                    <p class="max-w-48 truncate text-sm font-medium text-gray-900 dark:text-white">{{ item.user_email || item.user_name || `#${item.user_id}` }}</p>
                    <p class="text-xs text-gray-400">#{{ item.user_id }}<span v-if="item.user_name"> · {{ item.user_name }}</span></p>
                  </td>
                  <td class="whitespace-nowrap px-4 py-3 text-sm">
                    <span :class="['inline-flex rounded-full px-2 py-1 text-xs font-medium', sourceClass(item.source)]">{{ sourceLabel(item.source) }}</span>
                  </td>
                  <td :class="['whitespace-nowrap px-4 py-3 text-right text-sm font-semibold', item.amount >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400']">
                    {{ item.amount >= 0 ? '+' : '' }}${{ Math.abs(item.amount).toFixed(8) }}
                  </td>
                  <td class="whitespace-nowrap px-4 py-3 text-sm text-gray-600 dark:text-gray-300">{{ directionLabel(item.direction) }}</td>
                  <td class="whitespace-nowrap px-4 py-3 font-mono text-xs text-gray-600 dark:text-gray-300">{{ item.reference || '-' }}</td>
                  <td class="whitespace-nowrap px-4 py-3 text-sm text-gray-600 dark:text-gray-300">{{ item.payment_type || '-' }}</td>
                  <td class="max-w-64 px-4 py-3 text-sm text-gray-500 dark:text-gray-400" :title="item.notes">{{ item.notes || '-' }}</td>
                  <td class="whitespace-nowrap px-4 py-3 text-xs text-gray-500 dark:text-gray-400">{{ item.status }}</td>
                </tr>
              </tbody>
            </table>
          </div>
          <Pagination
            :page="pagination.page"
            :total="pagination.total"
            :page-size="pagination.page_size"
            @update:page="handlePageChange"
            @update:page-size="handlePageSizeChange"
          />
        </template>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI, type FinanceLedgerEntry, type FinanceLedgerSource, type FinanceLedgerDirection } from '@/api/admin'
import { formatDateTime } from '@/utils/format'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const appStore = useAppStore()
const route = useRoute()
const router = useRouter()

const localDate = (date: Date) => {
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}
const today = new Date()
const yesterday = new Date(today.getTime() - 24 * 60 * 60 * 1000)
const defaultStartDate = localDate(yesterday)
const defaultEndDate = localDate(today)

const firstQuery = (value: unknown): string => {
  if (Array.isArray(value)) return typeof value[0] === 'string' ? value[0] : ''
  return typeof value === 'string' ? value : ''
}

interface LedgerFilters {
  start_date: string
  end_date: string
  user: string
  exclude_users: string
  source: FinanceLedgerSource | ''
  direction: FinanceLedgerDirection | ''
  payment_type: string
  keyword: string
}

const filters = reactive<LedgerFilters>({
  start_date: firstQuery(route.query.start_date) || defaultStartDate,
  end_date: firstQuery(route.query.end_date) || defaultEndDate,
  user: firstQuery(route.query.user),
  exclude_users: firstQuery(route.query.exclude_users),
  source: firstQuery(route.query.source) as FinanceLedgerSource | '',
  direction: firstQuery(route.query.direction) as FinanceLedgerDirection | '',
  payment_type: firstQuery(route.query.payment_type),
  keyword: firstQuery(route.query.keyword),
})
const items = ref<FinanceLedgerEntry[]>([])
const loading = ref(false)
const exporting = ref(false)
const summary = reactive({ income: 0, deduction: 0, net: 0, count: 0 })
const pagination = reactive({
  page: Number(firstQuery(route.query.page)) || 1,
  page_size: Number(firstQuery(route.query.page_size)) || 20,
  total: 0,
})

const sourceOptions = computed(() => [
  { value: '', label: t('finance.allSources') },
  { value: 'payment', label: t('finance.sourcePayment') },
  { value: 'refund', label: t('finance.sourceRefund') },
  { value: 'redeem', label: t('finance.sourceRedeem') },
  { value: 'admin_adjustment', label: t('finance.sourceAdminAdjustment') },
  { value: 'affiliate_transfer', label: t('finance.sourceAffiliateTransfer') },
])
const directionOptions = computed(() => [
  { value: '', label: t('finance.allDirections') },
  { value: 'income', label: t('finance.directionIncome') },
  { value: 'deduction', label: t('finance.directionDeduction') },
])
const paymentTypeOptions = computed(() => [
  { value: '', label: t('finance.allPaymentTypes') },
  { value: 'alipay', label: t('finance.paymentTypeAlipay') },
  { value: 'wxpay', label: t('finance.paymentTypeWxpay') },
  { value: 'stripe', label: t('finance.paymentTypeStripe') },
  { value: 'airwallex', label: t('finance.paymentTypeAirwallex') },
])
const headings = computed(() => [
  { key: 'time', label: t('finance.time') },
  { key: 'user', label: t('finance.user') },
  { key: 'source', label: t('finance.source') },
  { key: 'amount', label: t('finance.balanceChange') },
  { key: 'direction', label: t('finance.direction') },
  { key: 'reference', label: t('finance.reference') },
  { key: 'payment_type', label: t('finance.paymentType') },
  { key: 'notes', label: t('finance.notes') },
  { key: 'status', label: t('finance.status') },
])
const summaryCards = computed(() => [
  { key: 'income', label: t('finance.income'), value: `$${summary.income.toFixed(8)}`, className: 'text-emerald-600 dark:text-emerald-400' },
  { key: 'deduction', label: t('finance.deduction'), value: `$${summary.deduction.toFixed(8)}`, className: 'text-red-600 dark:text-red-400' },
  { key: 'net', label: t('finance.net'), value: `${summary.net >= 0 ? '+' : ''}$${summary.net.toFixed(8)}`, className: summary.net >= 0 ? 'text-blue-600 dark:text-blue-400' : 'text-red-600 dark:text-red-400' },
  { key: 'count', label: t('finance.recordCount'), value: String(summary.count), className: 'text-gray-900 dark:text-white' },
])

const timezone = () => {
  try { return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC' } catch { return 'UTC' }
}

function buildParams(includePage = true) {
  const params: Record<string, string | number> = {
    start_date: filters.start_date,
    end_date: filters.end_date,
    timezone: timezone(),
  }
  if (includePage) {
    params.page = pagination.page
    params.page_size = pagination.page_size
  }
  for (const key of ['user', 'exclude_users', 'source', 'direction', 'payment_type', 'keyword'] as const) {
    const value = filters[key]
    if (value) params[key] = value
  }
  return params
}

function syncRoute() {
  const query: Record<string, string> = {}
  const { timezone: _, ...routeParams } = buildParams()
  for (const [key, value] of Object.entries(routeParams)) {
    if (String(value)) query[key] = String(value)
  }
  void router.replace({ query })
}

function applyRouteQuery(query: typeof route.query) {
  const nextFilters: LedgerFilters = {
    start_date: firstQuery(query.start_date) || defaultStartDate,
    end_date: firstQuery(query.end_date) || defaultEndDate,
    user: firstQuery(query.user),
    exclude_users: firstQuery(query.exclude_users),
    source: firstQuery(query.source) as FinanceLedgerSource | '',
    direction: firstQuery(query.direction) as FinanceLedgerDirection | '',
    payment_type: firstQuery(query.payment_type),
    keyword: firstQuery(query.keyword),
  }
  const nextPage = Number(firstQuery(query.page)) || 1
  const nextPageSize = Number(firstQuery(query.page_size)) || 20
  let changed = false
  for (const key of Object.keys(nextFilters) as (keyof LedgerFilters)[]) {
    if (filters[key] !== nextFilters[key]) changed = true
  }
  Object.assign(filters, nextFilters)
  if (pagination.page !== nextPage) {
    pagination.page = nextPage
    changed = true
  }
  if (pagination.page_size !== nextPageSize) {
    pagination.page_size = nextPageSize
    changed = true
  }
  return changed
}

async function loadLedger() {
  loading.value = true
  try {
    const result = await adminAPI.finance.getLedger(buildParams())
    items.value = result.items || []
    pagination.total = result.total || 0
    summary.income = result.summary?.income || 0
    summary.deduction = result.summary?.deduction || 0
    summary.net = result.summary?.net || 0
    summary.count = result.summary?.count || 0
  } catch (error) {
    appStore.showError((error as Error)?.message || t('common.error'))
  } finally {
    loading.value = false
  }
}

function applyFilters() {
  pagination.page = 1
  syncRoute()
  void loadLedger()
}
function handleDateRangeChange(range: { startDate: string; endDate: string }) {
  filters.start_date = range.startDate
  filters.end_date = range.endDate
  applyFilters()
}
function handlePageChange(page: number) {
  pagination.page = page
  syncRoute()
  void loadLedger()
}
function handlePageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  syncRoute()
  void loadLedger()
}
async function exportCsv() {
  exporting.value = true
  try {
    const blob = await adminAPI.finance.exportLedger(buildParams(false))
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `finance-ledger-${filters.start_date}-to-${filters.end_date}.csv`
    link.click()
    URL.revokeObjectURL(url)
  } catch (error) {
    appStore.showError((error as Error)?.message || t('finance.exportFailed'))
  } finally {
    exporting.value = false
  }
}
function sourceLabel(source: string) {
  const keys: Record<string, string> = {
    payment: 'finance.sourcePayment',
    refund: 'finance.sourceRefund',
    redeem: 'finance.sourceRedeem',
    admin_adjustment: 'finance.sourceAdminAdjustment',
    affiliate_transfer: 'finance.sourceAffiliateTransfer',
  }
  return t(keys[source] || 'finance.source')
}
function sourceClass(source: string) {
  if (source === 'payment') return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
  if (source === 'refund') return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
  if (source === 'redeem') return 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-300'
  if (source === 'admin_adjustment') return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
}
function directionLabel(direction: string) {
  return t(direction === 'income' ? 'finance.directionIncome' : 'finance.directionDeduction')
}

watch(() => route.query, (query) => {
  if (route.path !== '/admin/finance/ledger') return
  if (applyRouteQuery(query)) {
    void loadLedger()
  }
}, { deep: true })

onMounted(() => {
  syncRoute()
  void loadLedger()
})
</script>
