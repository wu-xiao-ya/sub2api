<template>
  <BaseDialog :show="show" :title="plan ? t('payment.admin.editPlan') : t('payment.admin.createPlan')" width="wide" @close="emit('close')">
    <form id="plan-form" @submit.prevent="handleSavePlan" class="space-y-4">
      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="input-label">{{ t('payment.admin.planName') }} <span class="text-red-500">*</span></label>
          <input v-model="planForm.name" type="text" class="input" required />
        </div>
        <div>
          <label class="input-label">{{ t('payment.admin.tier') }}</label>
          <Select v-model="planForm.tier_code" :options="tierOptions" class="w-full" />
        </div>
      </div>

      <div>
        <label class="input-label">{{ t('payment.admin.includedGroups') }} <span class="text-red-500">*</span></label>
        <div class="grid max-h-40 gap-2 overflow-y-auto rounded-lg border border-gray-200 p-3 dark:border-dark-600 sm:grid-cols-2">
          <label
            v-for="group in openaiGroups"
            :key="group.id"
            class="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-gray-50 dark:hover:bg-dark-700"
          >
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="planForm.group_ids.includes(group.id)"
              @change="toggleGroup(group.id)"
            />
            <span class="truncate" :class="platformTextClass(group.platform)">{{ group.name }}</span>
            <span class="ml-auto text-xs text-gray-400">{{ group.rate_multiplier }}x</span>
          </label>
          <p v-if="openaiGroups.length === 0" class="text-xs text-gray-500 dark:text-dark-400">
            {{ t('payment.admin.noOpenaiGroups') }}
          </p>
        </div>
      </div>

      <div><label class="input-label">{{ t('payment.admin.planDescription') }} <span class="text-red-500">*</span></label><textarea v-model="planForm.description" rows="2" class="input" required></textarea></div>
      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="input-label">{{ t('payment.admin.price') }} <span class="text-red-500">*</span></label>
          <input v-model.number="planForm.price" type="number" step="0.01" min="0.01" class="input" required />
          <p v-if="subscriptionCnyPreview" class="mt-1 text-xs font-medium text-primary-600 dark:text-primary-400">
            {{ t('payment.admin.subscriptionCnyPayPreview', { amount: subscriptionCnyPreview.amount }) }}
            <span v-if="subscriptionCnyPreview.feeRate > 0">
              {{ t('payment.admin.subscriptionCnyPayPreviewWithFee', { feeRate: subscriptionCnyPreview.feeRate, total: subscriptionCnyPreview.total }) }}
            </span>
          </p>
        </div>
        <div><label class="input-label">{{ t('payment.admin.originalPrice') }}</label><input v-model.number="planForm.original_price" type="number" step="0.01" min="0" class="input" /></div>
      </div>
      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="input-label">{{ t('payment.admin.concurrency') }}</label>
          <input v-model.number="planForm.concurrency_entitlement" type="number" min="0" class="input" />
        </div>
        <div>
          <label class="input-label">{{ t('payment.admin.lifetimeQuota') }}</label>
          <input v-model.number="planForm.lifetime_quota_usd" type="number" min="0" step="0.000001" class="input" />
        </div>
      </div>
      <div class="grid grid-cols-3 gap-4">
        <div>
          <label class="input-label">{{ t('payment.admin.dailyQuota') }}</label>
          <input v-model.number="planForm.daily_quota_usd" type="number" min="0" step="0.000001" class="input" />
        </div>
        <div>
          <label class="input-label">{{ t('payment.admin.weeklyQuota') }}</label>
          <input v-model.number="planForm.weekly_quota_usd" type="number" min="0" step="0.000001" class="input" />
        </div>
        <div>
          <label class="input-label">{{ t('payment.admin.monthlyQuota') }}</label>
          <input v-model.number="planForm.monthly_quota_usd" type="number" min="0" step="0.000001" class="input" />
        </div>
      </div>
      <div class="grid grid-cols-2 gap-4">
        <div><label class="input-label">{{ t('payment.admin.validity') }} <span class="text-red-500">*</span></label><input v-model.number="planForm.validity_days" type="number" min="1" class="input" required /></div>
        <div><label class="input-label">{{ t('payment.admin.validityUnit') }} <span class="text-red-500">*</span></label><Select v-model="planForm.validity_unit" :options="validityUnitOptions" /></div>
      </div>
      <div class="grid grid-cols-2 gap-4">
        <div><label class="input-label">{{ t('payment.admin.sortOrder') }}</label><input v-model.number="planForm.sort_order" type="number" min="0" class="input" /></div>
        <div>
          <label class="input-label">{{ t('payment.admin.currency') }}</label>
          <input v-model="planForm.currency" type="text" maxlength="3" class="input uppercase" :placeholder="t('payment.admin.currencyPlaceholder')" />
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.currencyHint') }}</p>
        </div>
      </div>
      <div>
        <label class="input-label">{{ t('payment.admin.features') }}</label>
        <textarea v-model="planFeaturesText" rows="3" class="input" :placeholder="t('payment.admin.featuresPlaceholder')"></textarea>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.featuresHint') }}</p>
      </div>
      <div class="flex items-center gap-3">
        <label class="text-sm text-gray-700 dark:text-gray-300">{{ t('payment.admin.forSale') }}</label>
        <button
          type="button"
          :class="[
            'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
            planForm.for_sale ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-600'
          ]"
          @click="planForm.for_sale = !planForm.for_sale"
        >
          <span :class="[
            'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
            planForm.for_sale ? 'translate-x-5' : 'translate-x-0'
          ]" />
        </button>
      </div>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" @click="emit('close')" class="btn btn-secondary">{{ t('common.cancel') }}</button>
        <button type="submit" form="plan-form" :disabled="saving" class="btn btn-primary">{{ saving ? t('common.saving') : t('common.save') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminPaymentAPI } from '@/api/admin/payment'
import type { AdminPaymentConfig } from '@/api/admin/payment'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatPaymentAmount } from '@/components/payment/currency'
import type { SubscriptionPlan } from '@/types/payment'
import type { AdminGroup } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
 import { platformTextClass } from '@/utils/platformColors'

