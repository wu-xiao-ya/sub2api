<template>
  <AppLayout>
    <div class="space-y-6">
      <div v-if="loading" class="flex justify-center py-12" data-testid="subscriptions-loading">
        <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent" />
      </div>

      <div v-else-if="sharedSubscriptions.length === 0" class="card p-12 text-center">
        <div class="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-700">
          <Icon name="creditCard" size="xl" class="text-gray-400" />
        </div>
        <h3 class="mb-2 text-lg font-semibold text-gray-900 dark:text-white">
          {{ t('userSubscriptions.noActiveSubscriptions') }}
        </h3>
        <p class="text-gray-500 dark:text-dark-400">
          {{ t('userSubscriptions.noActiveSubscriptionsDesc') }}
        </p>
        <button class="btn btn-primary mt-5" type="button" @click="openPurchase">
          {{ t('nav.buySubscription') }}
        </button>
      </div>

      <section v-else class="space-y-3">
        <div class="flex flex-col gap-4 border-b border-gray-100 pb-3 dark:border-dark-700 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t('userSubscriptions.sharedTitle') }}
            </h2>
            <p class="text-sm text-gray-500 dark:text-dark-400">
              {{ t('userSubscriptions.sharedDescription') }}
            </p>
          </div>
          <label class="flex max-w-md cursor-pointer items-start gap-3 text-sm text-gray-700 dark:text-dark-200">
            <input
              v-model="balanceTopupEnabled"
              type="checkbox"
              class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :disabled="updatingPreference"
              @change="toggleBalanceTopupPreference"
            />
            <span>
              <span class="block font-medium">{{ t('userSubscriptions.subscriptionBalanceTopup') }}</span>
              <span class="mt-0.5 block text-xs text-gray-500 dark:text-dark-400">
                {{ t('userSubscriptions.subscriptionBalanceTopupHint') }}
              </span>
            </span>
          </label>
        </div>

        <div class="grid gap-4 lg:grid-cols-2">
          <article
            v-for="purchase in sharedSubscriptions"
            :key="purchase.id"
            class="overflow-hidden rounded-2xl border border-primary-100 bg-white dark:border-primary-900/40 dark:bg-dark-800"
            :data-testid="`shared-subscription-${purchase.id}`"
          >
            <header class="flex items-start justify-between border-b border-gray-100 p-4 dark:border-dark-700">
              <div>
                <div class="flex flex-wrap items-center gap-2">
                  <h3 class="font-semibold text-gray-900 dark:text-white">{{ purchase.name }}</h3>
                  <span class="rounded-md bg-primary-50 px-2 py-0.5 text-[11px] font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
                    {{ tierLabel(purchase.tier_code) }}
                  </span>
                </div>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                  {{ formatExpirationDate(purchase.expires_at) }}
                </p>
              </div>
              <span class="rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
                {{ statusLabel(purchase.status) }}
              </span>
            </header>

            <div class="space-y-4 p-4">
              <div>
                <div class="mb-2 flex items-center justify-between text-sm">
                  <span class="font-medium text-gray-700 dark:text-gray-300">
                    {{ t('userSubscriptions.sharedGroups') }}
                  </span>
                  <span class="text-xs text-gray-500 dark:text-dark-400">{{ purchase.groups.length }}</span>
                </div>
                <div class="flex flex-wrap gap-2">
                  <span
                    v-for="group in purchase.groups"
                    :key="group.id"
                    class="rounded-md border border-gray-200 px-2 py-1 text-xs text-gray-600 dark:border-dark-600 dark:text-dark-300"
                  >
                    {{ group.name }}
                  </span>
                </div>
              </div>

              <div v-if="sharedQuotaRows(purchase).length" class="grid gap-3 sm:grid-cols-2">
                <div v-for="quota in sharedQuotaRows(purchase)" :key="quota.key" class="space-y-1">
                  <div class="flex items-center justify-between gap-2 text-xs">
                    <span class="text-gray-500 dark:text-dark-400">{{ quota.label }}</span>
                    <span class="font-medium text-gray-700 dark:text-gray-300">
                      {{ formatUsd(quota.used) }} / {{ quota.limit > 0 ? formatUsd(quota.limit) : t('userSubscriptions.unlimited') }}
                    </span>
                  </div>
                  <div class="h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                    <div
                      class="h-full rounded-full transition-all"
                      :class="sharedQuotaClass(quota.used, quota.limit)"
                      :style="{ width: sharedQuotaWidth(quota.used, quota.limit) }"
                    />
                  </div>
                </div>
              </div>

              <div class="flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 pt-3 text-sm dark:border-dark-700">
                <span class="text-gray-600 dark:text-dark-300">
                  {{ t('userSubscriptions.sharedConcurrency') }}:
                  <strong class="text-gray-900 dark:text-white">{{ purchase.concurrency_entitlement }}</strong>
                </span>
              </div>
              <div class="flex flex-col gap-3 border-t border-gray-100 pt-3 text-xs dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <div class="font-medium text-gray-700 dark:text-gray-300">
                    {{ t('userSubscriptions.billingPriority') }}
                  </div>
                  <div class="mt-1 text-gray-500 dark:text-dark-400">
                    {{ t('userSubscriptions.billingPriorityHint') }}
                  </div>
                </div>
                <div class="inline-flex shrink-0 rounded-lg border border-gray-200 p-0.5 dark:border-dark-600" role="group">
                  <button
                    v-for="priority in billingPriorities"
                    :key="priority.value"
                    type="button"
                    class="rounded-md px-3 py-1.5 font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50"
                    :class="purchase.billing_priority === priority.value
                      ? 'bg-primary-600 text-white shadow-sm'
                      : 'text-gray-600 hover:bg-gray-100 dark:text-dark-300 dark:hover:bg-dark-700'"
                    :disabled="updatingPurchaseId === purchase.id || purchase.billing_priority === priority.value"
                    @click="setBillingPriority(purchase, priority.value)"
                  >
                    {{ priority.label }}
                  </button>
                </div>
              </div>
            </div>
          </article>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import subscriptionsAPI, { type SharedSubscription } from '@/api/subscriptions'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { formatDateTimeToMinute } from '@/utils/format'

