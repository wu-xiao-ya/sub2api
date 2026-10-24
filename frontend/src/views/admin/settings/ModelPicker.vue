<template>
  <div ref="containerRef" class="relative">
    <!-- Free text stays available because upstream providers expose image model
         IDs that appear in no shipped catalog; candidates are only a shortcut. -->
    <input
      v-model="query"
      type="text"
      autocomplete="off"
      class="input"
      :placeholder="placeholderText"
      @input="showDropdown = true"
      @focus="showDropdown = true"
    />

    <div
      v-if="showDropdown && filteredCandidates.length > 0"
      ref="dropdownRef"
      class="absolute z-50 mt-1 max-h-56 w-full overflow-auto rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-600 dark:bg-dark-700"
    >
      <button
        v-for="candidate in filteredCandidates"
        :key="candidate"
        type="button"
        class="flex w-full items-center px-3 py-2 text-left text-sm hover:bg-gray-50 dark:hover:bg-dark-600"
        @click="pick(candidate)"
      >
        <span class="min-w-0 flex-1 truncate">{{ candidate }}</span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

const props = withDefaults(
  defineProps<{
    modelValue: string;
    candidates?: string[];
    placeholder?: string;
  }>(),
  {
    candidates: () => [],
    placeholder: "",
  },
);

const emit = defineEmits<{
  "update:modelValue": [value: string];
}>();

const { t } = useI18n();
const containerRef = ref<HTMLElement | null>(null);
const dropdownRef = ref<HTMLElement | null>(null);
const showDropdown = ref(false);
const query = ref(props.modelValue);

watch(
  () => props.modelValue,
  (value) => {
    if (value !== query.value) query.value = value;
  },
);

watch(query, (value) => {
  emit("update:modelValue", value);
});

const placeholderText = computed(
  () => props.placeholder || t("admin.settings.imageUpstreamCost.modelSearchPlaceholder"),
);

const filteredCandidates = computed(() => {
  const needle = query.value.trim().toLowerCase();
  const unique = Array.from(new Set(props.candidates.filter((item) => item.trim() !== "")));
  if (!needle) return unique.slice(0, 20);
  return unique.filter((item) => item.toLowerCase().includes(needle)).slice(0, 20);
});

function pick(candidate: string): void {
  query.value = candidate;
  showDropdown.value = false;
}

function handleClickOutside(event: MouseEvent): void {
  const target = event.target as Node | null;
  if (!target) return;
  if (containerRef.value?.contains(target) || dropdownRef.value?.contains(target)) return;
  showDropdown.value = false;
}

// The ref must sit on the wrapper so the input and the teleport-free dropdown
// share one hit area.
onMounted(() => document.addEventListener("mousedown", handleClickOutside));
onUnmounted(() => document.removeEventListener("mousedown", handleClickOutside));
</script>
