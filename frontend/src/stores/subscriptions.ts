/**
 * Subscription Store
 * Global state for shared subscription purchase snapshots with caching and deduplication.
 */

import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import subscriptionsAPI, { type SharedSubscription } from '@/api/subscriptions'

const CACHE_TTL_MS = 60_000
let requestGeneration = 0

export const useSubscriptionStore = defineStore('subscriptions', () => {
  const sharedSubscriptions = ref<SharedSubscription[]>([])
  // Compatibility alias for existing callers. Values are shared purchase snapshots.
  const activeSubscriptions = computed((): SharedSubscription[] => sharedSubscriptions.value)
  const loading = ref(false)
  const loaded = ref(false)
  const lastFetchedAt = ref<number | null>(null)
  let activePromise: Promise<SharedSubscription[]> | null = null
  let pollerInterval: ReturnType<typeof setInterval> | null = null

  const hasSharedSubscriptions = computed(() => sharedSubscriptions.value.length > 0)
  const hasActiveSubscriptions = hasSharedSubscriptions

  async function fetchSharedSubscriptions(force = false): Promise<SharedSubscription[]> {
    const now = Date.now()
    if (!force && loaded.value && lastFetchedAt.value !== null && now - lastFetchedAt.value < CACHE_TTL_MS) {
      return sharedSubscriptions.value
    }
    if (activePromise && !force) return activePromise

    const currentGeneration = ++requestGeneration
    loading.value = true
    const requestPromise = subscriptionsAPI.getMySharedSubscriptions()
      .then((purchases) => {
        if (currentGeneration === requestGeneration) {
          sharedSubscriptions.value = purchases
          loaded.value = true
          lastFetchedAt.value = Date.now()
        }
        return purchases
      })
      .catch((error) => {
        console.error('Failed to fetch shared subscriptions:', error)
        throw error
      })
      .finally(() => {
        if (activePromise === requestPromise) {
          loading.value = false
          activePromise = null
        }
      })

    activePromise = requestPromise
    return activePromise
  }

  // Compatibility alias; this invokes the shared purchase fetch above.
  const fetchActiveSubscriptions = fetchSharedSubscriptions

  function startPolling() {
    if (pollerInterval) return
    pollerInterval = setInterval(() => {
      fetchSharedSubscriptions(true).catch((error) => console.error('Subscription polling failed:', error))
    }, 5 * 60 * 1000)
  }

  function stopPolling() {
    if (pollerInterval) {
      clearInterval(pollerInterval)
      pollerInterval = null
    }
  }

  function clear() {
    requestGeneration++
    activePromise = null
    sharedSubscriptions.value = []
    loaded.value = false
    lastFetchedAt.value = null
    stopPolling()
  }

  function invalidateCache() {
    lastFetchedAt.value = null
  }

  return {
    sharedSubscriptions,
    activeSubscriptions,
    loading,
    hasSharedSubscriptions,
    hasActiveSubscriptions,
    fetchSharedSubscriptions,
    fetchActiveSubscriptions,
    startPolling,
    stopPolling,
    clear,
    invalidateCache,
  }
})
