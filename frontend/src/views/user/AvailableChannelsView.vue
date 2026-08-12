<template>
  <AppLayout>
    <div class="mx-auto flex w-full max-w-[1800px] flex-col gap-5">
      <section class="flex flex-col gap-1">
        <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('availableChannels.title') }}</h1>
        <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('availableChannels.description') }}</p>
      </section>

      <section class="flex flex-col gap-3 xl:flex-row xl:items-center">
        <div class="relative min-w-0 flex-1">
          <Icon
            name="search"
            size="md"
            class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500"
          />
          <input
            v-model="searchQuery"
            type="search"
            :placeholder="t('availableChannels.searchPlaceholder')"
            class="input h-11 pl-10"
          />
        </div>

        <div class="flex items-center gap-2">
          <div
            class="inline-flex h-11 items-center rounded-lg border border-gray-200 bg-white p-1 dark:border-dark-700 dark:bg-dark-800"
            :aria-label="t('availableChannels.tokenUnit')"
          >
            <button
              type="button"
              class="h-full rounded-md px-3 text-xs font-semibold transition-colors"
              :class="tokenScale === 1_000_000 ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300' : 'text-gray-500 hover:text-gray-800 dark:text-dark-400 dark:hover:text-dark-100'"
              @click="tokenScale = 1_000_000"
            >
              / 1M
            </button>
            <button
              type="button"
              class="h-full rounded-md px-3 text-xs font-semibold transition-colors"
              :class="tokenScale === 1_000 ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300' : 'text-gray-500 hover:text-gray-800 dark:text-dark-400 dark:hover:text-dark-100'"
              @click="tokenScale = 1_000"
            >
              / 1K
            </button>
          </div>

          <Select
            v-model="sortMode"
            :options="sortOptions"
            :aria-label="t('availableChannels.sortLabel')"
            class="min-w-36"
          />

          <button
            type="button"
            @click="loadChannels"
            :disabled="loading"
            class="btn btn-secondary h-11 w-11 !px-0"
            :title="t('common.refresh', 'Refresh')"
            :aria-label="t('common.refresh', 'Refresh')"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </section>

      <div class="grid items-start gap-5 xl:grid-cols-[15.5rem_minmax(0,1fr)]">
        <aside class="xl:sticky xl:top-6">
          <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800">
            <div class="flex items-center justify-between gap-3">
              <span class="text-sm font-semibold text-gray-900 dark:text-white">
                {{ t('availableChannels.filtersTitle') }}
              </span>
              <button
                v-if="hasActiveFilters"
                type="button"
                class="text-xs font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
                @click="clearFilters"
              >
                {{ t('availableChannels.clearFilters') }}
              </button>
            </div>

            <section class="mt-5">
              <h2 class="text-xs font-semibold uppercase tracking-wide text-gray-400 dark:text-dark-500">
                {{ t('availableChannels.providersTitle') }}
              </h2>
              <div class="mt-2 space-y-1">
                <label
                  v-for="platform in platformOptions"
                  :key="platform.value"
                  class="flex cursor-pointer items-center gap-2 rounded-md px-1 py-1.5 text-sm text-gray-600 hover:bg-gray-50 dark:text-dark-300 dark:hover:bg-dark-700/60"
                >
                  <input
                    type="checkbox"
                    class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
                    :checked="selectedPlatforms.includes(platform.value)"
                    @change="togglePlatform(platform.value)"
                  />
                  <PlatformIcon :platform="platform.value as GroupPlatform" size="xs" :class="platformTextClass(platform.value)" />
                  <span class="min-w-0 flex-1 truncate">{{ platform.label }}</span>
                  <span class="text-xs text-gray-400 dark:text-dark-500">{{ platform.count }}</span>
                </label>
              </div>
            </section>

            <section v-if="groupOptions.length > 0" class="mt-5 border-t border-gray-100 pt-5 dark:border-dark-700">
              <h2 class="text-xs font-semibold uppercase tracking-wide text-gray-400 dark:text-dark-500">
                {{ t('availableChannels.groupsFilterTitle') }}
              </h2>
              <div class="mt-2 max-h-72 space-y-1 overflow-y-auto pr-1">
                <label
                  v-for="group in groupOptions"
                  :key="group.id"
                  class="flex cursor-pointer items-start gap-2 rounded-md px-1 py-1.5 text-sm text-gray-600 hover:bg-gray-50 dark:text-dark-300 dark:hover:bg-dark-700/60"
                >
                  <input
                    type="checkbox"
                    class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
                    :checked="selectedGroupIDs.includes(group.id)"
                    @change="toggleGroup(group.id)"
                  />
                  <span class="min-w-0 flex-1 break-words leading-5">{{ group.name }}</span>
                  <span class="pt-0.5 text-xs text-gray-400 dark:text-dark-500">{{ group.count }}</span>
                </label>
              </div>
            </section>

            <section v-if="billingOptions.length > 0" class="mt-5 border-t border-gray-100 pt-5 dark:border-dark-700">
              <h2 class="text-xs font-semibold uppercase tracking-wide text-gray-400 dark:text-dark-500">
                {{ t('availableChannels.billingFilterTitle') }}
              </h2>
              <div class="mt-2 space-y-1">
                <label
                  v-for="billing in billingOptions"
                  :key="billing.value"
                  class="flex cursor-pointer items-center gap-2 rounded-md px-1 py-1.5 text-sm text-gray-600 hover:bg-gray-50 dark:text-dark-300 dark:hover:bg-dark-700/60"
                >
                  <input
                    type="checkbox"
                    class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
                    :checked="selectedBillingModes.includes(billing.value)"
                    @change="toggleBillingMode(billing.value)"
                  />
                  <span class="min-w-0 flex-1 truncate">{{ billing.label }}</span>
                  <span class="text-xs text-gray-400 dark:text-dark-500">{{ billing.count }}</span>
                </label>
              </div>
            </section>
          </div>
        </aside>

        <section class="min-w-0">
          <div class="mb-3 flex items-center justify-between gap-3">
            <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-500 dark:text-dark-400">
              <span class="inline-flex items-center gap-1">
                <Icon name="cpu" size="xs" />
                {{ t('availableChannels.modelCount', { count: filteredItems.length }) }}
              </span>
              <span class="inline-flex items-center gap-1">
                <Icon name="users" size="xs" />
                {{ t('availableChannels.groupCount', { count: visibleGroupCount }) }}
              </span>
            </div>
            <span class="text-xs text-gray-400 dark:text-dark-500">
              {{ t('availableChannels.channelCount', { count: visibleChannelCount }) }}
            </span>
          </div>

          <ModelPlazaGrid
            :items="filteredItems"
            :loading="loading"
            :empty-label="emptyLabel"
            :user-group-rates="userGroupRates"
            :token-scale="tokenScale"
            @toggle-group="toggleGroup"
          />
        </section>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import ModelPlazaGrid from '@/components/channels/ModelPlazaGrid.vue'
