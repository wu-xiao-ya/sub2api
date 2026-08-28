<template>
  <BaseDialog
    :show="show"
    :title="editing ? t('admin.channelMonitor.editTitle') : t('admin.channelMonitor.createTitle')"
    width="wide"
    @close="$emit('close')"
  >
    <form id="channel-monitor-form" @submit.prevent="handleSubmit" class="space-y-5">
      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.name') }} <span class="text-red-500">*</span></label>
        <input v-model="form.name" type="text" required class="input" :placeholder="t('admin.channelMonitor.form.namePlaceholder')" />
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.provider') }} <span class="text-red-500">*</span></label>
        <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <button
            v-for="opt in providerOptions"
            :key="opt.value"
            type="button"
            :data-testid="`monitor-provider-${opt.value}`"
            :aria-pressed="form.provider === opt.value"
            class="flex items-center justify-center gap-2 rounded-lg border-2 px-3 py-2.5 text-sm font-medium transition-colors"
            :class="providerPickerClass(opt.value, form.provider === opt.value)"
            @click="selectProvider(opt.value)"
          >
            <ProviderIcon :provider="opt.value" :size="18" />
            <span>{{ opt.label }}</span>
          </button>
        </div>
      </div>

      <div v-if="supportsSelectableAPIMode(form.provider)" class="rounded-lg border border-blue-100 bg-blue-50/50 p-3 dark:border-blue-500/20 dark:bg-blue-500/10">
        <label class="input-label">{{ t('admin.channelMonitor.form.apiMode') }}</label>
        <div class="grid gap-3 sm:grid-cols-2">
          <button
            v-for="opt in apiModeOptions"
            :key="opt.value"
            type="button"
            :aria-pressed="form.api_mode === opt.value"
            class="rounded-lg border-2 px-3 py-2 text-left transition-colors"
            :class="apiModeButtonClass(opt.value)"
            @click="form.api_mode = opt.value"
          >
            <span class="block text-sm font-semibold">{{ opt.label }}</span>
            <span class="mt-0.5 block text-xs opacity-80">{{ opt.hint }}</span>
          </button>
        </div>
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.sourceMode') }}</label>
        <Select v-model="sourceModeSelectValue" :options="sourceModeOptions" />
        <p class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.form.sourceModeHint') }}</p>
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.endpoint') }} <span v-if="form.source_mode !== 'internal_gateway'" class="text-red-500">*</span></label>
        <div class="flex gap-2">
          <input v-model="form.endpoint" data-testid="monitor-endpoint" type="text" :required="form.source_mode !== 'internal_gateway'" :disabled="form.source_mode === 'internal_gateway'" class="input flex-1" :placeholder="t('admin.channelMonitor.form.endpointPlaceholder')" />
          <button type="button" @click="useCurrentDomain" class="btn btn-secondary whitespace-nowrap">
            {{ t('admin.channelMonitor.form.useCurrentDomain') }}
          </button>
        </div>
      </div>

      <div v-if="form.source_mode === 'internal_gateway'">
        <label class="input-label">{{ t('admin.channelMonitor.form.internalKey') }}</label>
        <Select
          v-model="internalKeySelectValue"
          :options="internalKeyOptions"
          :placeholder="t('admin.channelMonitor.form.internalKeyPlaceholder')"
          :disabled="internalKeysLoading"
        />
        <button
          v-if="canProvisionInternalKeys"
          type="button"
          class="btn btn-secondary mt-2"
          :disabled="internalKeysLoading || accountGroupsLoading || accountGroups.length === 0"
          @click="ensureMonitoringKeys"
        >
          {{ t('admin.channelMonitor.form.ensureInternalKeys') }}
        </button>
        <p class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.form.internalKeyHint') }}</p>
      </div>

      <div>
        <label v-if="form.source_mode !== 'internal_gateway'" class="input-label">
          {{ t('admin.channelMonitor.form.apiKey') }}<span v-if="!editing" class="text-red-500"> *</span>
        </label>
        <div v-if="form.source_mode !== 'internal_gateway'" class="flex gap-2">
          <input
            v-model="form.api_key"
            type="password"
            :required="!editing"
            class="input flex-1"
            :placeholder="editing ? t('admin.channelMonitor.form.apiKeyEditPlaceholder') : t('admin.channelMonitor.form.apiKeyPlaceholder')"
          />
          <button type="button" @click="openMyKeyPicker" class="btn btn-secondary whitespace-nowrap">
            {{ t('admin.channelMonitor.form.useMyKey') }}
          </button>
        </div>
        <p v-if="editing && editing.api_key_masked && form.source_mode !== 'internal_gateway'" class="mt-1 text-xs text-gray-400">{{ editing.api_key_masked }}</p>
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.primaryModel') }} <span class="text-red-500">*</span></label>
        <input
          v-model="form.primary_model"
          data-testid="monitor-primary-model"
          type="text"
          required
          class="input font-medium"
          :class="getPlatformTextClass(form.provider)"
          :placeholder="t('admin.channelMonitor.form.primaryModelPlaceholder')"
        />
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.extraModels') }}</label>
        <ModelTagInput
          :models="form.extra_models"
          :platform="form.provider"
          :placeholder="t('admin.channelMonitor.form.extraModelsPlaceholder')"
          @update:models="form.extra_models = $event"
        />
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.groupName') }}</label>
        <input v-model="form.group_name" type="text" class="input" :placeholder="t('admin.channelMonitor.form.groupNamePlaceholder')" />
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.accountGroup') }}</label>
        <Select
          v-model="accountGroupSelectValue"
          :options="accountGroupOptions"
          :placeholder="t('admin.channelMonitor.form.accountGroupPlaceholder')"
          :disabled="accountGroupsLoading"
        />
        <p class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.form.accountGroupHint') }}</p>
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.intervalSeconds') }} <span class="text-red-500">*</span></label>
        <input v-model.number="form.interval_seconds" type="number" min="15" max="3600" required class="input" />
        <p class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.form.intervalSecondsHint') }}</p>
      </div>

      <div v-if="form.api_mode === API_MODE_IMAGES">
        <label class="input-label">{{ t('admin.channelMonitor.form.imageRequestTimeoutSeconds') }} <span class="text-red-500">*</span></label>
        <input v-model.number="form.request_timeout_seconds" type="number" min="15" max="900" required class="input" />
        <p class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.form.imageRequestTimeoutSecondsHint') }}</p>
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.jitterSeconds') }}</label>
        <input v-model.number="form.jitter_seconds" type="number" min="0" :max="maxJitterSeconds" class="input" />
        <p class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.form.jitterSecondsHint') }}</p>
      </div>

      <div class="flex items-center justify-between">
        <label class="input-label mb-0">{{ t('admin.channelMonitor.form.enabled') }}</label>
        <Toggle v-model="form.enabled" />
      </div>

      <!-- ?????????? + ??? headers/body -->
      <details class="rounded-lg border border-gray-200 bg-gray-50/50 p-3 dark:border-dark-700 dark:bg-dark-900/30">
        <summary class="cursor-pointer text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.channelMonitor.advanced.section') }}
        </summary>
        <p class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.advanced.sectionHint') }}</p>

        <div class="mt-4 space-y-4">
          <div>
            <label class="input-label">{{ t('admin.channelMonitor.templateField.label') }}</label>
            <Select
              v-model="templateSelectValue"
              :options="templateOptions"
              :placeholder="t('admin.channelMonitor.templateField.placeholder')"
            />
            <p class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.templateField.applyHint') }}</p>
          </div>

          <MonitorAdvancedRequestConfig
            :provider="form.provider"
            :api-mode="form.api_mode"
            :extra-headers="form.extra_headers"
            :body-override-mode="form.body_override_mode"
            :body-override="form.body_override"
            @update:extra-headers="form.extra_headers = $event"
            @update:body-override-mode="form.body_override_mode = $event"
            @update:body-override="form.body_override = $event"
          />
        </div>
      </details>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="$emit('close')" type="button" class="btn btn-secondary">
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          form="channel-monitor-form"
          :disabled="submitting"
          class="btn btn-primary"
        >
          {{ submitting
            ? t('common.submitting')
            : editing ? t('common.update') : t('common.create') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <MonitorKeyPickerDialog
    :show="showKeyPicker"
    :loading="myKeysLoading"
    :keys="myActiveKeys"
    :provider="form.provider"
    :user-group-rates="userGroupRates"
    @close="showKeyPicker = false"
    @pick="pickMyKey"
  />
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { adminAPI } from '@/api/admin'
import { keysAPI } from '@/api/keys'
import { userGroupsAPI } from '@/api/groups'
import type {
	BodyOverrideMode,
	ChannelMonitor,
	CreateParams,
	APIMode,
	Provider,
	UpdateParams,
	MonitorSourceMode,
	InternalMonitorKey,
} from '@/api/admin/channelMonitor'
import type { ChannelMonitorTemplate } from '@/api/admin/channelMonitorTemplate'
import type { AdminGroup, ApiKey } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import Select from '@/components/common/Select.vue'
import ModelTagInput from '@/components/admin/channel/ModelTagInput.vue'
import { getPlatformTextClass } from '@/components/admin/channel/types'
import MonitorKeyPickerDialog from '@/components/admin/monitor/MonitorKeyPickerDialog.vue'
import MonitorAdvancedRequestConfig from '@/components/admin/monitor/MonitorAdvancedRequestConfig.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import {
  PROVIDER_OPENAI,
  PROVIDER_ANTHROPIC,
  PROVIDER_GEMINI,
  PROVIDER_GROK,
  PROVIDER_ANTIGRAVITY,
  PROVIDER_DEEPSEEK,
  PROVIDER_KIMI,
  PROVIDER_GLM,
  PROVIDER_QWEN,
  PROVIDER_MINIMAX,
  PROVIDER_MIMO,
  PROVIDER_HUNYUAN,
  API_MODE_CHAT_COMPLETIONS,
  API_MODE_RESPONSES,
  API_MODE_MODELS,
  API_MODE_IMAGES,
  DEFAULT_GROK_ENDPOINT,
  DEFAULT_GROK_MODEL,
  DEFAULT_INTERVAL_SECONDS,
  supportsSelectableAPIMode,
} from '@/constants/channelMonitor'

const props = defineProps<{
  show: boolean
  monitor: ChannelMonitor | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'saved'): void
}>()

