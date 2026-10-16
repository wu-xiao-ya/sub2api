<template>
  <div class="flex flex-wrap items-center gap-1.5">
    <span
      :class="[
        'inline-flex items-center gap-0.5 text-[10px] font-medium uppercase',
        exclusive ? 'text-purple-600 dark:text-purple-400' : 'text-gray-500 dark:text-dark-400',
      ]"
      :title="exclusive ? t('availableChannels.exclusiveTooltip') : t('availableChannels.publicTooltip')"
    >
      <Icon :name="exclusive ? 'shield' : 'globe'" size="xs" />
      {{ exclusive ? t('availableChannels.exclusive') : t('availableChannels.public') }}
    </span>

    <GroupBadge
      v-for="group in groups"
      :key="group.id"
      :name="group.name"
      :platform="group.platform as GroupPlatform"
      :subscription-type="(group.subscription_type || 'standard') as SubscriptionType"
      :rate-multiplier="group.rate_multiplier"
      :user-rate-multiplier="userGroupRates[group.id] ?? null"
      :peak-rate-enabled="group.peak_rate_enabled"
      :peak-start="group.peak_start"
      :peak-end="group.peak_end"
      :peak-rate-multiplier="group.peak_rate_multiplier"
      always-show-rate
    />
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import type { UserAvailableGroup } from '@/api/channels'
import type { GroupPlatform, SubscriptionType } from '@/types'

defineProps<{
  groups: UserAvailableGroup[]
  exclusive: boolean
  userGroupRates: Record<number, number>
}>()

const { t } = useI18n()
</script>