const { t, te } = useI18n()
const router = useRouter()
const appStore = useAppStore()
const sharedSubscriptions = ref<SharedSubscription[]>([])
const loading = ref(true)
const updatingPurchaseId = ref<number | null>(null)
const balanceTopupEnabled = ref(false)
const updatingPreference = ref(false)
const billingPriorities = [
  { value: 'subscription' as const, label: t('userSubscriptions.subscriptionPriority') },
  { value: 'balance' as const, label: t('userSubscriptions.balancePriority') },
]

async function loadSubscriptions() {
  loading.value = true
  try {
    const [subscriptions, preferences] = await Promise.all([
      subscriptionsAPI.getMySharedSubscriptions(),
      subscriptionsAPI.getSubscriptionPreferences(),
    ])
    sharedSubscriptions.value = subscriptions
    balanceTopupEnabled.value = preferences.balance_topup_enabled
  } catch (error) {
    console.error('Failed to load shared subscriptions:', error)
    appStore.showError(t('userSubscriptions.failedToLoad'))
  } finally {
    loading.value = false
  }
}

function openPurchase() {
  router.push({ path: '/purchase', query: { tab: 'subscription' } })
}

function tierLabel(tierCode: string): string {
  const key = `payment.admin.tier${tierCode.charAt(0).toUpperCase()}${tierCode.slice(1)}`
  return te(key) ? t(key) : tierCode
}

function statusLabel(status: string): string {
  const key = `userSubscriptions.status.${status}`
  return te(key) ? t(key) : status
}

function formatUsd(value: number): string {
  return `$${(value || 0).toFixed(2)}`
}

function sharedQuotaRows(purchase: SharedSubscription) {
  return [
    { key: 'lifetime', label: t('userSubscriptions.lifetime'), used: purchase.lifetime_usage_usd, limit: purchase.lifetime_quota_usd },
    { key: 'daily', label: t('userSubscriptions.daily'), used: purchase.daily_usage_usd, limit: purchase.daily_quota_usd },
    { key: 'weekly', label: t('userSubscriptions.weekly'), used: purchase.weekly_usage_usd, limit: purchase.weekly_quota_usd },
    { key: 'monthly', label: t('userSubscriptions.monthly'), used: purchase.monthly_usage_usd, limit: purchase.monthly_quota_usd },
  ].filter((item) => item.limit > 0 || item.used > 0)
}

function sharedQuotaWidth(used: number, limit: number): string {
  if (!limit || limit <= 0) return '0%'
  return `${Math.min(100, Math.max(0, (used / limit) * 100))}%`
}

function sharedQuotaClass(used: number, limit: number): string {
  if (!limit || limit <= 0) return 'bg-gray-400'
  const ratio = used / limit
  if (ratio >= 0.9) return 'bg-red-500'
  if (ratio >= 0.7) return 'bg-orange-500'
  return 'bg-primary-500'
}

async function toggleBalanceTopupPreference() {
  if (updatingPreference.value) return
  const previous = !balanceTopupEnabled.value
  updatingPreference.value = true
  try {
    const updated = await subscriptionsAPI.setSubscriptionBalanceTopupPreference(balanceTopupEnabled.value)
    balanceTopupEnabled.value = updated.balance_topup_enabled
  } catch (error) {
    balanceTopupEnabled.value = previous
    console.error('Failed to update global balance top-up:', error)
    appStore.showError(t('userSubscriptions.failedToUpdate'))
  } finally {
    updatingPreference.value = false
  }
}

async function setBillingPriority(purchase: SharedSubscription, priority: SharedSubscription['billing_priority']) {
  if (updatingPurchaseId.value !== null || purchase.billing_priority === priority) return
  const previous = purchase.billing_priority
  updatingPurchaseId.value = purchase.id
  purchase.billing_priority = priority
  try {
    const updated = await subscriptionsAPI.setSharedBillingPriority(purchase.id, priority)
    purchase.billing_priority = updated.billing_priority
  } catch (error) {
    purchase.billing_priority = previous
    console.error('Failed to update billing priority:', error)
    appStore.showError(t('userSubscriptions.failedToUpdate'))
  } finally {
    updatingPurchaseId.value = null
  }
}

function formatExpirationDate(expiresAt: string): string {
  const expires = new Date(expiresAt)
  const days = Math.ceil((expires.getTime() - Date.now()) / 86_400_000)
  if (days < 0) return t('userSubscriptions.status.expired')

  const date = formatDateTimeToMinute(expires)
  if (days === 0) return `${date} (${t('common.today')})`
  if (days === 1) return `${date} (${t('common.tomorrow')})`
  return `${t('userSubscriptions.daysRemaining', { days })} (${date})`
}

onMounted(loadSubscriptions)
</script>
