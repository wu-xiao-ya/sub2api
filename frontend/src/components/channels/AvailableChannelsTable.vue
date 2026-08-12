<template>
  <div
    v-if="loading && rows.length === 0"
    class="grid grid-cols-1 items-start gap-3 xl:grid-cols-2"
  >
    <div
      v-for="index in 4"
      :key="index"
      class="min-h-64 animate-pulse rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800"
    >
      <div class="flex items-start justify-between gap-3">
        <div class="flex min-w-0 items-center gap-3">
          <div class="h-9 w-9 rounded-md bg-gray-100 dark:bg-dark-700"></div>
          <div class="min-w-0 space-y-2">
            <div class="h-4 w-32 rounded bg-gray-200 dark:bg-dark-700"></div>
            <div class="h-3 w-48 max-w-[48vw] rounded bg-gray-100 dark:bg-dark-900"></div>
          </div>
        </div>
        <div class="h-7 w-16 rounded bg-gray-100 dark:bg-dark-900"></div>
      </div>
      <div class="mt-4 space-y-3 border-t border-gray-100 pt-3 dark:border-dark-700">
        <div v-for="section in 2" :key="section" class="grid grid-cols-[6rem_1fr] gap-3">
          <div class="h-7 rounded bg-gray-100 dark:bg-dark-900"></div>
          <div class="space-y-2">
            <div class="h-3 w-16 rounded bg-gray-100 dark:bg-dark-900"></div>
            <div class="h-6 w-full rounded bg-gray-100 dark:bg-dark-900"></div>
          </div>
        </div>
      </div>
    </div>
  </div>

  <EmptyState
    v-else-if="rows.length === 0"
    :title="emptyLabel"
    :description="t('availableChannels.emptyDescription')"
  />

  <div v-else class="grid grid-cols-1 items-start gap-3 xl:grid-cols-2">
    <article
      v-for="(channel, channelIndex) in rows"
      :key="`${channel.name}-${channelIndex}`"
      class="overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm transition-shadow hover:shadow-md dark:border-dark-700 dark:bg-dark-800"
    >
      <header class="flex items-start justify-between gap-4 px-4 py-3.5">
        <div class="flex min-w-0 items-start gap-3">
          <div class="flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-md bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-200">
            <Icon name="database" size="md" />
          </div>
          <div class="min-w-0">
            <h2 class="truncate text-sm font-semibold text-gray-900 dark:text-white">{{ channel.name }}</h2>
            <p v-if="channel.description" class="mt-0.5 line-clamp-2 text-xs leading-5 text-gray-500 dark:text-dark-400">
              {{ channel.description }}
            </p>
          </div>
        </div>
        <div class="flex flex-shrink-0 items-center gap-1.5 text-[11px] text-gray-500 dark:text-dark-400">
          <span class="inline-flex items-center gap-1">
            <Icon name="grid" size="xs" />
            {{ channel.platforms.length }}
          </span>
          <span class="inline-flex items-center gap-1">
            <Icon name="cpu" size="xs" />
            {{ channelModelCount(channel) }}
          </span>
        </div>
      </header>

      <div class="border-t border-gray-100 dark:border-dark-700">
        <section
          v-for="(section, sectionIndex) in channel.platforms"
          :key="`${channel.name}-${section.platform}`"
          class="grid gap-3 px-4 py-3 sm:grid-cols-[7.5rem_minmax(0,1fr)] lg:grid-cols-[7.5rem_minmax(0,0.9fr)_minmax(0,1.1fr)]"
          :class="{ 'border-t border-gray-100 dark:border-dark-700': sectionIndex > 0 }"
        >
          <div class="flex min-w-0 items-start gap-2.5">
            <span
              :class="[
                'inline-flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-md border',
                platformBadgeClass(section.platform),
              ]"
            >
              <PlatformIcon :platform="section.platform as GroupPlatform" size="sm" />
            </span>
            <div class="min-w-0 pt-0.5">
              <div class="truncate text-xs font-semibold text-gray-800 dark:text-dark-100">
                {{ platformLabel(section.platform) }}
              </div>
              <div class="mt-0.5 text-[11px] text-gray-400 dark:text-dark-500">
                {{ section.groups.length }} {{ t('availableChannels.groupUnit') }} · {{ section.supported_models.length }} {{ t('availableChannels.modelUnit') }}
              </div>
            </div>
          </div>

          <div class="min-w-0 lg:border-l lg:border-gray-100 lg:pl-3 lg:dark:border-dark-700">
            <div class="mb-1.5 flex items-center gap-1 text-[10px] font-medium uppercase text-gray-400 dark:text-dark-500">
              <Icon name="users" size="xs" />
              {{ t('availableChannels.groupsLabel') }}
            </div>
            <div v-if="section.groups.length > 0" class="flex flex-col gap-1.5">
              <GroupRow
                v-if="exclusiveGroups(section).length > 0"
                :groups="exclusiveGroups(section)"
                :exclusive="true"
                :user-group-rates="userGroupRates"
              />
              <GroupRow
                v-if="publicGroups(section).length > 0"
                :groups="publicGroups(section)"
                :exclusive="false"
                :user-group-rates="userGroupRates"
              />
            </div>
            <span v-else class="text-xs text-gray-400 dark:text-dark-500">{{ noGroupsLabel }}</span>
          </div>

          <div class="min-w-0 lg:border-l lg:border-gray-100 lg:pl-3 lg:dark:border-dark-700">
            <div class="mb-1.5 flex items-center gap-1 text-[10px] font-medium uppercase text-gray-400 dark:text-dark-500">
              <Icon name="cpu" size="xs" />
              {{ t('availableChannels.modelsLabel') }}
            </div>
            <div v-if="section.supported_models.length > 0" class="flex flex-wrap gap-1">
              <SupportedModelChip
                v-for="model in section.supported_models"
                :key="`${section.platform}-${model.name}`"
                :model="model"
                :pricing-key-prefix="pricingKeyPrefix"
                :no-pricing-label="noPricingLabel"
                :show-platform="false"
                :platform-hint="section.platform"
              />
            </div>
            <span v-else class="text-xs text-gray-400 dark:text-dark-500">{{ noModelsLabel }}</span>
          </div>
        </section>
      </div>
    </article>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import SupportedModelChip from './SupportedModelChip.vue'