const { t } = useI18n()
const appStore = useAppStore()
const { providerPickerClass } = useChannelMonitorFormat()
const DEFAULT_IMAGE_MONITOR_INTERVAL_SECONDS = 30 * 60
const DEFAULT_TEXT_REQUEST_TIMEOUT_SECONDS = 45
const DEFAULT_IMAGE_REQUEST_TIMEOUT_SECONDS = 5 * 60

// System-configured default interval for new monitors. Falls back to the static
// constant when public settings haven't loaded yet or store the legacy 0 value.
const systemDefaultInterval = computed<number>(() => {
  const configured = appStore.cachedPublicSettings?.channel_monitor_default_interval_seconds
  return configured && configured > 0 ? configured : DEFAULT_INTERVAL_SECONDS
})

// editing is true when we have an existing monitor
const editing = computed<ChannelMonitor | null>(() => props.monitor)

const submitting = ref(false)

// API key picker
const showKeyPicker = ref(false)
const myKeysLoading = ref(false)
const myActiveKeys = ref<ApiKey[]>([])
const userGroupRates = ref<Record<number, number>>({})

interface MonitorForm {
  name: string
  provider: Provider
  api_mode: APIMode
  endpoint: string
  api_key: string
  source_mode: MonitorSourceMode
  internal_api_key_id: number | null
  internal_group_id: number | null
  primary_model: string
  extra_models: string[]
  group_name: string
  account_group_id: number | null
  interval_seconds: number
  jitter_seconds: number
  request_timeout_seconds: number
  enabled: boolean
  // ??????
  template_id: number | null
  extra_headers: Record<string, string>
  body_override_mode: BodyOverrideMode
  body_override: Record<string, unknown> | null
}

