<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import HostMetricChart from '@/components/admin/HostMetricChart.vue'
import { useServerMonitor } from '@/composables/useServerMonitor'
const { t } = useI18n()
const { summary, samples, stale, loading, error, refreshSeconds, load, logService, logLines, logs, logsLoading, logsError, loadLogs } = useServerMonitor()
const labels = computed(() => samples.value.map(s => new Date(s.at).toLocaleTimeString()))
const latest = computed(() => samples.value[samples.value.length - 1])
const charts = computed(() => [
  { key: 'cpu', percent: true, unit: '%', series: [{ label: t('serverMonitor.cpu'), values: samples.value.map(s => s.cpu), color: '#2563eb' }] },
  { key: 'memory', percent: true, unit: '%', series: [{ label: t('serverMonitor.memory'), values: samples.value.map(s => s.mem), color: '#059669' }] },
  { key: 'disk', percent: true, unit: '%', series: [{ label: t('serverMonitor.disk'), values: samples.value.map(s => s.disk), color: '#d97706' }] },
  { key: 'network', percent: false, unit: ' MiB/s', series: [
    { label: t('serverMonitor.download'), values: samples.value.map(s => s.rx / 1048576), color: '#2563eb' },
    { label: t('serverMonitor.upload'), values: samples.value.map(s => s.tx / 1048576), color: '#db2777' }
  ] }
])
function bytes(value: number | undefined): string {
  if (value == null || !Number.isFinite(value)) return '-'
  if (value >= 1073741824) return (value / 1073741824).toFixed(2) + ' GiB'
  if (value >= 1048576) return (value / 1048576).toFixed(2) + ' MiB'
  return (value / 1024).toFixed(1) + ' KiB'
}
</script>

