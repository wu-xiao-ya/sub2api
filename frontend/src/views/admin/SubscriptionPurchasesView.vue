<template>
  <AppLayout>
    <div class="space-y-5">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.subscriptionPurchases.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.subscriptionPurchases.description') }}</p>
        </div>
        <button class="btn btn-secondary" :disabled="loading" type="button" @click="loadPurchases">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          {{ t('common.refresh') }}
        </button>
      </div>

      <section class="card p-4">
        <div class="mb-3 flex items-center justify-between">
          <h2 class="font-medium text-gray-900 dark:text-white">{{ t('admin.subscriptionPurchases.grantTitle') }}</h2>
          <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.subscriptionPurchases.grantHint') }}</span>
        </div>
        <div class="grid gap-3 md:grid-cols-4">
          <label class="text-sm">
            <span class="mb-1 block text-gray-600 dark:text-dark-300">{{ t('admin.subscriptionPurchases.userIds') }}</span>
            <input v-model="grantUserIds" class="input w-full" :placeholder="t('admin.subscriptionPurchases.userIdsPlaceholder')" />
          </label>
          <label class="text-sm">
            <span class="mb-1 block text-gray-600 dark:text-dark-300">{{ t('admin.subscriptionPurchases.plan') }}</span>
            <select v-model.number="grantPlanId" class="input w-full">
              <option :value="0">{{ t('admin.subscriptionPurchases.selectPlan') }}</option>
              <option v-for="plan in plans" :key="plan.id" :value="plan.id">
                #{{ plan.id }} {{ plan.name }} ({{ plan.tier_code || 'standard' }})
              </option>
            </select>
          </label>
          <label class="text-sm md:col-span-2">
            <span class="mb-1 block text-gray-600 dark:text-dark-300">{{ t('admin.subscriptionPurchases.notes') }}</span>
            <input v-model="grantNotes" class="input w-full" />
          </label>
        </div>
        <div class="mt-3 flex flex-wrap gap-2">
          <button class="btn btn-primary" :disabled="granting || !grantPlanId || parsedUserIds.length !== 1" type="button" @click="grantOne">
            {{ granting ? t('common.loading') : t('admin.subscriptionPurchases.grant') }}
          </button>
          <button class="btn btn-secondary" :disabled="granting || !grantPlanId || parsedUserIds.length < 1" type="button" @click="grantMany">
            {{ granting ? t('common.loading') : t('admin.subscriptionPurchases.bulkGrant') }}
          </button>
        </div>
      </section>

      <section class="card p-4">
        <div class="grid gap-3 md:grid-cols-6">
          <input v-model="filters.keyword" class="input md:col-span-2" :placeholder="t('admin.subscriptionPurchases.keyword')" @keyup.enter="reload" />
          <input v-model.number="filters.user_id" class="input" type="number" :placeholder="t('admin.subscriptionPurchases.userId')" @keyup.enter="reload" />
          <select v-model="filters.status" class="input">
            <option value="">{{ t('admin.subscriptionPurchases.allStatuses') }}</option>
            <option value="active">{{ t('admin.subscriptionPurchases.status.active') }}</option>
            <option value="revoked">{{ t('admin.subscriptionPurchases.status.revoked') }}</option>
            <option value="suspended">{{ t('admin.subscriptionPurchases.status.suspended') }}</option>
          </select>
          <input v-model="filters.platform" class="input" :placeholder="t('admin.subscriptionPurchases.platform')" @keyup.enter="reload" />
          <button class="btn btn-primary" type="button" @click="reload">{{ t('common.search') }}</button>
        </div>
      </section>

      <div v-if="errorMessage" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
        {{ errorMessage }}
      </div>

      <section class="card overflow-hidden">
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead class="bg-gray-50 dark:bg-dark-800">
              <tr>
                <th v-for="label in headers" :key="label" class="whitespace-nowrap px-4 py-3 text-left font-medium text-gray-500 dark:text-dark-400">{{ label }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="item in purchases" :key="item.id" class="align-top">
                <td class="whitespace-nowrap px-4 py-3 font-medium text-gray-900 dark:text-white">#{{ item.id }}</td>
                <td class="px-4 py-3">
                  <div class="font-medium text-gray-900 dark:text-white">{{ item.user_email || item.username || `#${item.user_id}` }}</div>
                  <div class="text-xs text-gray-500 dark:text-dark-400">ID {{ item.user_id }}</div>
                </td>
                <td class="px-4 py-3">
                  <div class="font-medium text-gray-900 dark:text-white">{{ item.name }}</div>
                  <div class="text-xs text-gray-500 dark:text-dark-400">{{ item.tier_code }} · {{ item.source }}</div>
                </td>
                <td class="px-4 py-3">
                  <div class="flex max-w-xs flex-wrap gap-1">
                    <span v-for="group in item.groups" :key="`${item.id}-${group.id}`" class="rounded bg-gray-100 px-2 py-1 text-xs dark:bg-dark-700">
                      {{ group.name }} <span class="text-gray-400">({{ group.platform }})</span>
                    </span>
                  </div>
                </td>
                <td class="whitespace-nowrap px-4 py-3">
                  <span :class="statusClass(item.status)">{{ item.status }}</span>
                  <div class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ formatDate(item.expires_at) }}</div>
                </td>
                <td class="whitespace-nowrap px-4 py-3">
                  <div>{{ formatUsd(item.lifetime_usage_usd) }} / {{ limit(item.lifetime_quota_usd) }}</div>
                  <div class="text-xs text-gray-500 dark:text-dark-400">
                    {{ t('admin.subscriptionPurchases.concurrency') }} {{ item.concurrency_entitlement }}
                  </div>
                  <div class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                    {{ t('admin.subscriptionPurchases.billingPriority') }}:
                    {{ item.billing_priority === 'balance'
                      ? t('userSubscriptions.balancePriority')
                      : t('userSubscriptions.subscriptionPriority') }}
                  </div>
                </td>
                <td class="whitespace-nowrap px-4 py-3">
                  <div class="flex flex-wrap gap-1">
                    <button v-if="item.status === 'active'" class="btn btn-xs btn-secondary" type="button" @click="adjust(item.id, 'revoke')">{{ t('admin.subscriptionPurchases.revoke') }}</button>
                    <button v-else-if="item.status === 'revoked'" class="btn btn-xs btn-secondary" type="button" @click="adjust(item.id, 'restore')">{{ t('admin.subscriptionPurchases.restore') }}</button>
                    <button class="btn btn-xs btn-secondary" type="button" @click="extend(item.id)">{{ t('admin.subscriptionPurchases.extend') }}</button>
                    <button class="btn btn-xs btn-secondary" type="button" @click="resetQuota(item.id)">{{ t('admin.subscriptionPurchases.resetQuota') }}</button>
                  </div>
                </td>
              </tr>
              <tr v-if="!loading && purchases.length === 0">
                <td :colspan="headers.length" class="px-4 py-12 text-center text-gray-500 dark:text-dark-400">{{ t('admin.subscriptionPurchases.empty') }}</td>
              </tr>
              <tr v-if="loading">
                <td :colspan="headers.length" class="px-4 py-12 text-center text-gray-500 dark:text-dark-400">{{ t('common.loading') }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="flex items-center justify-between border-t border-gray-100 px-4 py-3 text-sm dark:border-dark-700">
          <span class="text-gray-500 dark:text-dark-400">{{ t('admin.subscriptionPurchases.total', { count: total }) }}</span>
          <div class="flex gap-2">
            <button class="btn btn-xs btn-secondary" :disabled="page <= 1" type="button" @click="page--; loadPurchases()">{{ t('common.previous') }}</button>
            <span class="px-2 py-1 text-gray-500">{{ page }}</span>
            <button class="btn btn-xs btn-secondary" :disabled="page * pageSize >= total" type="button" @click="page++; loadPurchases()">{{ t('common.next') }}</button>
          </div>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { adminPaymentAPI } from '@/api/admin/payment'
import adminSubscriptionPurchasesAPI, { type AdminPurchaseRecord } from '@/api/admin/subscriptionPurchases'
import type { SubscriptionPlan } from '@/types/payment'

const { t } = useI18n()
const appStore = useAppStore()
const purchases = ref<AdminPurchaseRecord[]>([])
const plans = ref<SubscriptionPlan[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const loading = ref(false)
const granting = ref(false)
const errorMessage = ref('')
const grantUserIds = ref('')
const grantPlanId = ref(0)
const grantNotes = ref('')
const filters = reactive({ keyword: '', user_id: undefined as number | undefined, status: '', platform: '' })

const headers = computed(() => [
  t('admin.subscriptionPurchases.id'),
  t('admin.subscriptionPurchases.user'),
  t('admin.subscriptionPurchases.plan'),
  t('admin.subscriptionPurchases.groups'),
  t('admin.subscriptionPurchases.statusLabel'),
  t('admin.subscriptionPurchases.usage'),
  t('common.actions'),
])

const parsedUserIds = computed(() => grantUserIds.value.split(/[,\s]+/).map(Number).filter((id) => Number.isInteger(id) && id > 0))

async function loadPurchases() {
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await adminSubscriptionPurchasesAPI.list({ ...filters, page: page.value, page_size: pageSize })
    purchases.value = response.data.items
    total.value = response.data.total
  } catch (error: any) {
    errorMessage.value = error?.response?.data?.message || error?.response?.data?.detail || t('admin.subscriptionPurchases.loadFailed')
  } finally {
    loading.value = false
  }
}

async function loadPlans() {
  try {
    plans.value = (await adminPaymentAPI.getPlans()).data || []
  } catch {
    // The page remains usable for existing records when payment plans are unavailable.
  }
}

function reload() {
  page.value = 1
  loadPurchases()
}

async function grantOne() {
  await grant(false)
}

async function grantMany() {
  await grant(true)
}

async function grant(bulk: boolean) {
  if (!grantPlanId.value || parsedUserIds.value.length === 0) return
  granting.value = true
  try {
    if (bulk) {
      await adminSubscriptionPurchasesAPI.bulkGrant({ user_ids: parsedUserIds.value, plan_id: grantPlanId.value, notes: grantNotes.value })
    } else {
      await adminSubscriptionPurchasesAPI.grant({ user_id: parsedUserIds.value[0], plan_id: grantPlanId.value, notes: grantNotes.value })
    }
    appStore.showSuccess(t('admin.subscriptionPurchases.grantSuccess'))
    await loadPurchases()
  } catch (error: any) {
    appStore.showError(error?.response?.data?.message || error?.response?.data?.detail || t('admin.subscriptionPurchases.grantFailed'))
  } finally {
    granting.value = false
  }
}

async function adjust(id: number, action: 'revoke' | 'restore') {
  if (!window.confirm(t(`admin.subscriptionPurchases.confirm.${action}`))) return
  try {
    if (action === 'revoke') await adminSubscriptionPurchasesAPI.revoke(id)
    else await adminSubscriptionPurchasesAPI.restore(id)
    await loadPurchases()
  } catch (error: any) {
    appStore.showError(error?.response?.data?.message || error?.response?.data?.detail || t('admin.subscriptionPurchases.actionFailed'))
  }
}

async function extend(id: number) {
  const raw = window.prompt(t('admin.subscriptionPurchases.extendPrompt'), '30')
  if (raw === null) return
  const days = Number(raw)
  if (!Number.isInteger(days) || days === 0) return
  try {
    await adminSubscriptionPurchasesAPI.extend(id, days)
    await loadPurchases()
  } catch (error: any) {
    appStore.showError(error?.response?.data?.message || error?.response?.data?.detail || t('admin.subscriptionPurchases.actionFailed'))
  }
}

async function resetQuota(id: number) {
  if (!window.confirm(t('admin.subscriptionPurchases.confirm.resetQuota'))) return
  try {
    await adminSubscriptionPurchasesAPI.resetQuota(id, { daily: true, weekly: true, monthly: true })
    await loadPurchases()
  } catch (error: any) {
    appStore.showError(error?.response?.data?.message || error?.response?.data?.detail || t('admin.subscriptionPurchases.actionFailed'))
  }
}

function formatDate(value: string) {
  return new Date(value).toLocaleString()
}

function formatUsd(value: number) {
  return `$${(value || 0).toFixed(4)}`
}

function limit(value: number) {
  return value > 0 ? formatUsd(value) : t('admin.subscriptionPurchases.unlimited')
}

function statusClass(status: string) {
  return status === 'active'
    ? 'rounded bg-emerald-100 px-2 py-1 text-xs text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    : 'rounded bg-gray-100 px-2 py-1 text-xs text-gray-600 dark:bg-dark-700 dark:text-dark-300'
}

onMounted(() => {
  loadPlans()
  loadPurchases()
})
</script>
