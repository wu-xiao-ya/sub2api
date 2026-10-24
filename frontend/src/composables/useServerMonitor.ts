import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { getServerSummary, getServerLogs, type HostSummary, type LogService } from '@/api/admin/serverMonitor'

export function useServerMonitor() {
  const summary = ref<HostSummary | null>(null)
  const loading = ref(false)
  const error = ref(false)
  const refreshSeconds = ref(15)
  const logService = ref<LogService>('sub2api')
  const logLines = ref(80)
  const logs = ref('')
  const logsLoading = ref(false)
  const logsError = ref(false)
  const clock = ref(Date.now())
  let timer: ReturnType<typeof setTimeout> | undefined
  let clockTimer: ReturnType<typeof setInterval> | undefined
  let controller: AbortController | undefined
  let logController: AbortController | undefined
  let disposed = false
  const samples = computed(() => (summary.value?.samples ?? []).slice(-10))
  const stale = computed(() => summary.value && clock.value - Date.parse(summary.value.generated_at) > 60000)
  function schedule() {
    clearTimeout(timer)
    if (!disposed && !document.hidden && refreshSeconds.value > 0) timer = setTimeout(load, refreshSeconds.value * 1000)
  }
  async function load() {
    if (loading.value || disposed || document.hidden) return
    clearTimeout(timer)
    controller = new AbortController()
    loading.value = true
    try {
      const data = await getServerSummary(controller.signal)
      if (!disposed && !controller.signal.aborted) { summary.value = data; error.value = false; clock.value = Date.now() }
    } catch {
      if (!disposed && !controller.signal.aborted) error.value = true
    } finally { loading.value = false; schedule() }
  }
  async function loadLogs() {
    if (logsLoading.value || disposed) return
    logController = new AbortController()
    logsLoading.value = true
    logsError.value = false
    try {
      const data = await getServerLogs(logService.value, logLines.value, logController.signal)
      if (!disposed) { logs.value = data.lines; logsError.value = !data.ok }
    } catch {
      if (!disposed && !logController.signal.aborted) logsError.value = true
    } finally { logsLoading.value = false }
  }
  function onVisibility() {
    if (document.hidden) { clearTimeout(timer); controller?.abort() }
    else if (refreshSeconds.value > 0) void load()
  }
  watch(refreshSeconds, value => {
    try { localStorage.setItem('server_monitor_refresh_seconds', String(value)) } catch { /* storage may be disabled */ }
    schedule()
  })
  watch([logService, logLines], () => { logs.value = ''; logsError.value = false })
  onMounted(() => {
    try {
      const saved = localStorage.getItem('server_monitor_refresh_seconds')
      if (saved !== null && [0, 5, 15, 30, 60].includes(Number(saved))) refreshSeconds.value = Number(saved)
    } catch { /* storage may be disabled */ }
    document.addEventListener('visibilitychange', onVisibility)
    clockTimer = setInterval(() => { clock.value = Date.now() }, 5000)
    void load()
  })
  onBeforeUnmount(() => {
    disposed = true
    clearTimeout(timer)
    clearInterval(clockTimer)
    controller?.abort()
    logController?.abort()
    document.removeEventListener('visibilitychange', onVisibility)
  })
  return { summary, samples, stale, loading, error, refreshSeconds, load, logService, logLines, logs, logsLoading, logsError, loadLogs }
}