const form = reactive<MonitorForm>({
  name: '',
  provider: PROVIDER_ANTHROPIC,
  api_mode: API_MODE_CHAT_COMPLETIONS,
  endpoint: '',
  api_key: '',
  source_mode: 'direct_upstream',
  internal_api_key_id: null,
  internal_group_id: null,
  primary_model: '',
  extra_models: [],
  group_name: '',
  account_group_id: null,
  interval_seconds: systemDefaultInterval.value,
  jitter_seconds: 0,
  request_timeout_seconds: DEFAULT_TEXT_REQUEST_TIMEOUT_SECONDS,
  enabled: true,
  template_id: null,
  extra_headers: {},
  body_override_mode: 'off',
  body_override: null,
})

// jitter ??????????interval - jitter ?????????? 15 ??
const maxJitterSeconds = computed<number>(() => Math.max(0, (form.interval_seconds || 0) - 15))

let suppressFormWatchers = false

// ????????? dialog ?????? cache?? provider / api mode ????
const templatesCache = ref<ChannelMonitorTemplate[]>([])
const templatesLoading = ref(false)
const accountGroupsCache = new Map<Provider, AdminGroup[]>()
const accountGroups = ref<AdminGroup[]>([])
const accountGroupsLoading = ref(false)
const internalKeys = ref<InternalMonitorKey[]>([])
const internalKeysLoading = ref(false)
const canProvisionInternalKeys = computed(() =>
  [PROVIDER_DEEPSEEK, PROVIDER_KIMI, PROVIDER_GLM, PROVIDER_QWEN, PROVIDER_MINIMAX, PROVIDER_MIMO, PROVIDER_HUNYUAN]
    .includes(form.provider),
)

