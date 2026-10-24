import { defineComponent, nextTick } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useServerMonitor } from '../useServerMonitor'
import { getServerSummary } from '@/api/admin/serverMonitor'
vi.mock('@/api/admin/serverMonitor', () => ({ getServerSummary: vi.fn(), getServerLogs: vi.fn() }))

describe('server monitor polling', () => {
  let state: ReturnType<typeof useServerMonitor>
  let wrapper: ReturnType<typeof mount>
  const start = () => {
    wrapper = mount(defineComponent({ setup() { state = useServerMonitor(); return () => null } }))
  }
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    localStorage.clear()
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    vi.mocked(getServerSummary).mockResolvedValue({ generated_at: new Date().toISOString(), samples: Array.from({ length: 20 }, (_, at) => ({ at })) } as never)
  })
  afterEach(() => { wrapper?.unmount(); vi.useRealTimers() })

  it('keeps only ten samples, switches cadence and supports off', async () => {
    start(); await flushPromises()
    expect(state!.samples.value).toHaveLength(10)
    state!.refreshSeconds.value = 5; await nextTick()
    await vi.advanceTimersByTimeAsync(5000)
    expect(getServerSummary).toHaveBeenCalledTimes(2)
    state!.refreshSeconds.value = 0; await nextTick()
    await vi.advanceTimersByTimeAsync(60000)
    expect(getServerSummary).toHaveBeenCalledTimes(2)
    expect(localStorage.getItem('server_monitor_refresh_seconds')).toBe('0')
    await state!.load()
    expect(getServerSummary).toHaveBeenCalledTimes(3)
  })
  it('does not overlap requests and aborts on unmount', async () => {
    vi.mocked(getServerSummary).mockImplementation(() => new Promise(() => {}))
    start()
    state!.refreshSeconds.value = 5; await nextTick()
    await vi.advanceTimersByTimeAsync(30000)
    await state!.load()
    expect(getServerSummary).toHaveBeenCalledTimes(1)
    const signal = vi.mocked(getServerSummary).mock.calls[0]?.[0]
    wrapper.unmount()
    expect(signal?.aborted).toBe(true)
  })
  it('pauses when hidden, resumes when visible, preserves last good sample on error', async () => {
    start(); await flushPromises()
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(60000)
    expect(getServerSummary).toHaveBeenCalledTimes(1)
    vi.mocked(getServerSummary).mockRejectedValueOnce(new Error('offline'))
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(state!.error.value).toBe(true)
    expect(state!.samples.value).toHaveLength(10)
  })
  it('restores off mode but still fetches once on entry', async () => {
    localStorage.setItem('server_monitor_refresh_seconds', '0')
    start(); await flushPromises()
    await vi.advanceTimersByTimeAsync(60000)
    expect(getServerSummary).toHaveBeenCalledTimes(1)
  })
})
