<template>
  <article
    class="flex min-h-[20rem] flex-col rounded-lg border border-gray-200 bg-white p-4 shadow-sm transition-shadow hover:shadow-md dark:border-dark-700 dark:bg-dark-800"
  >
    <header class="flex items-start gap-3">
      <span
        :class="[
          'flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-md border',
          platformBadgeClass(item.platform),
        ]"
      >
        <PlatformIcon :platform="item.platform as GroupPlatform" size="lg" />
      </span>

      <div class="min-w-0 flex-1">
        <h2 class="truncate font-mono text-sm font-semibold text-gray-900 dark:text-white">
          {{ item.model.name }}
        </h2>
        <div class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px]">
          <span :class="['inline-flex items-center gap-1 font-medium', platformTextClass(item.platform)]">
            <PlatformIcon :platform="item.platform as GroupPlatform" size="xs" />
            {{ platformLabel(item.platform) }}
          </span>
          <span class="rounded bg-primary-50 px-1.5 py-0.5 font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
            {{ billingModeLabel }}
          </span>
          <span v-if="lowestRate !== null" class="font-medium text-emerald-600 dark:text-emerald-400">
            {{ t('availableChannels.lowestRate') }} x{{ formatRate(lowestRate) }}
          </span>
          <span
            v-if="hasLongContext"
            class="inline-flex items-center rounded bg-amber-50 px-1.5 py-0.5 font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
          >
            {{ t('availableChannels.pricing.longContext') }}
          </span>
        </div>
      </div>
    </header>

    <p
      v-if="item.channelDescription"
      class="mt-3 line-clamp-2 min-h-9 text-xs leading-4 text-gray-500 dark:text-dark-400"
    >
      {{ item.channelDescription }}
    </p>
    <p v-else class="mt-3 min-h-9 text-xs leading-4 text-gray-400 dark:text-dark-500">
      {{ t('availableChannels.sourceLabel', { name: item.channelName }) }}
    </p>

    <section
      class="mt-3 min-h-[5.5rem] rounded-md bg-gray-50 px-3 py-2.5 dark:bg-dark-900/45"
      :aria-label="t('availableChannels.pricing.billingMode')"
    >
      <template v-if="item.model.pricing">
        <div v-if="item.model.pricing.billing_mode === BILLING_MODE_TOKEN" class="grid grid-cols-2 gap-x-5 gap-y-1.5">
          <PriceMetric
            :label="t('availableChannels.pricing.inputPrice')"
            :value="formatTokenPrice(item.model.pricing.input_price)"
          />
          <PriceMetric
            :label="t('availableChannels.pricing.outputPrice')"
            :value="formatTokenPrice(item.model.pricing.output_price)"
          />
          <PriceMetric
            v-if="item.model.pricing.cache_write_price != null"
            :label="t('availableChannels.pricing.cacheWritePrice')"
            :value="formatTokenPrice(item.model.pricing.cache_write_price)"
          />
          <PriceMetric
            v-if="item.model.pricing.cache_read_price != null"
            :label="t('availableChannels.pricing.cacheReadPrice')"
            :value="formatTokenPrice(item.model.pricing.cache_read_price)"
          />
          <PriceMetric
            v-if="item.model.pricing.image_input_price != null && item.model.pricing.image_input_price > 0"
            :label="t('availableChannels.pricing.imageInputPrice')"
            :value="formatTokenPrice(item.model.pricing.image_input_price)"
          />
          <PriceMetric
            v-if="item.model.pricing.image_output_price != null && item.model.pricing.image_output_price > 0"
            :label="t('availableChannels.pricing.imageOutputPrice')"
            :value="formatTokenPrice(item.model.pricing.image_output_price)"
          />
        </div>

        <div
          v-else
          class="flex h-full items-center justify-between gap-3"
        >
          <span class="text-xs text-gray-500 dark:text-dark-400">
            {{
              item.model.pricing.billing_mode === BILLING_MODE_IMAGE
                ? t('availableChannels.pricing.imageOutputPrice')
                : t('availableChannels.pricing.perRequestPrice')
            }}
          </span>
          <span class="font-mono text-sm font-semibold text-gray-800 dark:text-dark-100">
            {{ formatScaled(item.model.pricing.per_request_price ?? item.model.pricing.image_output_price, 1) }}
          </span>
        </div>
      </template>
      <span v-else class="flex h-full items-center text-xs text-gray-400 dark:text-dark-500">
        {{ t('availableChannels.noPricing') }}
      </span>

      <div
        v-if="item.model.pricing?.billing_mode === BILLING_MODE_TOKEN"
        class="mt-2 text-right font-mono text-[10px] text-gray-400 dark:text-dark-500"
      >
        / {{ tokenUnitLabel }} tokens
      </div>

      <div
        v-if="hasLongContext"
        class="mt-2 border-t border-gray-200/80 pt-2 text-[10px] text-amber-700 dark:border-dark-700 dark:text-amber-300"
      >
        {{ t('availableChannels.pricing.longContext') }}：
        {{ t('availableChannels.pricing.longContextThreshold', { threshold: formatTokenThreshold }) }}，
        {{ t('availableChannels.pricing.longContextInputMultiplier', { multiplier: formatMultiplier(longContextInputMultiplier) }) }}，
        {{ t('availableChannels.pricing.longContextOutputMultiplier', { multiplier: formatMultiplier(longContextOutputMultiplier) }) }}
      </div>
    </section>

    <section class="mt-4">
      <div class="mb-2 flex items-center justify-between gap-3">
        <span class="text-[11px] font-semibold uppercase tracking-wide text-gray-400 dark:text-dark-500">
          {{ t('availableChannels.groupsLabel') }}
        </span>
        <span class="text-[11px] text-gray-400 dark:text-dark-500">{{ item.groups.length }}</span>
      </div>
      <div v-if="item.groups.length > 0" class="flex flex-wrap gap-1.5">
        <button
          v-for="group in item.groups"
          :key="group.id"
          type="button"
          class="text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50"
          :title="t('availableChannels.filterByGroup', { name: group.name })"
          @click="emit('toggleGroup', group.id)"
        >
          <GroupBadge
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
        </button>
      </div>
      <span v-else class="text-xs text-gray-400 dark:text-dark-500">{{ t('availableChannels.noGroups') }}</span>
    </section>
  </article>
