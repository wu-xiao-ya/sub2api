<template><BrandIcon class="model-icon" :brand="resolveModelBrand(model)" :size="size" /></template>
<script setup lang="ts">
import BrandIcon from './BrandIcon.vue'
import { resolveModelBrand } from '@/utils/brandRegistry'
withDefaults(defineProps<{ model: string; size?: number | string }>(), { size: '18px' })
</script>
