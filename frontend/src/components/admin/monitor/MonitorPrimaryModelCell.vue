<template>
  <div class="flex items-center gap-2">
    <span class="text-sm text-gray-900 dark:text-gray-100">{{ row.primary_model }}</span>
    <HelpTooltip>
      <template #trigger>
        <span
          class="inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-medium"
          :class="statusBadgeClass(row.primary_status)"
        >
          {{ statusLabel(row.primary_status) }}
        </span>
      </template>
      <div class="space-y-2">
        <div class="text-xs font-semibold text-gray-100">
          {{ row.primary_model }}
          <span
            class="ml-1 inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium"
            :class="statusBadgeClass(row.primary_status)"
          >
            {{ statusLabel(row.primary_status) }}
          </span>
        </div>
        <div v-if="row.primary_account_name || row.primary_probe_mode" class="space-y-0.5 border-y border-white/10 py-1.5 text-[11px] text-gray-300">
          <div v-if="row.primary_account_name">
            {{ t('admin.channelMonitor.probeAccount', { name: row.primary_account_name }) }}
          </div>
          <div v-if="row.primary_probe_mode">
            {{ t('admin.channelMonitor.probeMode', { mode: probeModeLabel(row.primary_probe_mode) }) }}
          </div>
          <div v-if="row.primary_candidate_count">
            {{ t('admin.channelMonitor.probeCandidates', {
              healthy: row.primary_healthy_count || 0,
              candidates: row.primary_candidate_count,
            }) }}
          </div>
          <div v-if="row.primary_checked_at">
            {{ t('admin.channelMonitor.probeCheckedAt', { time: formatRelativeTime(row.primary_checked_at) }) }}
          </div>
        </div>
        <div v-if="(row.extra_models?.length ?? 0) === 0" class="text-[11px] text-gray-300">
          {{ t('monitorCommon.extraModelsEmpty') }}
        </div>
        <div v-else class="space-y-1">
          <div class="text-[11px] font-semibold uppercase tracking-wide text-gray-400">
            {{ t('monitorCommon.extraModelsHeader') }}
          </div>
          <table class="w-full text-left text-[11px]">
            <thead>
              <tr class="text-gray-400">
                <th class="py-0.5 pr-2 font-medium">{{ t('admin.channelMonitor.columns.primaryModel') }}</th>
                <th class="py-0.5 pr-2 font-medium">{{ t('admin.channelMonitor.columns.actions') }}</th>
                <th class="py-0.5 font-medium">{{ t('admin.channelMonitor.columns.latency') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="m in (row.extra_models_status || [])" :key="m.model">
                <td class="py-0.5 pr-2 text-gray-100">{{ m.model }}</td>
                <td class="py-0.5 pr-2">
                  <span
                    class="inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px]"
                    :class="statusBadgeClass(m.status)"
                  >
                    {{ statusLabel(m.status) }}
                  </span>
                </td>
                <td class="py-0.5 text-gray-100">{{ formatLatency(m.latency_ms) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </HelpTooltip>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'

defineProps<{
  row: ChannelMonitor
}>()

const { t } = useI18n()
const { statusLabel, statusBadgeClass, formatLatency, formatRelativeTime } = useChannelMonitorFormat()

function probeModeLabel(mode: string): string {
  return t(`admin.channelMonitor.probeModes.${mode}`)
}
</script>
