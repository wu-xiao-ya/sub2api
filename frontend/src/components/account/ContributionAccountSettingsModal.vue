<template>
  <BaseDialog
    :show="show"
    :title="t('accountContributions.settings.title')"
    width="wide"
    close-on-click-outside
    @close="handleClose"
  >
    <form id="contribution-account-settings-form" class="space-y-5" @submit.prevent="submit">
      <div class="grid gap-3 md:grid-cols-3">
        <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-700">
          <div class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountContributions.settings.accountLevel') }}</div>
          <div class="mt-1 font-medium text-gray-900 dark:text-white">{{ accountLevelLabel }}</div>
        </div>
        <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-700 md:col-span-2">
          <div class="text-xs text-gray-500 dark:text-dark-400">{{ t('accountContributions.settings.groups') }}</div>
          <div class="mt-2 flex flex-wrap gap-1.5">
            <span
              v-for="group in account?.groups || []"
              :key="group.id"
              class="rounded bg-primary-50 px-2 py-0.5 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
            >
              {{ group.name }}
            </span>
            <span v-if="!account?.groups?.length" class="text-sm text-gray-400">-</span>
          </div>
        </div>
      </div>

      <div class="grid gap-4 md:grid-cols-2">
        <div>
          <label class="input-label">{{ t('accountContributions.settings.name') }}</label>
          <input v-model.trim="form.name" class="input" required />
        </div>
        <div>
          <label class="input-label">{{ t('accountContributions.settings.expiresAt') }}</label>
          <input v-model="form.expires_at" type="datetime-local" class="input" />
        </div>
        <div>
          <label class="input-label">{{ t('accountContributions.settings.concurrency') }}</label>
          <input v-model.number="form.concurrency" type="number" min="0" class="input" />
        </div>
        <div>
          <label class="input-label">{{ t('accountContributions.settings.loadFactor') }}</label>
          <input v-model.number="form.load_factor" type="number" min="0" max="10000" class="input" />
        </div>
        <div class="md:col-span-2">
          <label class="input-label">{{ t('accountContributions.settings.notes') }}</label>
          <textarea v-model.trim="form.notes" class="input min-h-20" />
        </div>
      </div>

      <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
        <input v-model="form.auto_pause_on_expired" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
        <span>{{ t('accountContributions.settings.autoPauseOnExpired') }}</span>
      </label>

      <div class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-700">
        <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <label class="flex items-center gap-2 text-sm font-medium text-gray-800 dark:text-dark-100">
            <input v-model="form.temp_unschedulable_enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            <span>{{ t('accountContributions.settings.tempUnschedulable') }}</span>
          </label>
          <button type="button" class="btn btn-secondary btn-sm" @click="addRule">
            <Icon name="plus" size="sm" />
            <span>{{ t('accountContributions.settings.addRule') }}</span>
          </button>
        </div>

        <div v-if="form.temp_unschedulable_rules.length" class="space-y-3">
          <div
            v-for="(rule, index) in form.temp_unschedulable_rules"
            :key="index"
            class="grid gap-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-800 md:grid-cols-[110px_1fr_120px_auto]"
          >
            <div>
              <label class="input-label">{{ t('accountContributions.settings.errorCode') }}</label>
              <input v-model.number="rule.error_code" type="number" min="1" class="input" />
            </div>
            <div>
              <label class="input-label">{{ t('accountContributions.settings.keywords') }}</label>
              <input v-model="rule.keywords" class="input" />
            </div>
            <div>
              <label class="input-label">{{ t('accountContributions.settings.durationMinutes') }}</label>
              <input v-model.number="rule.duration_minutes" type="number" min="1" class="input" />
            </div>
            <div class="flex items-end">
              <button type="button" class="btn btn-secondary btn-sm text-red-600 dark:text-red-400" @click="removeRule(index)">
                <Icon name="trash" size="sm" />
              </button>
            </div>
            <div class="md:col-span-4">
              <label class="input-label">{{ t('accountContributions.settings.ruleDescription') }}</label>
              <input v-model="rule.description" class="input" />
            </div>
          </div>
        </div>
        <div v-else class="text-sm text-gray-400">-</div>
      </div>

      <div class="space-y-3 rounded-lg border border-gray-200 p-4 dark:border-dark-700">
        <div class="text-sm font-medium text-gray-800 dark:text-dark-100">
          {{ t('accountContributions.settings.codexProtection') }}
        </div>
        <div class="grid gap-4 md:grid-cols-2">
          <div class="space-y-2">
            <label class="input-label">{{ t('accountContributions.settings.threshold5h') }}</label>
            <input
              v-model.number="form.auto_pause_5h_threshold"
              type="number"
              min="0"
              max="100"
              step="1"
              class="input"
              :disabled="form.auto_pause_5h_disabled"
            />
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
              <input v-model="form.auto_pause_5h_disabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
              <span>{{ t('accountContributions.settings.disable5h') }}</span>
            </label>
          </div>
          <div class="space-y-2">
            <label class="input-label">{{ t('accountContributions.settings.threshold7d') }}</label>
            <input
              v-model.number="form.auto_pause_7d_threshold"
              type="number"
              min="0"
              max="100"
              step="1"
              class="input"
              :disabled="form.auto_pause_7d_disabled"
            />
            <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-200">
              <input v-model="form.auto_pause_7d_disabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
              <span>{{ t('accountContributions.settings.disable7d') }}</span>
            </label>
          </div>
        </div>
      </div>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="saving" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button type="submit" form="contribution-account-settings-form" class="btn btn-primary" :disabled="saving">
          <Icon v-if="saving" name="refresh" size="sm" class="animate-spin" />
          <span>{{ saving ? t('common.saving') : t('common.save') }}</span>
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import type { ContributionAccountConfigRequest } from '@/api/accountContributions'
import type { Account, TempUnschedulableRule } from '@/types'

