<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
import Icon from '@/components/icons/Icon.vue'
import type { CustomEndpoint } from '@/types'

type ProbeStatus = 'unknown' | 'checking' | 'online' | 'degraded' | 'offline'

interface EndpointItem {
  key: string
  name: string
  endpoint: string
  description: string
  isDefault: boolean
}

interface ProbeState {
  status: ProbeStatus
  latencyMs: number | null
  checkedAt: number | null
  error: string
}

const props = withDefaults(defineProps<{
  apiBaseUrl: string
  customEndpoints: CustomEndpoint[]
  probeIntervalSeconds?: number
}>(), {
  probeIntervalSeconds: 5,
})

const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const copiedEndpoint = ref<string | null>(null)
const probeStates = reactive<Record<string, ProbeState>>({})

let copiedResetTimer: number | undefined
let probeTimer: number | undefined
const activeControllers = new Set<AbortController>()

function defaultEndpoint(): string {
  const configured = props.apiBaseUrl.trim()
  if (configured) return configured.replace(/\/+$/, '')
  if (typeof window !== 'undefined') return window.location.origin.replace(/\/+$/, '')
  return ''
}

const allEndpoints = computed<EndpointItem[]>(() => {
  const items: EndpointItem[] = []
  const base = defaultEndpoint()
  if (base) {
    items.push({
      key: `default:${base}`,
      name: t('keys.endpoints.title'),
      endpoint: base,
      description: '',
      isDefault: true,
    })
  }
  for (const ep of props.customEndpoints) {
    const endpoint = ep.endpoint.trim().replace(/\/+$/, '')
    if (!endpoint) continue
    items.push({
      key: `custom:${ep.name}:${endpoint}`,
      name: ep.name,
      endpoint,
      description: ep.description,
      isDefault: false,
    })
  }
  return items
})

const normalizedProbeIntervalSeconds = computed(() => {
  const raw = Number(props.probeIntervalSeconds)
  if (!Number.isFinite(raw)) return 5
  if (raw <= 0) return 0
  return Math.min(300, Math.max(3, Math.floor(raw)))
})

function ensureProbeState(item: EndpointItem): ProbeState {
  if (!probeStates[item.key]) {
    probeStates[item.key] = {
      status: 'unknown',
      latencyMs: null,
      checkedAt: null,
      error: '',
    }
  }
  return probeStates[item.key]
}

function endpointRoot(endpoint: string): string {
  let url = endpoint.trim().replace(/\/+$/, '')
  url = url.replace(/\/(?:api\/)?v1(?:beta)?$/i, '')
  return url || endpoint.trim().replace(/\/+$/, '')
}

function healthUrl(endpoint: string): string {
  return `${endpointRoot(endpoint)}/health`
}

function speedTestUrl(endpoint: string): string {
  return `https://www.tcptest.cn/http/${encodeURIComponent(endpoint)}`
}

function clearProbeTimer() {
  if (probeTimer !== undefined) {
    window.clearInterval(probeTimer)
    probeTimer = undefined
  }
}

function abortActiveProbes() {
  for (const controller of activeControllers) {
    controller.abort()
  }
  activeControllers.clear()
}

function statusClass(status: ProbeStatus): string {
  switch (status) {
    case 'online':
      return 'bg-emerald-400 shadow-emerald-500/40'
    case 'degraded':
      return 'bg-amber-400 shadow-amber-500/40'
    case 'offline':
      return 'bg-red-400 shadow-red-500/40'
    case 'checking':
      return 'bg-sky-400 shadow-sky-500/40'
    default:
      return 'bg-gray-400 shadow-gray-500/30'
  }
}

function latencyClass(state: ProbeState): string {
  if (state.status === 'offline') return 'text-red-500 dark:text-red-300'
  if (state.status === 'degraded') return 'text-amber-500 dark:text-amber-300'
  if (state.status === 'online') return 'text-emerald-600 dark:text-emerald-300'
  return 'text-gray-500 dark:text-gray-400'
}

function latencyText(state: ProbeState): string {
  if (state.status === 'checking') return t('keys.endpoints.checking')
  if (state.status === 'offline') return t('keys.endpoints.offline')
  if (state.latencyMs !== null) return `${state.latencyMs}ms`
  return t('keys.endpoints.unknown')
}

function lastCheckedText(state: ProbeState): string {
  if (!state.checkedAt) return t('keys.endpoints.notChecked')
  return t('keys.endpoints.lastCheckedAt', {
    time: new Date(state.checkedAt).toLocaleTimeString(),
  })
}

function tooltipHint(item: EndpointItem): string {
  const state = ensureProbeState(item)
  const copyHint = copiedEndpoint.value === item.endpoint
    ? t('keys.endpoints.copiedHint')
    : t('keys.endpoints.clickToCopy')
  const statusHint = state.error || lastCheckedText(state)
  const description = item.description.trim()
  return description
    ? `${description} | ${copyHint} | ${statusHint}`
    : `${copyHint} | ${statusHint}`
}

async function copy(url: string) {
  const success = await copyToClipboard(url, t('keys.endpoints.copied'))
  if (!success) return

  copiedEndpoint.value = url
  if (copiedResetTimer !== undefined) {
    window.clearTimeout(copiedResetTimer)
  }
  copiedResetTimer = window.setTimeout(() => {
    if (copiedEndpoint.value === url) {
      copiedEndpoint.value = null
    }
  }, 1800)
}

