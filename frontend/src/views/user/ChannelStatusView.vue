<template>
  <AppLayout>
    <MonitorHero
      :overall-status="overallStatus"
      :interval-seconds="DEFAULT_INTERVAL_SECONDS"
      :window="currentWindow"
      :loading="loading"
      :auto-refresh="autoRefresh"
      @update:window="handleWindowChange"
      @refresh="manualReload"
    />

    <MonitorCardGrid
      :items="channelGroups"
      :window="currentWindow"
      :countdown-seconds="countdown"
      :loading="loading"
      :detail-cache="detailCache"
      :image-urls="imageUrls"
      @card-click="openDetail"
    />

    <MonitorDetailDialog
      :show="showDetail"
      :monitor-ids="detailTarget?.monitorIds ?? []"
      :title="detailTitle"
      @close="closeDetail"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  list as listChannelMonitorViews,
  status as fetchChannelMonitorDetail,
  latestImage as fetchChannelMonitorLatestImage,
  type UserMonitorView,
  type UserMonitorDetail,
} from '@/api/channelMonitor'
import {
  groupChannelMonitorViews,
  groupedChannelStatus,
  type GroupedChannelStatus,
} from '@/utils/channelMonitorGrouping'
import AppLayout from '@/components/layout/AppLayout.vue'
import MonitorHero, {
  type MonitorWindow,
  type OverallStatus,
} from '@/components/user/monitor/MonitorHero.vue'
import MonitorCardGrid from '@/components/user/monitor/MonitorCardGrid.vue'
import MonitorDetailDialog from '@/components/user/MonitorDetailDialog.vue'
import { DEFAULT_INTERVAL_SECONDS, STATUS_OPERATIONAL } from '@/constants/channelMonitor'
import { useAutoRefresh } from '@/composables/useAutoRefresh'

const { t } = useI18n()
const appStore = useAppStore()

// ── State ──
const items = ref<UserMonitorView[]>([])
const loading = ref(false)
const currentWindow = ref<MonitorWindow>('7d')
const detailCache = reactive<Record<number, UserMonitorDetail>>({})
const imageUrls = reactive<Record<number, string>>({})
const showDetail = ref(false)
const detailTarget = ref<GroupedChannelStatus | null>(null)

let abortController: AbortController | null = null
let imageLoadGeneration = 0

const autoRefresh = useAutoRefresh({
  storageKey: 'channel-status-auto-refresh',
  intervals: [30, 60, 120] as const,
  defaultInterval: DEFAULT_INTERVAL_SECONDS,
  onRefresh: () => reload(true),
  shouldPause: () => document.hidden || loading.value,
})
const countdown = autoRefresh.countdown

// ── Computed ──
const overallStatus = computed<OverallStatus>(() => {
  if (channelGroups.value.length === 0) return 'operational'
  for (const group of channelGroups.value) {
    if (groupedChannelStatus(group) !== STATUS_OPERATIONAL) return 'degraded'
  }
  return 'operational'
})

const channelGroups = computed(() => groupChannelMonitorViews(items.value))

const detailTitle = computed(() => {
  return detailTarget.value?.name || t('channelStatus.detailTitle')
})

// ── Loaders ──
async function reload(silent = false) {
  if (abortController) abortController.abort()
  const ctrl = new AbortController()
  abortController = ctrl
  if (!silent) loading.value = true
  try {
    const res = await listChannelMonitorViews({ signal: ctrl.signal })
    if (ctrl.signal.aborted || abortController !== ctrl) return
    items.value = res.items || []
    void loadLatestImages(items.value)
  } catch (err: unknown) {
    const e = err as { name?: string; code?: string }
    if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.loadError')))
  } finally {
    if (abortController === ctrl) {
      if (!silent) loading.value = false
      countdown.value = DEFAULT_INTERVAL_SECONDS
      abortController = null
    }
  }
}

async function loadLatestImages(rows: UserMonitorView[]) {
  const generation = ++imageLoadGeneration
  const imageMonitorIDs = new Set(
    rows.filter(row => row.api_mode === 'images').map(row => row.id)
  )

  for (const [id, url] of Object.entries(imageUrls)) {
    if (!imageMonitorIDs.has(Number(id))) {
      URL.revokeObjectURL(url)
      delete imageUrls[Number(id)]
    }
  }

  const loaded = await Promise.all(
    rows
      .filter(row => row.api_mode === 'images')
      .map(async row => {
        try {
          const blob = await fetchChannelMonitorLatestImage(row.id)
          return { id: row.id, url: URL.createObjectURL(blob) }
        } catch (err: unknown) {
          const status = (err as { status?: number })?.status
          if (status !== 404) {
            appStore.showError(extractApiErrorMessage(err, t('channelStatus.imageLoadError')))
          }
          return null
        }
      })
  )

  if (generation !== imageLoadGeneration) {
    loaded.forEach(entry => {
      if (entry) URL.revokeObjectURL(entry.url)
    })
    return
  }

  for (const entry of loaded) {
    if (!entry) continue
    const previous = imageUrls[entry.id]
    if (previous) URL.revokeObjectURL(previous)
    imageUrls[entry.id] = entry.url
  }
}

async function manualReload() {
  await reload(false)
  // After base reload, refresh any cached detail records so non-7d availability
  // values stay in sync without forcing the user to switch tabs again.
  if (currentWindow.value !== '7d') {
    await Promise.all(items.value.map(it => loadDetail(it.id, true)))
  }
}

async function loadDetail(id: number, force = false) {
  if (!force && detailCache[id]) return
  try {
    detailCache[id] = await fetchChannelMonitorDetail(id)
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.detailLoadError')))
  }
}

async function ensureDetailsForWindow() {
  if (currentWindow.value === '7d') return
  await Promise.all(items.value.map(it => loadDetail(it.id)))
}

// ── Handlers ──
async function handleWindowChange(value: MonitorWindow) {
  currentWindow.value = value
  await ensureDetailsForWindow()
}

function openDetail(group: GroupedChannelStatus) {
  detailTarget.value = group
  showDetail.value = true
}

function closeDetail() {
  showDetail.value = false
  detailTarget.value = null
}

watch(items, () => {
  void ensureDetailsForWindow()
})

watch(
  () => appStore.cachedPublicSettings?.channel_monitor_enabled,
  (enabled) => {
    if (enabled === false) autoRefresh.stop()
    else if (autoRefresh.enabled.value) autoRefresh.start()
  },
)

onMounted(() => {
  void reload(false)
  if (appStore.cachedPublicSettings?.channel_monitor_enabled !== false) {
    autoRefresh.setEnabled(true)
  }
})

onBeforeUnmount(() => {
  if (abortController) abortController.abort()
  imageLoadGeneration++
  for (const url of Object.values(imageUrls)) URL.revokeObjectURL(url)
  for (const id of Object.keys(imageUrls)) delete imageUrls[Number(id)]
})
</script>