interface RuleForm {
  error_code: number
  keywords: string
  duration_minutes: number
  description: string
}

const props = withDefaults(
  defineProps<{
    show: boolean
    account: Account | null
    saving?: boolean
  }>(),
  {
    saving: false
  }
)

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'save', payload: ContributionAccountConfigRequest): void
}>()

const { t } = useI18n()

const form = reactive({
  name: '',
  notes: '',
  concurrency: 1,
  load_factor: null as number | null,
  expires_at: '',
  auto_pause_on_expired: true,
  temp_unschedulable_enabled: false,
  temp_unschedulable_rules: [] as RuleForm[],
  auto_pause_5h_threshold: null as number | null,
  auto_pause_7d_threshold: null as number | null,
  auto_pause_5h_disabled: false,
  auto_pause_7d_disabled: false
})

const accountLevelLabel = computed(() => {
  const credentials = asRecord(props.account?.credentials)
  const extra = asRecord(props.account?.extra)
  const value =
    readString(credentials, 'plan_type') ||
    readString(credentials, 'account_plan') ||
    readString(extra, 'plan_type') ||
    readString(extra, 'subscription_tier_raw') ||
    readString(extra, 'subscription_tier')
  return value || '-'
})

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

function readString(record: Record<string, unknown>, key: string): string {
  const value = record[key]
  return typeof value === 'string' ? value.trim() : ''
}

function readNumber(record: Record<string, unknown>, key: string): number | null {
  const value = record[key]
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value)
    if (Number.isFinite(parsed)) return parsed
  }
  return null
}

function readBoolean(record: Record<string, unknown>, key: string): boolean {
  return record[key] === true
}

function thresholdPercent(record: Record<string, unknown>, key: string): number | null {
  const value = readNumber(record, key)
  if (value == null || value <= 0) return null
  return value <= 1 ? Math.round(value * 100) : Math.round(value)
}

