<template><BrandIcon :brand="resolvePlatformBrand(provider)" :size="size" /></template>
<script setup lang="ts">
import BrandIcon from '@/components/common/BrandIcon.vue'
import { resolvePlatformBrand } from '@/utils/brandRegistry'
withDefaults(defineProps<{ provider: string; size?: number }>(), { size: 20 })
</script>
