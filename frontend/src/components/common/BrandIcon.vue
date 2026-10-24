<template>
  <span class="brand-icon" :style="{ width: dimension, height: dimension }" :data-brand="brand || 'unknown'" aria-hidden="true">
    <img v-if="src" :src="src" alt="" width="24" height="24" draggable="false" />
    <Icon v-else name="cube" size="sm" />
  </span>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import { brandAssets, type BrandKey } from '@/utils/brandRegistry'
const props = withDefaults(defineProps<{ brand?: BrandKey | null; size?: number | string }>(), { brand: null, size: 20 })
const dimension = computed(() => typeof props.size === 'number' ? `${Math.max(1, props.size)}px` : /^\d+(?:\.\d+)?(?:px|em|rem)$/.test(props.size) ? props.size : '20px')
const src = computed(() => props.brand ? brandAssets[props.brand] : null)
</script>
<style scoped>
.brand-icon { display: inline-flex; align-items: center; justify-content: center; flex: none; vertical-align: middle; color: #71717a; }
.brand-icon img { display: block; width: 100%; height: 100%; object-fit: contain; }
:global(.dark .brand-icon:has(img)) { background: #fafafa; border-radius: 3px; }
.brand-icon[data-brand="kimi"] { background: #09090b; border-radius: 3px; padding: 2px; }
:global(.dark .brand-icon[data-brand="kimi"]) { background: #09090b; }
</style>
