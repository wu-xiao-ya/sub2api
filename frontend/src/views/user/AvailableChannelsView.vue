<template>
  <AppLayout>
    <div class="mx-auto flex w-full max-w-[1680px] flex-col gap-4">
      <section class="flex flex-col gap-3 border-b border-gray-200 pb-4 dark:border-dark-700 lg:flex-row lg:items-center">
        <div class="relative min-w-0 flex-1 lg:max-w-xl">
          <Icon
            name="search"
            size="md"
            class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
          />
          <input
            v-model="searchQuery"
            type="search"
            :placeholder="t('availableChannels.searchPlaceholder')"
            class="input pl-10"
          />
        </div>

        <div class="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-center lg:ml-auto">
          <Select
            v-model="platformFilter"
            :options="platformOptions"
            class="w-full sm:w-44"
            :aria-label="t('availableChannels.platformFilter')"
          />

          <div class="flex items-center justify-between gap-3 sm:justify-end">
            <div class="flex items-center gap-3 text-xs text-gray-500 dark:text-dark-400">
              <span class="inline-flex items-center gap-1">
                <Icon name="database" size="xs" />
                {{ t('availableChannels.channelCount', { count: filteredChannels.length }) }}
              </span>
              <span class="inline-flex items-center gap-1">
                <Icon name="users" size="xs" />
                {{ t('availableChannels.groupCount', { count: visibleGroupCount }) }}
              </span>
              <span class="inline-flex items-center gap-1">
                <Icon name="cpu" size="xs" />
                {{ t('availableChannels.modelCount', { count: visibleModelCount }) }}
              </span>
            </div>

            <button
              type="button"
              @click="loadChannels"
              :disabled="loading"
              class="btn btn-secondary h-9 w-9 !px-0"
              :title="t('common.refresh', 'Refresh')"
              :aria-label="t('common.refresh', 'Refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
          </div>
        </div>
      </section>

      <AvailableChannelsTable
        :rows="filteredChannels"
        :loading="loading"
        :user-group-rates="userGroupRates"
        pricing-key-prefix="availableChannels.pricing"
        :no-pricing-label="t('availableChannels.noPricing')"
        :no-models-label="t('availableChannels.noModels')"
        :no-groups-label="t('availableChannels.noGroups')"
        :empty-label="emptyLabel"
      />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import AvailableChannelsTable from '@/components/channels/AvailableChannelsTable.vue'
import userChannelsAPI, { type UserAvailableChannel } from '@/api/channels'
import userGroupsAPI from '@/api/groups'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import { platformLabel } from '@/utils/platformColors'

const { t } = useI18n()
const appStore = useAppStore()

const channels = ref<UserAvailableChannel[]>([])
const userGroupRates = ref<Record<number, number>>({})
const loading = ref(false)
const searchQuery = ref('')
const platformFilter = ref<string | null>(null)

const platformOptions = computed<SelectOption[]>(() => {
  const platforms = new Set<string>()
  channels.value.forEach(channel => channel.platforms.forEach(section => platforms.add(section.platform)))
  return [
    { value: null, label: t('availableChannels.allPlatforms') },
    ...Array.from(platforms)
      .sort((a, b) => platformLabel(a).localeCompare(platformLabel(b)))
      .map(platform => ({ value: platform, label: platformLabel(platform) })),
  ]
})

/**
 * 搜索过滤：
 * - 命中渠道名/描述 → 整个渠道（所有 platforms）都保留
 * - 否则按 platform/group/model 维度在 sections 里过滤，保留有匹配的 section
 * - 所有 sections 都不匹配时，渠道本身被过滤掉
 */
const filteredChannels = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  return channels.value
    .map((ch) => {
      const platformSections = ch.platforms.filter(
        section => !platformFilter.value || section.platform === platformFilter.value,
      )
      if (platformSections.length === 0) return null
      if (!q) return { ...ch, platforms: platformSections }

      const nameHit = ch.name.toLowerCase().includes(q)
      const descHit = (ch.description || '').toLowerCase().includes(q)
      if (nameHit || descHit) return { ...ch, platforms: platformSections }
      const matchingSections = platformSections.filter(
        (p) =>
          p.platform.toLowerCase().includes(q) ||
          platformLabel(p.platform).toLowerCase().includes(q) ||
          p.groups.some((g) => g.name.toLowerCase().includes(q)) ||
          p.supported_models.some((m) => m.name.toLowerCase().includes(q)),
      )
      if (matchingSections.length === 0) return null
      return { ...ch, platforms: matchingSections }
    })
    .filter((ch): ch is UserAvailableChannel => ch !== null)
})

const visibleGroupCount = computed(() => {
  const ids = new Set<number>()
  filteredChannels.value.forEach(channel => {
    channel.platforms.forEach(section => section.groups.forEach(group => ids.add(group.id)))
  })
  return ids.size
})

const visibleModelCount = computed(() => {
  const models = new Set<string>()
  filteredChannels.value.forEach(channel => {
    channel.platforms.forEach(section => {
      section.supported_models.forEach(model => models.add(`${section.platform}:${model.name}`))
    })
  })
  return models.size
})

const emptyLabel = computed(() =>
  searchQuery.value.trim() || platformFilter.value
    ? t('availableChannels.noMatching')
    : t('availableChannels.empty'),
)

async function loadChannels() {
  loading.value = true
  try {
    // 渠道列表和用户专属倍率并发拉取。专属倍率失败不阻塞渠道展示——
    // 失败时只是无法渲染专属倍率角标，降级为仅显示默认倍率。
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
