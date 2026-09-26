<template>
  <div class="space-y-4">
    <div v-if="loading" class="py-8 text-center text-sm text-gray-500">
      {{ t("common.loading") }}
    </div>

    <div v-else-if="groups.length === 0" class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
      {{ t("intelligence.gallery.empty") }}
    </div>

    <!-- One strip per configured target: oldest left, newest right, mirroring
         the channel monitor timeline reading direction. -->
    <div
      v-for="group in groups"
      :key="group.targetId"
      class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800"
    >
      <div class="mb-3 flex flex-wrap items-center gap-2">
        <span class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ group.groupName }}</span>
        <span class="rounded bg-gray-100 px-2 py-0.5 text-xs text-gray-600 dark:bg-dark-600 dark:text-gray-300">
          {{ group.model }}
        </span>
        <span class="text-xs text-gray-400">{{ group.items.length }} {{ t("intelligence.gallery.samples") }}</span>
      </div>

      <div class="flex gap-2 overflow-x-auto pb-1">
        <button
          v-for="item in group.items"
          :key="item.id"
          type="button"
          class="relative h-20 w-20 flex-none overflow-hidden rounded-lg border transition hover:ring-2 hover:ring-indigo-400"
          :class="item.status === 'success' ? 'border-gray-200 dark:border-dark-600' : 'border-red-200 bg-red-50 dark:border-red-900 dark:bg-red-950'"
          :title="tooltip(item)"
          @click="selected = item"
        >
          <img
            v-if="item.status === 'success' && item.response_svg"
            :src="svgDataUri(item.response_svg)"
            class="h-full w-full bg-white object-contain"
            :alt="t('intelligence.gallery.thumbnailAlt')"
          />
          <span v-else-if="item.status === 'no_svg'" class="block pt-8 text-xs text-gray-500 dark:text-gray-400">
            {{ t("intelligence.gallery.noSvg") }}
          </span>
          <span v-else class="block pt-8 text-xs text-red-600 dark:text-red-400">
            {{ t("intelligence.gallery.failed") }}
          </span>
        </button>
      </div>
    </div>

    <!-- Detail dialog: full-size SVG plus metadata. -->
    <BaseDialog
      :show="selected !== null"
      :title="t('intelligence.gallery.detailTitle')"
      width="wide"
      @close="selected = null"
    >
      <div v-if="selected" class="space-y-3">
        <div class="flex flex-wrap gap-x-6 gap-y-1 text-sm text-gray-600 dark:text-gray-300">
          <span>{{ selected.group_name }}</span>
          <span class="font-medium">{{ selected.model }}</span>
          <span>{{ formatTime(selected.created_at) }}</span>
          <span v-if="selected.latency_ms !== null">{{ selected.latency_ms }} ms</span>
          <span
            class="rounded px-2 py-0.5 text-xs"
            :class="statusBadgeClass(selected.status)"
          >
            {{ statusText(selected.status) }}
          </span>
        </div>

        <div
          v-if="selected.status === 'success' && selected.response_svg"
          class="flex max-h-[60vh] items-center justify-center overflow-auto rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-600"
        >
          <img
            :src="svgDataUri(selected.response_svg)"
            class="max-h-[55vh] w-auto max-w-full object-contain"
            :alt="t('intelligence.gallery.thumbnailAlt')"
          />
        </div>
        <div
          v-else
          class="max-h-[60vh] overflow-auto whitespace-pre-wrap rounded-lg border border-gray-200 bg-gray-50 p-4 text-xs text-gray-600 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-300"
        >
          {{ selected.status === "no_svg" ? selected.response_excerpt : selected.error_message }}
        </div>
      </div>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import {
  intelligenceProbeAPI,
  type IntelligenceProbeUserResult,
} from "@/api/intelligenceProbe";
import BaseDialog from "@/components/common/BaseDialog.vue";

const props = withDefaults(
  defineProps<{
    // When provided the gallery renders this data directly instead of
    // fetching; parents that need to know "is there anything" (e.g. to hide
    // the whole section) use it to stay the single data owner.
    items?: IntelligenceProbeUserResult[] | null;
  }>(),
  {
    items: null,
  },
);

const { t, locale } = useI18n();

const loading = ref(false);
const fetchedItems = ref<IntelligenceProbeUserResult[]>([]);
let pollTimer: number | undefined;
const selected = ref<IntelligenceProbeUserResult | null>(null);

const effectiveItems = computed<IntelligenceProbeUserResult[]>(() =>
  props.items !== null ? props.items : fetchedItems.value,
);

interface GalleryGroup {
  targetId: number;
  groupName: string;
  model: string;
  items: IntelligenceProbeUserResult[];
}

const groups = computed<GalleryGroup[]>(() => {
  const byTarget = new Map<number, GalleryGroup>();
  for (const item of effectiveItems.value) {
    let group = byTarget.get(item.target_id);
    if (!group) {
      group = { targetId: item.target_id, groupName: item.group_name, model: item.model, items: [] };
      byTarget.set(item.target_id, group);
    }
    group.items.push(item);
  }
  // Newest first within a group: the API returns newest-first rows already,
  // so keep the incoming order.
  return Array.from(byTarget.values());
});

// SVG is rendered through an <img>, which never executes scripts inside the
// document, so untrusted model output cannot run in the page.
function svgDataUri(svg: string): string {
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
}

function tooltip(item: IntelligenceProbeUserResult): string {
  const parts = [formatTime(item.created_at), statusText(item.status)];
  if (item.latency_ms !== null) parts.push(`${item.latency_ms} ms`);
  return parts.join(" · ");
}

function statusText(status: string): string {
  if (status === "success") return t("intelligence.gallery.statusSuccess");
  if (status === "no_svg") return t("intelligence.gallery.statusNoSvg");
  return t("intelligence.gallery.statusFailed");
}

function statusBadgeClass(status: string): string {
  if (status === "success") return "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300";
  if (status === "no_svg") return "bg-gray-100 text-gray-600 dark:bg-dark-600 dark:text-gray-300";
  return "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300";
}

function formatTime(value: string): string {
  return new Date(value).toLocaleString(locale.value);
}

async function load() {
  if (props.items !== null) return;
  loading.value = true;
  try {
    const data = await intelligenceProbeAPI.listResults(120);
    fetchedItems.value = data.items ?? [];
  } finally {
    loading.value = false;
  }
}

watch(
  () => props.items,
  () => {
    if (props.items !== null) fetchedItems.value = [];
  },
);

onMounted(() => {
  void load();
  // Admin galleries render new results without a manual reload; the drawings
  // land a couple of minutes after each run starts.
  pollTimer = window.setInterval(() => {
    if (!document.hidden && props.items === null) void load();
  }, 30000);
});

onBeforeUnmount(() => {
  if (pollTimer !== undefined) window.clearInterval(pollTimer);
});

defineExpose({ load });
</script>