import userChannelsAPI, { type UserAvailableChannel } from '@/api/channels'
import userGroupsAPI from '@/api/groups'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { BILLING_MODE_IMAGE, BILLING_MODE_PER_REQUEST, BILLING_MODE_TOKEN } from '@/constants/channel'
import type { GroupPlatform } from '@/types'
import { buildModelPlazaItems, lowestGroupRate } from '@/components/channels/modelPlaza'
import { platformLabel, platformTextClass } from '@/utils/platformColors'

const { t } = useI18n()
const appStore = useAppStore()

const channels = ref<UserAvailableChannel[]>([])
const userGroupRates = ref<Record<number, number>>({})
const loading = ref(false)
const searchQuery = ref('')
const selectedPlatforms = ref<string[]>([])
const selectedGroupIDs = ref<number[]>([])
const selectedBillingModes = ref<string[]>([])
const tokenScale = ref<1_000 | 1_000_000>(1_000_000)
const sortMode = ref('default')

const modelItems = computed(() => buildModelPlazaItems(channels.value))

const platformOptions = computed(() => {
  const counts = new Map<string, number>()
  modelItems.value.forEach(item => counts.set(item.platform, (counts.get(item.platform) || 0) + 1))
  return Array.from(counts, ([value, count]) => ({ value, count, label: platformLabel(value) }))
    .sort((a, b) => a.label.localeCompare(b.label))
})

const groupOptions = computed(() => {
  const entries = new Map<number, { id: number; name: string; count: number }>()
  modelItems.value.forEach(item => {
    item.groups.forEach(group => {
      const current = entries.get(group.id)
      if (current) current.count++
      else entries.set(group.id, { id: group.id, name: group.name, count: 1 })
    })
  })
  return Array.from(entries.values()).sort((a, b) => a.name.localeCompare(b.name))
})