import GroupRow from './AvailableChannelGroupRow.vue'
import type { UserAvailableChannel, UserAvailableGroup, UserChannelPlatformSection } from '@/api/channels'
import type { GroupPlatform } from '@/types'
import { platformBadgeClass, platformLabel } from '@/utils/platformColors'
import EmptyState from '@/components/common/EmptyState.vue'

const props = defineProps<{
  rows: UserAvailableChannel[]
  loading: boolean
  pricingKeyPrefix: string
  noPricingLabel: string
  noModelsLabel: string
  noGroupsLabel: string
  emptyLabel: string
  /** 用户专属倍率（group_id → multiplier）；无专属时由分组标签仅显示默认倍率。 */
  userGroupRates: Record<number, number>
}>()

// Suppress unused warning — props is accessed via template automatically but
// the explicit reference here keeps the linter from flagging userGroupRates.
void props.userGroupRates

const { t } = useI18n()

function exclusiveGroups(section: UserChannelPlatformSection): UserAvailableGroup[] {
  return section.groups.filter((g) => g.is_exclusive)
}

function publicGroups(section: UserChannelPlatformSection): UserAvailableGroup[] {
  return section.groups.filter((g) => !g.is_exclusive)
}

function channelModelCount(channel: UserAvailableChannel): number {
  return channel.platforms.reduce((total, section) => total + section.supported_models.length, 0)
}
</script>