const sourceModeOptions = computed(() => [
  { value: 'direct_upstream', label: t('admin.channelMonitor.form.sourceModeDirect') },
  { value: 'internal_gateway', label: t('admin.channelMonitor.form.sourceModeInternal') },
])

const sourceModeSelectValue = computed<string>({
  get: () => form.source_mode,
  set: (value) => {
    form.source_mode = value === 'internal_gateway' ? 'internal_gateway' : 'direct_upstream'
    if (form.source_mode === 'internal_gateway') {
      form.endpoint = ''
      form.api_key = ''
      form.api_mode = API_MODE_CHAT_COMPLETIONS
      void loadInternalKeys()
    } else {
      form.internal_api_key_id = null
      form.internal_group_id = null
    }
  },
})

const internalKeyOptions = computed(() => [
  { value: '', label: t('admin.channelMonitor.form.internalKeyPlaceholder') },
  ...internalKeys.value
    .filter((key) => key.provider === form.provider || (form.provider === 'openai' && key.provider === 'openai'))
    .map((key) => ({ value: String(key.id), label: `${key.name} ? ${key.group_name} (#${key.group_id})` })),
])

const internalKeySelectValue = computed<string>({
  get: () => (form.internal_api_key_id == null ? '' : String(form.internal_api_key_id)),
  set: (raw) => {
    const key = internalKeys.value.find((item) => String(item.id) === raw)
    form.internal_api_key_id = key?.id ?? null
    form.internal_group_id = key?.group_id ?? null
    if (key) form.provider = key.provider as Provider
  },
})

async function loadInternalKeys() {
  if (internalKeys.value.length > 0 || internalKeysLoading.value) return
  internalKeysLoading.value = true
  try {
    const result = await adminAPI.channelMonitor.listInternalKeys()
    internalKeys.value = result.items || []
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.form.internalKeyLoadFailed')))
  } finally {
    internalKeysLoading.value = false
  }
}