const billingOptions = computed(() => {
  const counts = new Map<string, number>()
  modelItems.value.forEach(item => {
    const mode = item.model.pricing?.billing_mode
    if (mode) counts.set(mode, (counts.get(mode) || 0) + 1)
  })
  const labels: Record<string, string> = {
    [BILLING_MODE_TOKEN]: t('availableChannels.pricing.billingModeToken'),
    [BILLING_MODE_PER_REQUEST]: t('availableChannels.pricing.billingModePerRequest'),
    [BILLING_MODE_IMAGE]: t('availableChannels.pricing.billingModeImage'),
  }
  return [BILLING_MODE_TOKEN, BILLING_MODE_PER_REQUEST, BILLING_MODE_IMAGE]
    .filter(value => counts.has(value))
    .map(value => ({ value, label: labels[value], count: counts.get(value) || 0 }))
})

const sortOptions = computed<SelectOption[]>(() => [
  { value: 'default', label: t('availableChannels.sortDefault') },
  { value: 'name', label: t('availableChannels.sortName') },
  { value: 'rate', label: t('availableChannels.sortRate') },
])

const filteredItems = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  const items = modelItems.value.filter(item => {
    if (selectedPlatforms.value.length > 0 && !selectedPlatforms.value.includes(item.platform)) return false
    if (
      selectedGroupIDs.value.length > 0 &&
      !item.groups.some(group => selectedGroupIDs.value.includes(group.id))
    ) {
      return false
    }
    if (
      selectedBillingModes.value.length > 0 &&
      !selectedBillingModes.value.includes(item.model.pricing?.billing_mode || '')
    ) {
      return false
    }
    if (!query) return true
    return [
      item.model.name,
      item.channelName,
      item.channelDescription,
      item.platform,
      platformLabel(item.platform),
      ...item.groups.map(group => group.name),
    ].some(value => value.toLowerCase().includes(query))
  })

  if (sortMode.value === 'name') {
    return [...items].sort((a, b) => a.model.name.localeCompare(b.model.name))
  }
  if (sortMode.value === 'rate') {
    return [...items].sort((a, b) => {
      const aRate = lowestGroupRate(a.groups, userGroupRates.value) ?? Number.POSITIVE_INFINITY
      const bRate = lowestGroupRate(b.groups, userGroupRates.value) ?? Number.POSITIVE_INFINITY
      return aRate - bRate || a.model.name.localeCompare(b.model.name)
    })
  }
  return items
})

const visibleGroupCount = computed(() => {
  const ids = new Set<number>()
  filteredItems.value.forEach(item => item.groups.forEach(group => ids.add(group.id)))
  return ids.size
})

const visibleChannelCount = computed(() => new Set(filteredItems.value.map(item => item.channelName)).size)

const hasActiveFilters = computed(
  () =>
    searchQuery.value.trim().length > 0 ||
    selectedPlatforms.value.length > 0 ||
    selectedGroupIDs.value.length > 0 ||
    selectedBillingModes.value.length > 0 ||
    sortMode.value !== 'default',
)

const emptyLabel = computed(() =>
  hasActiveFilters.value ? t('availableChannels.noMatching') : t('availableChannels.empty'),
)

function togglePlatform(platform: string) {
  selectedPlatforms.value = selectedPlatforms.value.includes(platform)
    ? selectedPlatforms.value.filter(item => item !== platform)
    : [...selectedPlatforms.value, platform]
}

function toggleGroup(groupID: number) {
  selectedGroupIDs.value = selectedGroupIDs.value.includes(groupID)
    ? selectedGroupIDs.value.filter(item => item !== groupID)
    : [...selectedGroupIDs.value, groupID]
}

function toggleBillingMode(mode: string) {
  selectedBillingModes.value = selectedBillingModes.value.includes(mode)
    ? selectedBillingModes.value.filter(item => item !== mode)
    : [...selectedBillingModes.value, mode]
}

function clearFilters() {
  searchQuery.value = ''
  selectedPlatforms.value = []
  selectedGroupIDs.value = []
  selectedBillingModes.value = []
  sortMode.value = 'default'
}

async function loadChannels() {
  loading.value = true
  try {
    const [list, rates] = await Promise.all([
      userChannelsAPI.getAvailable(),
      userGroupsAPI.getUserGroupRates().catch((err: unknown) => {
        console.error('Failed to load user group rates:', err)
        return {} as Record<number, number>
      }),
    ])
    channels.value = list
    userGroupRates.value = rates
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    loading.value = false
  }
}

onMounted(loadChannels)
</script>