<template>
  <AppLayout>
    <div class="space-y-5 text-gray-900 dark:text-gray-100">
      <header class="flex flex-wrap items-center justify-between gap-3">
        <div class="min-w-0">
          <h1 class="text-xl font-semibold">{{ t('serverMonitor.title') }}</h1>
          <p v-if="summary" class="mt-1 break-all text-sm text-gray-500">{{ summary.host.hostname }} · {{ summary.host.kernel }}</p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <label for="monitor-refresh" class="text-sm">{{ t('serverMonitor.interval') }}</label>
          <select id="monitor-refresh" v-model.number="refreshSeconds" class="input !w-auto">
            <option :value="0">{{ t('serverMonitor.off') }}</option>
            <option v-for="n in [5, 15, 30, 60]" :key="n" :value="n">{{ t('serverMonitor.seconds', { n }) }}</option>
          </select>
          <button class="btn btn-secondary h-10 w-10 !p-0" :disabled="loading" :title="t('serverMonitor.refresh')" :aria-label="t('serverMonitor.refresh')" @click="load"><Icon name="refresh" size="md" /></button>
        </div>
      </header>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ t('serverMonitor.unavailable') }}</p>
      <p v-if="stale" role="status" class="text-sm text-amber-600">{{ t('serverMonitor.stale') }}</p>
      <p v-if="!summary && loading" role="status">{{ t('serverMonitor.loading') }}</p>
      <template v-if="summary">
        <div class="flex flex-wrap gap-x-6 gap-y-1 text-xs text-gray-500">
          <span>{{ t('serverMonitor.updated') }}: {{ new Date(summary.generated_at).toLocaleString() }}</span>
          <span>{{ t('serverMonitor.uptime') }}: {{ t('serverMonitor.days', { n: (summary.host.uptime_seconds / 86400).toFixed(1) }) }}</span>
          <span>{{ t('serverMonitor.load') }}: {{ summary.host.load.join(' / ') }}</span>
        </div>
        <dl class="grid grid-cols-1 gap-4 border-y border-gray-200 py-5 sm:grid-cols-2 xl:grid-cols-4 dark:border-gray-700">
          <div><dt class="text-sm text-gray-500">{{ t('serverMonitor.cpu') }}</dt><dd class="mt-1 text-xl tabular-nums">{{ summary.cpu.percent.toFixed(1) }}%</dd><dd class="text-xs text-gray-500">{{ t('serverMonitor.cores', { n: summary.cpu.cores }) }}</dd></div>
          <div><dt class="text-sm text-gray-500">{{ t('serverMonitor.memory') }} · {{ t('serverMonitor.usedTotal') }}</dt><dd class="mt-1 text-lg tabular-nums">{{ bytes(summary.memory.used) }} / {{ bytes(summary.memory.total) }}</dd><dd class="text-xs text-gray-500">{{ summary.memory.percent.toFixed(1) }}%</dd></div>
          <div><dt class="text-sm text-gray-500">{{ t('serverMonitor.disk') }} · {{ t('serverMonitor.usedTotal') }}</dt><dd class="mt-1 text-lg tabular-nums">{{ bytes(summary.disk.used) }} / {{ bytes(summary.disk.total) }}</dd><dd class="text-xs text-gray-500">{{ summary.disk.percent.toFixed(1) }}%</dd></div>
          <div><dt class="text-sm text-gray-500">{{ t('serverMonitor.network') }}</dt><dd class="mt-1 text-sm tabular-nums">{{ t('serverMonitor.download') }} {{ bytes(latest?.rx) }}/s</dd><dd class="text-sm tabular-nums">{{ t('serverMonitor.upload') }} {{ bytes(latest?.tx) }}/s</dd></div>
        </dl>
        <div class="grid min-w-0 grid-cols-1 gap-x-6 md:grid-cols-2">
          <HostMetricChart v-for="chart in charts" :key="chart.key" :title="t('serverMonitor.' + chart.key)" :labels="labels" :series="chart.series" :unit="chart.unit" :percent="chart.percent" />
        </div>
        <section class="grid grid-cols-1 gap-5 border-t border-gray-200 pt-5 md:grid-cols-2 dark:border-gray-700">
          <div><h2 class="mb-2 text-sm font-semibold">{{ t('serverMonitor.services') }}</h2><div v-for="s in summary.services" :key="s.name" class="flex justify-between gap-3 py-1 text-sm"><span class="min-w-0 break-all">{{ s.name }}</span><span class="shrink-0" :class="s.ok ? 'text-emerald-600' : 'text-red-600'" :title="s.active + ' ' + (s.health || '')">{{ t(s.ok ? 'serverMonitor.healthy' : 'serverMonitor.abnormal') }}</span></div></div>
          <div><h2 class="mb-2 text-sm font-semibold">{{ t('serverMonitor.probes') }}</h2><div v-for="p in summary.probes" :key="p.name" class="flex justify-between gap-3 py-1 text-sm"><span class="min-w-0 break-all">{{ p.name }}</span><span class="shrink-0" :class="p.ok ? 'text-emerald-600' : 'text-red-600'">{{ p.status }} · {{ p.ms }} ms</span></div></div>
        </section>
        <section class="min-w-0 border-t border-gray-200 pt-5 dark:border-gray-700">
          <h2 class="mb-3 text-sm font-semibold">{{ t('serverMonitor.containers') }}</h2>
          <div class="overflow-x-auto"><table class="w-full text-left text-sm"><thead class="text-gray-500"><tr><th class="p-2">{{ t('serverMonitor.name') }}</th><th class="p-2">{{ t('serverMonitor.state') }}</th><th class="p-2">{{ t('serverMonitor.image') }}</th></tr></thead><tbody><tr v-for="c in summary.containers" :key="c.name" class="border-t border-gray-100 dark:border-gray-800"><td class="max-w-xs break-all p-2">{{ c.name }}</td><td class="p-2" :title="c.status">{{ c.state }}</td><td class="max-w-md break-all p-2 text-xs">{{ c.image }}</td></tr></tbody></table></div>
        </section>
        <details class="border-t border-gray-200 pt-4 dark:border-gray-700"><summary class="cursor-pointer text-sm font-semibold">{{ t('serverMonitor.ports') }}</summary><pre class="mt-3 max-h-56 overflow-auto text-xs">{{ summary.ports.join(String.fromCharCode(10)) }}</pre></details>
      </template>
      <section class="border-t border-gray-200 pt-5 dark:border-gray-700">
        <div class="flex flex-wrap items-center gap-2">
          <h2 class="mr-auto text-sm font-semibold">{{ t('serverMonitor.logs') }}</h2>
          <select v-model="logService" :disabled="logsLoading" class="input !w-auto" :aria-label="t('serverMonitor.logs')"><option v-for="s in ['sub2api', 'postgres', 'redis', 'caddy', 'server-monitor']" :key="s" :value="s">{{ s }}</option></select>
          <select v-model.number="logLines" :disabled="logsLoading" class="input !w-auto" :aria-label="t('serverMonitor.logLines')"><option v-for="n in [20, 80, 200]" :key="n" :value="n">{{ n }}</option></select>
          <button class="btn btn-secondary" :disabled="logsLoading" @click="loadLogs"><Icon name="refresh" size="sm" class="mr-2" />{{ t('serverMonitor.loadLogs') }}</button>
        </div>
        <p v-if="logsError" role="alert" class="mt-2 text-sm text-red-600">{{ t('serverMonitor.logsFailed') }}</p>
        <pre class="mt-3 max-h-96 min-h-24 overflow-auto rounded border border-gray-200 bg-gray-50 p-3 text-xs dark:border-gray-700 dark:bg-gray-900">{{ logs || t('serverMonitor.noLogs') }}</pre>
      </section>
    </div>
  </AppLayout>
</template>