const templateOptions = computed(() => {
  const items = templatesCache.value.filter((t) => {
    if (t.provider !== form.provider) return false
    if (!supportsSelectableAPIMode(form.provider)) return true
    return normalizeAPIMode(t.api_mode) === form.api_mode
  })
  return [
    { value: '', label: t('admin.channelMonitor.templateField.none') },
    ...items.map((t) => ({ value: String(t.id), label: templateOptionLabel(t) })),
  ]
})

async function loadTemplates() {
  if (templatesCache.value.length > 0) return
  templatesLoading.value = true
  try {
    const { items } = await adminAPI.channelMonitorTemplate.list()
    templatesCache.value = items
  } catch (err: unknown) {
    // ??????????????????????
    console.warn('load monitor templates failed', err)
  } finally {
    templatesLoading.value = false
  }
}

// ???????value ? string?Select ????????? number | null ???
const templateSelectValue = computed<string>({
  get: () => (form.template_id == null ? '' : String(form.template_id)),
  set: (raw: string) => {
    if (raw === '') {
      form.template_id = null
      return
    }
    const id = Number(raw)
    if (!Number.isFinite(id)) return
    form.template_id = id
    // ???? = ????
    const tpl = templatesCache.value.find((t) => t.id === id)
    if (tpl) {
      suppressFormWatchers = true
      form.api_mode = normalizeAPIMode(tpl.api_mode)
      form.template_id = id
      form.extra_headers = { ...(tpl.extra_headers || {}) }
      form.body_override_mode = tpl.body_override_mode
      form.body_override = tpl.body_override ? { ...tpl.body_override } : null
      suppressFormWatchers = false
    }
  },
})

const apiModeOptions = computed<{ value: APIMode; label: string; hint: string }[]>(() => [
  {
    value: API_MODE_CHAT_COMPLETIONS,
    label: t('admin.channelMonitor.form.apiModeChatCompletions'),
    hint: t('admin.channelMonitor.form.apiModeChatCompletionsHint'),
  },
  {
    value: API_MODE_RESPONSES,
    label: t('admin.channelMonitor.form.apiModeResponses'),
    hint: t('admin.channelMonitor.form.apiModeResponsesHint'),
  },
  {
    value: API_MODE_MODELS,
    label: t('admin.channelMonitor.form.apiModeModels'),
    hint: t('admin.channelMonitor.form.apiModeModelsHint'),
  },
  {
    value: API_MODE_IMAGES,
    label: t('admin.channelMonitor.form.apiModeImages'),
    hint: t('admin.channelMonitor.form.apiModeImagesHint'),
  },
])

function normalizeAPIMode(mode: APIMode | undefined | null): APIMode {
  if (mode === API_MODE_RESPONSES) return API_MODE_RESPONSES
  if (mode === API_MODE_MODELS) return API_MODE_MODELS
  if (mode === API_MODE_IMAGES) return API_MODE_IMAGES
  return API_MODE_CHAT_COMPLETIONS
}

function apiModeButtonClass(mode: APIMode): string {
  const active = form.api_mode === mode
  if (active) {
    return 'border-primary-500 bg-white text-primary-700 shadow-sm dark:border-primary-400 dark:bg-primary-500/15 dark:text-primary-300'
  }
  return 'border-blue-100 bg-white/70 text-gray-600 hover:border-primary-300 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-400'
}

function templateOptionLabel(tpl: ChannelMonitorTemplate): string {
  if (!supportsSelectableAPIMode(tpl.provider)) return tpl.name
  const labelKey = normalizeAPIMode(tpl.api_mode) === API_MODE_RESPONSES
    ? 'admin.channelMonitor.form.apiModeResponses'
    : normalizeAPIMode(tpl.api_mode) === API_MODE_MODELS
      ? 'admin.channelMonitor.form.apiModeModels'
      : normalizeAPIMode(tpl.api_mode) === API_MODE_IMAGES
        ? 'admin.channelMonitor.form.apiModeImages'
        : 'admin.channelMonitor.form.apiModeChatCompletions'
  return `${tpl.name} ? ${t(labelKey)}`
}