const props = defineProps<{
  show: boolean
  plan: SubscriptionPlan | null
  groups: AdminGroup[]
  paymentConfig?: AdminPaymentConfig | null
}>()

const emit = defineEmits<{
  close: []
  saved: []
}>()

const { t } = useI18n()
const appStore = useAppStore()

const saving = ref(false)
const planForm = reactive({
  name: '',
  group_id: null as number | null,
  group_ids: [] as number[],
  tier_code: 'standard',
  description: '',
  price: 0,
  original_price: 0,
  currency: '',
  validity_days: 30,
  validity_unit: 'days',
  sort_order: 0,
  for_sale: true,
  concurrency_entitlement: 0,
  lifetime_quota_usd: 0,
  daily_quota_usd: 0,
  weekly_quota_usd: 0,
  monthly_quota_usd: 0
})
const planFeaturesText = ref('')

const validityUnitOptions = computed(() => [
  { value: 'days', label: t('payment.admin.days') },
  { value: 'weeks', label: t('payment.admin.weeks') },
  { value: 'months', label: t('payment.admin.months') },
])

const tierOptions = computed(() => [
  { value: 'standard', label: t('payment.admin.tierStandard') },
  { value: 'pro', label: t('payment.admin.tierPro') },
  { value: 'plus', label: t('payment.admin.tierPlus') }
])

const openaiGroups = computed(() => props.groups.filter(g => g.platform === 'openai'))

function roundCnyAmount(value: number): number {
  return Math.round(value * 100) / 100
}

function ceilCnyAmount(value: number): number {
  return Math.ceil(value * 100) / 100
}

function toggleGroup(groupId: number) {
  const index = planForm.group_ids.indexOf(groupId)
  if (index >= 0) {
    planForm.group_ids.splice(index, 1)
  } else {
    planForm.group_ids.push(groupId)
  }
  planForm.group_id = planForm.group_ids[0] || null
}

