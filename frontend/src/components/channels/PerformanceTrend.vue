<template>
  <section class="min-w-0 border-t border-gray-200 py-4 dark:border-dark-700">
    <h3 class="mb-3 text-sm font-semibold text-gray-900 dark:text-dark-100">{{ title }}</h3>
    <div class="h-52 min-w-0"><Line :data="data" :options="options" :aria-label="title" role="img" /></div>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, type ChartOptions } from 'chart.js'
import { Line } from 'vue-chartjs'
import type { PerformancePoint } from '@/api/channelPerformance'
ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend)
const props = defineProps<{ title: string; points: PerformancePoint[]; field: 'first_character_ms' | 'output_tps' | 'success_rate'; color: string }>()
const unit = computed(() => props.field === 'first_character_ms' ? 's' : props.field === 'success_rate' ? '%' : 'tok/s')
const data = computed(() => ({
  labels: props.points.map(point => new Date(point.at).toLocaleString(undefined, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })),
  datasets: [{ label: props.title, data: props.points.map(point => point[props.field] == null ? null : point[props.field]! / (props.field === 'first_character_ms' ? 1000 : 1)),
    borderColor: props.color, backgroundColor: props.color, borderWidth: 2, pointRadius: 3, pointHoverRadius: 5, spanGaps: false, tension: 0.1 }]
}))
const options = computed<ChartOptions<'line'>>(() => ({
  responsive: true, maintainAspectRatio: false, animation: false,
  interaction: { mode: 'index', intersect: false },
  scales: { x: { ticks: { maxTicksLimit: 3, maxRotation: 0, color: '#808080' }, grid: { display: false } },
    y: { beginAtZero: true, ...(props.field === 'success_rate' ? { max: 100 } : {}), ticks: { color: '#808080', callback: v => v + ' ' + unit.value }, grid: { color: 'rgba(128,128,128,0.15)' } } },
  plugins: { legend: { display: false }, tooltip: { callbacks: { title: rows => props.points[rows[0]?.dataIndex]?.at ? new Date(props.points[rows[0].dataIndex].at).toLocaleString() : '', label: ctx => ctx.parsed.y == null ? '' : ctx.parsed.y.toFixed(2) + ' ' + unit.value } } }
}))
</script>
