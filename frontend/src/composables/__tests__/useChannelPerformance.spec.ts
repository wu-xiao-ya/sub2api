import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { useChannelPerformance } from '../useChannelPerformance'
import type { PerformanceFilter, PerformanceResult } from '@/api/channelPerformance'

const empty: PerformanceResult = { items: [], version: 2, source: 'user_requests', start: '', end: '', updated_at: null, coverage_start: null, coverage_end: null, incomplete_since: null }
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

describe('passive performance refresh', () => {
  it('pauses when hidden, honors off, aborts on close, and never probes', async () => {
    vi.useFakeTimers()
    const visibility = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    const load = vi.fn().mockResolvedValue(empty)
    const enabled = ref(true)
    const seconds = ref(5)
    const filters = ref<PerformanceFilter>({ range: '24h' })
    let state!: ReturnType<typeof useChannelPerformance>
    const wrapper = mount(defineComponent({ setup() { state = useChannelPerformance(filters, enabled, seconds, load); return () => null } }))
    await flushPromises()
    expect(load).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(5000)
    expect(load).toHaveBeenCalledTimes(2)
    visibility.mockReturnValue('hidden'); document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(30000)
    expect(load).toHaveBeenCalledTimes(2)
    visibility.mockReturnValue('visible'); document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(load).toHaveBeenCalledTimes(3)
    seconds.value = 0; await flushPromises(); await vi.advanceTimersByTimeAsync(30000)
    expect(load).toHaveBeenCalledTimes(3)
    filters.value = { range: '7d', service_tier: 'unknown' }; await flushPromises()
    expect(load).toHaveBeenCalledTimes(4)
    expect(load.mock.lastCall?.[0]).toEqual({ range: '7d', service_tier: 'unknown' })
    enabled.value = false; await flushPromises(); await state.refresh()
    expect(load).toHaveBeenCalledTimes(4)
    wrapper.unmount()
  })

  it('discards stale responses and does not overlap refreshes', async () => {
    vi.useFakeTimers()
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    let resolveOld!: (value: PerformanceResult) => void
    const load = vi.fn().mockImplementationOnce(() => new Promise<PerformanceResult>(resolve => { resolveOld = resolve }))
      .mockResolvedValue({ ...empty, version: 3 })
    const filters = ref<PerformanceFilter>({ range: '24h' })
    let state!: ReturnType<typeof useChannelPerformance>
    const wrapper = mount(defineComponent({ setup() { state = useChannelPerformance(filters, ref(true), ref(5), load); return () => null } }))
    await state.refresh(); await vi.advanceTimersByTimeAsync(15000)
    expect(load).toHaveBeenCalledTimes(1)
    const signal = load.mock.calls[0][1] as AbortSignal
    filters.value = { range: '90m' }; await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(state.data.value?.version).toBe(3)
    resolveOld(empty); await flushPromises()
    expect(state.data.value?.version).toBe(3)
    wrapper.unmount(); await vi.advanceTimersByTimeAsync(30000)
    expect(load).toHaveBeenCalledTimes(2)
  })

  it('exposes error state without mutating existing price or channel data', async () => {
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    const load = vi.fn().mockRejectedValue(new Error('unavailable'))
    let state!: ReturnType<typeof useChannelPerformance>
    const wrapper = mount(defineComponent({ setup() { state = useChannelPerformance(ref({ range: '24h' }), ref(true), ref(0), load); return () => null } }))
    await flushPromises()
    expect(state.error.value).toBe(true)
    expect(state.data.value).toBeNull()
    expect(state.loading.value).toBe(false)
    wrapper.unmount()
  })
})