function clearRequestSnapshot() {
  form.template_id = null
  form.extra_headers = {}
  form.body_override_mode = 'off'
  form.body_override = null
}

interface ProviderOption {
  value: Provider
  label: string
}

const providerOptions = computed<ProviderOption[]>(() => [
  { value: PROVIDER_ANTHROPIC, label: t('monitorCommon.providers.anthropic') },
  { value: PROVIDER_OPENAI, label: t('monitorCommon.providers.openai') },
  { value: PROVIDER_GEMINI, label: t('monitorCommon.providers.gemini') },
  { value: PROVIDER_GROK, label: t('monitorCommon.providers.grok') },
  { value: PROVIDER_ANTIGRAVITY, label: t('monitorCommon.providers.antigravity') },
  { value: PROVIDER_DEEPSEEK, label: t('monitorCommon.providers.deepseek') },
  { value: PROVIDER_KIMI, label: t('monitorCommon.providers.kimi') },
  { value: PROVIDER_GLM, label: t('monitorCommon.providers.glm') },
  { value: PROVIDER_QWEN, label: t('monitorCommon.providers.qwen') },
  { value: PROVIDER_MINIMAX, label: t('monitorCommon.providers.minimax') },
  { value: PROVIDER_MIMO, label: t('monitorCommon.providers.mimo') },
  { value: PROVIDER_HUNYUAN, label: t('monitorCommon.providers.hunyuan') },
])

const accountGroupOptions = computed(() => [
  { value: '', label: t('admin.channelMonitor.form.accountGroupNone') },
  ...accountGroups.value.map((group) => ({
    value: String(group.id),
    label: `${group.name} (#${group.id})`,
  })),
])

const accountGroupSelectValue = computed<string>({
  get: () => (form.account_group_id == null ? '' : String(form.account_group_id)),
  set: (raw: string) => {
    if (!raw) {
      form.account_group_id = null
      return
    }
    const id = Number(raw)
    form.account_group_id = Number.isSafeInteger(id) && id > 0 ? id : null
  },
})

async function loadAccountGroups() {
  const provider = form.provider
  const cached = accountGroupsCache.get(provider)
  if (cached) {
    accountGroups.value = cached
    return
  }
  accountGroupsLoading.value = true
  try {
    const groups = await adminAPI.groups.getByPlatform(provider)
    accountGroupsCache.set(provider, groups)
    if (form.provider === provider) {
      accountGroups.value = groups
    }
  } catch (err: unknown) {
    console.warn('load monitor account groups failed', err)
    if (form.provider === provider) {
      accountGroups.value = []
    }
  } finally {
    accountGroupsLoading.value = false
  }
}

async function ensureMonitoringKeys() {
  const groupIds = accountGroups.value.map(group => group.id).filter(id => Number.isSafeInteger(id) && id > 0)
  if (groupIds.length === 0) {
    appStore.showError(t('admin.channelMonitor.form.noMonitoringGroups'))
    return
  }
  internalKeysLoading.value = true
  try {
    const result = await adminAPI.channelMonitor.ensureInternalKeys(groupIds)
    const created = result.items.filter(item => item.created).length
    internalKeys.value = (await adminAPI.channelMonitor.listInternalKeys()).items
    appStore.showSuccess(t('admin.channelMonitor.form.ensureInternalKeysSuccess', { count: created }))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.form.ensureInternalKeysFailed')))
  } finally {
    internalKeysLoading.value = false
  }
}

