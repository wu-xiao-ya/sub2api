<script setup lang="ts">
import { computed } from 'vue'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, type ChartOptions } from 'chart.js'
import { Line } from 'vue-chartjs'
ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend)
const props = defineProps<{ title: string; labels: string[]; series: { label: string; values: number[]; color: string }[]; unit: string; percent?: boolean }>()
const data = computed(() => ({ labels: props.labels, datasets: props.series.map(s => ({ label: s.label, data: s.values, borderColor: s.color, backgroundColor: s.color, pointRadius: 3, pointHoverRadius: 5, borderWidth: 2, tension: 0.15 })) }))
const options = computed<ChartOptions<'line'>>(() => ({
  responsive: true, maintainAspectRatio: false, animation: false,
  interaction: { mode: 'index', intersect: false },
  scales: { y: { beginAtZero: true, ...(props.percent ? { max: 100 } : {}), ticks: { callback: v => v + props.unit } } },
  plugins: { legend: { display: props.series.length > 1 }, tooltip: { callbacks: { label: ctx => ctx.dataset.label + ': ' + Number(ctx.parsed.y).toFixed(2) + props.unit } } }
}))
</script>
<template>
  <section class="min-w-0 border-t border-gray-200 py-5 dark:border-gray-700">
    <h2 class="mb-3 text-sm font-semibold">{{ title }}</h2>
    <div class="h-56 min-w-0"><Line :data="data" :options="options" :aria-label="title" role="img" /></div>
  </section>
</template>
