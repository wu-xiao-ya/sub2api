<template>
  <AppLayout>
    <div class="console-page space-y-4 pb-8">
      <nav
        class="flex flex-wrap items-center gap-3 border-b border-gray-100 pb-3 dark:border-dark-700"
        role="tablist"
        :aria-label="t('channelStatus.viewAria')"
      >
        <span class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ t('channelStatus.title') }}</span>
        <div class="tabs inline-flex" role="presentation">
          <button
            type="button"
            role="tab"
            class="tab"
            :class="activeView === 'v2' ? 'tab-active' : ''"
            :aria-selected="activeView === 'v2'"
            @click="activeView = 'v2'"
          >
            {{ t('channelStatus.viewV2') }}
          </button>
          <button
            type="button"
            role="tab"
            class="tab"
            :class="activeView === 'v1' ? 'tab-active' : ''"
            :aria-selected="activeView === 'v1'"
            @click="activeView = 'v1'"
          >
            {{ t('channelStatus.viewV1') }}
          </button>
          <button
            type="button"
            role="tab"
            class="tab"
            :class="activeView === 'intelligence' ? 'tab-active' : ''"
            :aria-selected="activeView === 'intelligence'"
            @click="activeView = 'intelligence'"
          >
            {{ t('channelStatus.viewIntelligence') }}
          </button>
        </div>
      </nav>

      <ChannelStatusV2View v-if="activeView === 'v2'" embedded />
      <div v-else-if="activeView === 'intelligence'" class="card p-5 sm:p-6">
        <p class="mb-4 text-sm text-gray-500 dark:text-gray-400">
          {{ t('intelligence.gallery.tabHint') }}
        </p>
        <ProbeGallery />
      </div>
      <ChannelStatusV1View v-else embedded />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import ChannelStatusV1View from './ChannelStatusV1View.vue'
import ChannelStatusV2View from './ChannelStatusV2View.vue'
import ProbeGallery from '@/components/intelligence/ProbeGallery.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()

type StatusView = 'v2' | 'v1' | 'intelligence'

function parseView(value: unknown): StatusView {
  if (value === 'v2' || value === 'intelligence') return value
  return 'v1'
}

const activeView = ref<StatusView>(parseView(route.query.view))

watch(activeView, (view) => {
  void router.replace({
    query: {
      ...route.query,
      view,
    },
  })
})
</script>