function selectProvider(provider: Provider) {
  if (form.provider === provider) return
  const previousProvider = form.provider
  const clearGrokEndpoint =
    previousProvider === PROVIDER_GROK && form.endpoint === DEFAULT_GROK_ENDPOINT
  const clearGrokModel =
    previousProvider === PROVIDER_GROK && form.primary_model === DEFAULT_GROK_MODEL
  form.provider = provider
  if (provider === PROVIDER_GROK) {
    if (!form.endpoint.trim()) form.endpoint = DEFAULT_GROK_ENDPOINT
    if (!form.primary_model.trim()) form.primary_model = DEFAULT_GROK_MODEL
    return
  }
  if (clearGrokEndpoint) form.endpoint = ''
  if (clearGrokModel) form.primary_model = ''
}

// Clear api_key whenever provider changes to avoid cross-provider key mismatch.
// Editing mode loads api_key='' via loadFromMonitor and only sets it on user
// typing, so clearing on provider change is always a safe no-op until the user
// picks a new key.
// ???? template_id???? provider ???????????
watch(() => form.provider, () => {
  if (suppressFormWatchers) return
  form.api_key = ''
  form.internal_api_key_id = null
  form.internal_group_id = null
  form.account_group_id = null
  if (!supportsSelectableAPIMode(form.provider)) {
    form.api_mode = API_MODE_CHAT_COMPLETIONS
  }
  clearRequestSnapshot()
  void loadAccountGroups()
}, { flush: 'sync' })

watch(() => form.api_mode, () => {
  if (suppressFormWatchers) return
  if (supportsSelectableAPIMode(form.provider)) {
    clearRequestSnapshot()
  }
}, { flush: 'sync' })

function defaultIntervalForMode(mode: APIMode): number {
  return mode === API_MODE_IMAGES
    ? DEFAULT_IMAGE_MONITOR_INTERVAL_SECONDS
    : systemDefaultInterval.value
}

function defaultRequestTimeoutForMode(mode: APIMode): number {
  return mode === API_MODE_IMAGES
    ? DEFAULT_IMAGE_REQUEST_TIMEOUT_SECONDS
    : DEFAULT_TEXT_REQUEST_TIMEOUT_SECONDS
}

watch(() => form.api_mode, (mode, previousMode) => {
  if (suppressFormWatchers) return
  if (form.interval_seconds === defaultIntervalForMode(previousMode)) {
    form.interval_seconds = defaultIntervalForMode(mode)
  }
  if (form.request_timeout_seconds === defaultRequestTimeoutForMode(previousMode)) {
    form.request_timeout_seconds = defaultRequestTimeoutForMode(mode)
  }
}, { flush: 'sync' })

function resetForm() {
  suppressFormWatchers = true
  form.name = ''
  form.provider = PROVIDER_ANTHROPIC
  form.api_mode = API_MODE_CHAT_COMPLETIONS
  form.endpoint = ''
  form.api_key = ''
  form.source_mode = 'direct_upstream'
  form.internal_api_key_id = null
  form.internal_group_id = null
  form.primary_model = ''
  form.extra_models = []
  form.group_name = ''
  form.account_group_id = null
  form.interval_seconds = defaultIntervalForMode(form.api_mode)
  form.jitter_seconds = 0
  form.request_timeout_seconds = defaultRequestTimeoutForMode(form.api_mode)
  form.enabled = true
  form.template_id = null
  form.extra_headers = {}
  form.body_override_mode = 'off'
  form.body_override = null
  suppressFormWatchers = false
}

function loadFromMonitor(m: ChannelMonitor) {
  suppressFormWatchers = true
  form.name = m.name
  form.provider = m.provider
  form.api_mode = normalizeAPIMode(m.api_mode)
  form.endpoint = m.endpoint
  form.api_key = ''
  form.source_mode = m.source_mode || 'direct_upstream'
  form.internal_api_key_id = m.internal_api_key_id ?? null
  form.internal_group_id = m.internal_group_id ?? null
  form.primary_model = m.primary_model
  form.extra_models = [...(m.extra_models || [])]
  form.group_name = m.group_name || ''
  form.account_group_id = m.account_group_id ?? null
  form.interval_seconds = m.interval_seconds || defaultIntervalForMode(form.api_mode)
  form.jitter_seconds = m.jitter_seconds || 0
  form.request_timeout_seconds = m.request_timeout_seconds || defaultRequestTimeoutForMode(form.api_mode)
  form.enabled = m.enabled
  form.template_id = m.template_id ?? null
  form.extra_headers = { ...(m.extra_headers || {}) }
  form.body_override_mode = m.body_override_mode || 'off'
  form.body_override = m.body_override ? { ...m.body_override } : null
  suppressFormWatchers = false
}