const subscriptionCnyPreview = computed(() => {
  const price = Number(planForm.price) || 0
  const rate = Number(props.paymentConfig?.subscription_usd_to_cny_rate) || 0
  if (price <= 0 || rate <= 0) return null

  const amount = roundCnyAmount(price * rate)
  const feeRate = Number(props.paymentConfig?.recharge_fee_rate) || 0
  const fee = feeRate > 0 ? ceilCnyAmount((amount * feeRate) / 100) : 0
  const total = feeRate > 0 ? roundCnyAmount(amount + fee) : amount

  return {
    amount: formatPaymentAmount(amount, 'CNY'),
    feeRate,
    total: formatPaymentAmount(total, 'CNY'),
  }
})

// Reset form when dialog opens
watch(() => props.show, (visible) => {
  if (!visible) return
  if (props.plan) {
    Object.assign(planForm, {
      name: props.plan.name,
      group_id: props.plan.group_id,
      group_ids: [...(props.plan.group_ids?.length ? props.plan.group_ids : [props.plan.group_id])],
      tier_code: props.plan.tier_code || 'standard',
      description: props.plan.description,
      price: props.plan.price,
      original_price: props.plan.original_price || 0,
      currency: props.plan.currency || '',
      validity_days: props.plan.validity_days,
      validity_unit: props.plan.validity_unit || 'days',
      sort_order: props.plan.sort_order || 0,
      for_sale: props.plan.for_sale,
      concurrency_entitlement: props.plan.concurrency_entitlement || 0,
      lifetime_quota_usd: props.plan.lifetime_quota_usd || 0,
      daily_quota_usd: props.plan.daily_quota_usd || 0,
      weekly_quota_usd: props.plan.weekly_quota_usd || 0,
      monthly_quota_usd: props.plan.monthly_quota_usd || 0
    })
    planFeaturesText.value = (props.plan.features || []).join('\n')
  } else {
    Object.assign(planForm, {
      name: '', group_id: null, group_ids: [], tier_code: 'standard', description: '',
      price: 0, original_price: 0, currency: '', validity_days: 30, validity_unit: 'days',
      sort_order: 0, for_sale: true, concurrency_entitlement: 0,
      lifetime_quota_usd: 0, daily_quota_usd: 0, weekly_quota_usd: 0, monthly_quota_usd: 0
    })
    planFeaturesText.value = ''
  }
})

/** Build request payload with snake_case keys matching backend JSON tags */
function buildPlanPayload() {
  const features = planFeaturesText.value.split('\n').map(f => f.trim()).filter(Boolean).join('\n')
  return {
    name: planForm.name,
    group_id: planForm.group_id,
    group_ids: planForm.group_ids,
    tier_code: planForm.tier_code,
    description: planForm.description,
    price: planForm.price,
    original_price: planForm.original_price || 0,
    currency: planForm.currency.trim().toUpperCase(),
    validity_days: planForm.validity_days,
    validity_unit: planForm.validity_unit,
    sort_order: planForm.sort_order,
    for_sale: planForm.for_sale,
    features,
    concurrency_entitlement: planForm.concurrency_entitlement,
    lifetime_quota_usd: planForm.lifetime_quota_usd,
    daily_quota_usd: planForm.daily_quota_usd,
    weekly_quota_usd: planForm.weekly_quota_usd,
    monthly_quota_usd: planForm.monthly_quota_usd
  }
}

async function handleSavePlan() {
  if (!planForm.group_id || planForm.group_ids.length === 0) {
    appStore.showError(t('payment.admin.groupRequired'))
    return
  }
  if (!planForm.price || planForm.price <= 0) {
    appStore.showError(t('payment.admin.priceRequired'))
    return
  }
  if (!planForm.validity_days || planForm.validity_days < 1) {
    appStore.showError(t('payment.admin.validityRequired'))
    return
  }
  saving.value = true
  try {
    const data = buildPlanPayload()
    if (props.plan) { await adminPaymentAPI.updatePlan(props.plan.id, data) }
    else { await adminPaymentAPI.createPlan(data) }
    appStore.showSuccess(t('common.saved'))
    emit('close')
    emit('saved')
  } catch (err: unknown) { appStore.showError(extractApiErrorMessage(err, t('common.error'))) }
  finally { saving.value = false }
}
</script>