</template>

<script setup lang="ts">
import { computed, defineComponent, h } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupBadge from '@/components/common/GroupBadge.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import type { GroupPlatform, SubscriptionType } from '@/types'
import {
  BILLING_MODE_IMAGE,
  BILLING_MODE_PER_REQUEST,
  BILLING_MODE_TOKEN,
} from '@/constants/channel'
import { formatScaled } from '@/utils/pricing'
import { platformBadgeClass, platformLabel, platformTextClass } from '@/utils/platformColors'
import { lowestGroupRate, type ModelPlazaItem } from './modelPlaza'

const PriceMetric = defineComponent({
  name: 'ModelPlazaPriceMetric',
  props: {
    label: { type: String, required: true },
    value: { type: String, required: true },
  },
  setup(props) {
    return () =>
      h('div', { class: 'flex items-baseline justify-between gap-2' }, [
        h('span', { class: 'truncate text-xs text-gray-500 dark:text-dark-400' }, props.label),
        h('span', { class: 'font-mono text-xs font-semibold text-gray-800 dark:text-dark-100' }, props.value),
      ])
  },
})

const props = defineProps<{
  item: ModelPlazaItem
  userGroupRates: Record<number, number>
  tokenScale: number
}>()

const emit = defineEmits<{
  toggleGroup: [groupID: number]
}>()

const { t } = useI18n()

const lowestRate = computed(() => lowestGroupRate(props.item.groups, props.userGroupRates))
const tokenUnitLabel = computed(() => (props.tokenScale === 1_000 ? '1K' : '1M'))
const longContextPricing = computed(() => {
  const pricing = props.item.model.pricing
  return pricing?.long_context_enabled === true ? pricing : null
})
const hasLongContext = computed(() => longContextPricing.value !== null)
const longContextInputMultiplier = computed(
  () => longContextPricing.value?.long_context_input_multiplier ?? 2,
)
const longContextOutputMultiplier = computed(
  () => longContextPricing.value?.long_context_output_multiplier ?? 1.5,
)
const formatTokenThreshold = computed(() => {
  const threshold = longContextPricing.value?.long_context_input_token_threshold
  if (threshold == null || !Number.isFinite(threshold)) return '-'
  if (threshold >= 1_000_000) return `${formatMultiplier(threshold / 1_000_000)}M`
  if (threshold >= 1_000) return `${formatMultiplier(threshold / 1_000)}K`
  return `${formatMultiplier(threshold)}`
})

const billingModeLabel = computed(() => {
  switch (props.item.model.pricing?.billing_mode) {
    case BILLING_MODE_TOKEN:
      return t('availableChannels.pricing.billingModeToken')
    case BILLING_MODE_PER_REQUEST:
      return t('availableChannels.pricing.billingModePerRequest')
    case BILLING_MODE_IMAGE:
      return t('availableChannels.pricing.billingModeImage')
    default:
      return t('availableChannels.pricing.billingMode')
  }
})

function formatTokenPrice(value: number | null): string {
  return formatScaled(value, props.tokenScale)
}

function formatRate(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(3).replace(/\.?0+$/, '')
}

function formatMultiplier(value: number): string {
  return Number.isFinite(value) ? value.toFixed(2).replace(/\.?0+$/, '') : '-'
}
</script>
