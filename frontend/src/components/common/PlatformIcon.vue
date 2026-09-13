<template><BrandIcon :brand="resolvePlatformBrand(platform)" :size="sizes[size]" /></template>
<script setup lang="ts">
import BrandIcon from './BrandIcon.vue'
import { resolvePlatformBrand } from '@/utils/brandRegistry'
withDefaults(defineProps<{ platform?: string; size?: 'xs' | 'sm' | 'md' | 'lg' }>(), { size: 'sm' })
const sizes = { xs: 12, sm: 14, md: 16, lg: 20 }
</script>