function toDateTimeLocal(unixSeconds: number | null | undefined): string {
  if (!unixSeconds || unixSeconds <= 0) return ''
  const date = new Date(unixSeconds * 1000)
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function fromDateTimeLocal(value: string): number {
  if (!value) return 0
  const time = new Date(value).getTime()
  if (!Number.isFinite(time)) return 0
  return Math.floor(time / 1000)
}

function parseTempRules(account: Account | null): RuleForm[] {
  const credentials = asRecord(account?.credentials)
  const raw = credentials.temp_unschedulable_rules
  if (!Array.isArray(raw)) return []
  return raw.map((item) => {
    const record = asRecord(item)
    const keywords = Array.isArray(record.keywords)
      ? record.keywords.filter((keyword): keyword is string => typeof keyword === 'string').join(', ')
      : ''
    return {
      error_code: Number(record.error_code || 0),
      keywords,
      duration_minutes: Number(record.duration_minutes || 0),
      description: typeof record.description === 'string' ? record.description : ''
    }
  })
}

function resetForm(): void {
  const account = props.account
  const credentials = asRecord(account?.credentials)
  const extra = asRecord(account?.extra)
  form.name = account?.name || ''
  form.notes = account?.notes || ''
  form.concurrency = account?.concurrency ?? 1
  form.load_factor = account?.load_factor ?? null
  form.expires_at = toDateTimeLocal(account?.expires_at)
  form.auto_pause_on_expired = account?.auto_pause_on_expired ?? true
  form.temp_unschedulable_enabled = readBoolean(credentials, 'temp_unschedulable_enabled')
  form.temp_unschedulable_rules = parseTempRules(account)
  form.auto_pause_5h_threshold = thresholdPercent(extra, 'auto_pause_5h_threshold')
  form.auto_pause_7d_threshold = thresholdPercent(extra, 'auto_pause_7d_threshold')
  form.auto_pause_5h_disabled = readBoolean(extra, 'auto_pause_5h_disabled')
  form.auto_pause_7d_disabled = readBoolean(extra, 'auto_pause_7d_disabled')
}

function addRule(): void {
  form.temp_unschedulable_rules.push({
    error_code: 429,
    keywords: 'rate limit',
    duration_minutes: 30,
    description: ''
  })
}

function removeRule(index: number): void {
  form.temp_unschedulable_rules.splice(index, 1)
}

function buildRules(): TempUnschedulableRule[] {
  return form.temp_unschedulable_rules.map((rule) => ({
    error_code: Number(rule.error_code || 0),
    keywords: rule.keywords.split(',').map((keyword) => keyword.trim()).filter(Boolean),
    duration_minutes: Number(rule.duration_minutes || 0),
    description: rule.description.trim()
  }))
}

function submit(): void {
  const payload: ContributionAccountConfigRequest = {
    name: form.name.trim(),
    notes: form.notes.trim(),
    concurrency: Number(form.concurrency || 0),
    load_factor: form.load_factor && form.load_factor > 0 ? Number(form.load_factor) : 0,
    expires_at: fromDateTimeLocal(form.expires_at),
    auto_pause_on_expired: form.auto_pause_on_expired,
    temp_unschedulable_enabled: form.temp_unschedulable_enabled,
    temp_unschedulable_rules: buildRules(),
    auto_pause_5h_threshold: form.auto_pause_5h_disabled ? 0 : Number(form.auto_pause_5h_threshold || 0),
    auto_pause_7d_threshold: form.auto_pause_7d_disabled ? 0 : Number(form.auto_pause_7d_threshold || 0),
    auto_pause_5h_disabled: form.auto_pause_5h_disabled,
    auto_pause_7d_disabled: form.auto_pause_7d_disabled
  }
  emit('save', payload)
}

function handleClose(): void {
  if (props.saving) return
  emit('close')
}

watch(
  () => [props.show, props.account?.id],
  () => {
    if (props.show) resetForm()
  },
  { immediate: true }
)
</script>