async function probeEndpoint(item: EndpointItem) {
  const state = ensureProbeState(item)
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), 4000)
  activeControllers.add(controller)
  state.status = 'checking'
  state.error = ''

  const start = performance.now()
  try {
    const response = await fetch(healthUrl(item.endpoint), {
      cache: 'no-store',
      signal: controller.signal,
    })
    const elapsed = Math.max(1, Math.round(performance.now() - start))
    state.latencyMs = elapsed
    state.checkedAt = Date.now()
    state.status = response.ok ? (elapsed >= 1200 ? 'degraded' : 'online') : 'offline'
    if (!response.ok) {
      state.error = t('keys.endpoints.httpError', { status: response.status })
    }
  } catch (error) {
    state.latencyMs = null
    state.checkedAt = Date.now()
    state.status = 'offline'
    state.error = error instanceof DOMException && error.name === 'AbortError'
      ? t('keys.endpoints.timeout')
      : t('keys.endpoints.requestFailed')
  } finally {
    window.clearTimeout(timeout)
    activeControllers.delete(controller)
  }
}

async function probeAllEndpoints() {
  await Promise.allSettled(allEndpoints.value.map((item) => probeEndpoint(item)))
}

function restartAutoProbe() {
  clearProbeTimer()
  abortActiveProbes()
  if (allEndpoints.value.length === 0) return
  void probeAllEndpoints()
  const intervalSeconds = normalizedProbeIntervalSeconds.value
  if (intervalSeconds > 0) {
    probeTimer = window.setInterval(() => {
      void probeAllEndpoints()
    }, intervalSeconds * 1000)
  }
}

watch(
  () => [
    props.apiBaseUrl,
    JSON.stringify(props.customEndpoints),
    normalizedProbeIntervalSeconds.value,
  ],
  restartAutoProbe,
)

onMounted(restartAutoProbe)

onBeforeUnmount(() => {
  clearProbeTimer()
  abortActiveProbes()
  if (copiedResetTimer !== undefined) {
    window.clearTimeout(copiedResetTimer)
  }
})
</script>

<template>
  <div v-if="allEndpoints.length > 0" class="flex flex-wrap items-center gap-2">
    <div
      v-for="item in allEndpoints"
      :key="item.key"
      class="flex min-h-[2.5rem] max-w-full items-center gap-2 rounded-lg border border-gray-200 bg-white/90 px-3 py-2 text-xs shadow-sm transition-colors dark:border-dark-600 dark:bg-dark-800/90"
    >
      <span
        class="h-2.5 w-2.5 shrink-0 rounded-full shadow-[0_0_0_3px]"
        :class="statusClass(ensureProbeState(item).status)"
      ></span>
      <span class="shrink-0 font-semibold text-gray-700 dark:text-gray-200">
        {{ item.name }}
      </span>
      <span v-if="item.description" class="sr-only">{{ item.description }}</span>
      <span
        v-if="item.isDefault"
        class="shrink-0 rounded bg-primary-50 px-1.5 py-0.5 text-[10px] font-medium leading-tight text-primary-600 dark:bg-primary-900/30 dark:text-primary-300"
      >
        {{ t('keys.endpoints.default') }}
      </span>
      <span
        class="shrink-0 font-mono font-semibold tabular-nums"
        :class="latencyClass(ensureProbeState(item))"
        :title="lastCheckedText(ensureProbeState(item))"
      >
        {{ latencyText(ensureProbeState(item)) }}
      </span>
      <span class="h-4 w-px shrink-0 bg-gray-200 dark:bg-dark-600"></span>
      <button
        type="button"
        role="button"
        class="min-w-0 truncate font-mono text-gray-500 transition-colors hover:text-primary-600 focus:text-primary-600 focus:outline-none dark:text-gray-400 dark:hover:text-primary-300 dark:focus:text-primary-300"
        :title="tooltipHint(item)"
        @click="copy(item.endpoint)"
      >
        {{ item.endpoint }}
      </button>
      <button
        type="button"
        class="shrink-0 rounded p-1 transition-colors"
        :class="copiedEndpoint === item.endpoint
          ? 'text-emerald-500 dark:text-emerald-300'
          : 'text-gray-400 hover:bg-gray-100 hover:text-primary-600 dark:text-gray-500 dark:hover:bg-dark-700 dark:hover:text-primary-300'"
        :aria-label="copiedEndpoint === item.endpoint ? t('keys.endpoints.copiedHint') : t('keys.endpoints.clickToCopy')"
        @click="copy(item.endpoint)"
      >
        <Icon v-if="copiedEndpoint === item.endpoint" name="check" size="xs" :stroke-width="2" />
        <Icon v-else name="copy" size="xs" :stroke-width="2" />
      </button>
      <a
        :href="speedTestUrl(item.endpoint)"
        target="_blank"
        rel="noopener noreferrer"
        class="shrink-0 rounded p-1 text-gray-400 transition-colors hover:bg-gray-100 hover:text-amber-500 dark:text-gray-500 dark:hover:bg-dark-700 dark:hover:text-amber-300"
        :title="t('keys.endpoints.speedTest')"
      >
        <Icon name="bolt" size="xs" :stroke-width="2" />
      </a>
      <button
        type="button"
        class="shrink-0 rounded p-1 text-gray-400 transition-colors hover:bg-gray-100 hover:text-primary-600 disabled:cursor-wait disabled:opacity-60 dark:text-gray-500 dark:hover:bg-dark-700 dark:hover:text-primary-300"
        :disabled="ensureProbeState(item).status === 'checking'"
        :title="t('keys.endpoints.refresh')"
        @click="probeEndpoint(item)"
      >
        <Icon
          name="refresh"
          size="xs"
          :class="ensureProbeState(item).status === 'checking' ? 'animate-spin' : ''"
          :stroke-width="2"
        />
      </button>
    </div>
  </div>
</template>
