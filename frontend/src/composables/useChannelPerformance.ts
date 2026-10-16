import { onBeforeUnmount, onMounted, ref, shallowRef, watch, type Ref } from 'vue'
import { getChannelPerformance, type PerformanceFilter, type PerformanceResult } from '@/api/channelPerformance'

export function useChannelPerformance(
  filters: Readonly<Ref<PerformanceFilter>>,
  enabled: Readonly<Ref<boolean>>,
  refreshSeconds: Readonly<Ref<number>>,
  load = getChannelPerformance
) {
  const data = shallowRef<PerformanceResult | null>(null)
  const loading = ref(false)
  const error = ref(false)
  let timer: ReturnType<typeof setTimeout> | undefined
  let controller: AbortController | undefined
  let generation = 0
  let mounted = false

  const visible = () => mounted && enabled.value && document.visibilityState !== 'hidden'
  function clearTimer() { if (timer !== undefined) clearTimeout(timer); timer = undefined }
  function cancel() {
    clearTimer()
    generation++
    controller?.abort()
    controller = undefined
    loading.value = false
  }
  function schedule() {
    clearTimer()
    if (visible() && refreshSeconds.value > 0) timer = setTimeout(refresh, Math.max(5, refreshSeconds.value) * 1000)
  }
  async function refresh() {
    if (!visible() || loading.value) return
    clearTimer()
    const current = ++generation
    controller = new AbortController()
    loading.value = true
    try {
      const result = await load({ ...filters.value }, controller.signal)
      if (generation === current) { data.value = result; error.value = false }
    } catch {
      if (generation === current) error.value = true
    } finally {
      if (generation === current) { loading.value = false; controller = undefined; schedule() }
    }
  }
  function visibilityChanged() { cancel(); if (visible()) void refresh() }
  watch(filters, () => { cancel(); data.value = null; error.value = false; void refresh() }, { deep: true })
  watch(enabled, () => { cancel(); if (visible()) void refresh() })
  watch(refreshSeconds, schedule)
  onMounted(() => { mounted = true; document.addEventListener('visibilitychange', visibilityChanged); void refresh() })
  onBeforeUnmount(() => { mounted = false; cancel(); document.removeEventListener('visibilitychange', visibilityChanged) })
  return { data, loading, error, refresh }
}