// Re-sync form whenever the dialog is opened or the target monitor changes.
// ?????????cache ??????????
watch(
  () => [props.show, props.monitor] as const,
  ([show, m]) => {
    if (!show) return
    void loadTemplates()
    if (m) loadFromMonitor(m)
    else resetForm()
    void loadAccountGroups()
    if (form.source_mode === 'internal_gateway') void loadInternalKeys()
  },
  { immediate: true },
)

function useCurrentDomain() {
  form.endpoint = window.location.origin
}

async function openMyKeyPicker() {
  showKeyPicker.value = true
  if (myActiveKeys.value.length > 0) return
  myKeysLoading.value = true
  try {
    const [res, rates] = await Promise.all([
      keysAPI.list(1, 100, { status: 'active' }),
      userGroupsAPI.getUserGroupRates(),
    ])
    const items = res.items || []
    const now = Date.now()
    myActiveKeys.value = items.filter(k => {
      if (k.status !== 'active') return false
      if (!k.expires_at) return true
      return new Date(k.expires_at).getTime() > now
    })
    userGroupRates.value = rates
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.form.noActiveKey')))
  } finally {
    myKeysLoading.value = false
  }
}

function pickMyKey(k: ApiKey) {
  form.api_key = k.key
  showKeyPicker.value = false
}

function buildPayload(): CreateParams {
  return {
    name: form.name.trim(),
    provider: form.provider,
    api_mode: supportsSelectableAPIMode(form.provider) ? form.api_mode : API_MODE_CHAT_COMPLETIONS,
    endpoint: form.endpoint.trim(),
    api_key: form.api_key.trim(),
    source_mode: form.source_mode,
    internal_api_key_id: form.internal_api_key_id,
    internal_group_id: form.internal_group_id,
    primary_model: form.primary_model.trim(),
    extra_models: form.extra_models,
    group_name: form.group_name.trim(),
    account_group_id: form.account_group_id,
    enabled: form.enabled,
    interval_seconds: form.interval_seconds,
    jitter_seconds: form.jitter_seconds || 0,
    request_timeout_seconds: form.request_timeout_seconds,
    template_id: form.template_id,
    extra_headers: form.extra_headers,
    body_override_mode: form.body_override_mode,
    body_override: form.body_override,
  }
}

async function handleSubmit() {
  if (submitting.value) return
  if (!form.name.trim()) {
    appStore.showError(t('admin.channelMonitor.nameRequired'))
    return
  }
  if (!form.primary_model.trim()) {
    appStore.showError(t('admin.channelMonitor.primaryModelRequired'))
    return
  }

  submitting.value = true
  try {
    const target = editing.value
    if (target) {
      const { api_key, ...rest } = buildPayload()
      const req: UpdateParams = { ...rest }
      // Only send api_key if user typed a new value
      if (api_key) req.api_key = api_key
      // template_id=null ? clear_template=true ?????????pointer ???
      if (form.template_id == null) {
        req.clear_template = true
        delete req.template_id
      }
      if (form.account_group_id == null) {
        req.clear_account_group = true
        delete req.account_group_id
      }
      await adminAPI.channelMonitor.update(target.id, req)
      appStore.showSuccess(t('admin.channelMonitor.updateSuccess'))
    } else {
      await adminAPI.channelMonitor.create(buildPayload())
      appStore.showSuccess(t('admin.channelMonitor.createSuccess'))
    }
    emit('saved')
    emit('close')
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    submitting.value = false
  }
}
</script>
