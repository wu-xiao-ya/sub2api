<template>
  <div ref="containerRef" class="relative">
    <!-- Selected account chip -->
    <div v-if="selectedAccount" class="flex flex-wrap items-center gap-2">
      <span
        class="inline-flex max-w-full items-center gap-1.5 rounded-md bg-gray-100 px-2.5 py-1.5 text-xs text-gray-700 dark:bg-dark-600 dark:text-gray-200"
      >
        <span class="max-w-56 truncate font-medium" :title="selectedLabel">
          {{ selectedLabel }}
        </span>
        <button
          type="button"
          class="shrink-0 rounded text-gray-400 hover:text-red-600 dark:hover:text-red-400"
          :aria-label="t('common.delete')"
          :title="t('common.delete')"
          @click="clear"
        >
          <Icon name="x" size="xs" :stroke-width="2" />
        </button>
      </span>
    </div>

    <!-- Search input -->
    <div v-else class="relative">
      <Icon
        name="search"
        size="sm"
        class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
      />
      <input
        v-model="searchQuery"
        type="text"
        autocomplete="off"
        class="input pl-9"
        :placeholder="placeholderText"
        @input="onInput"
        @focus="onFocus"
      />
    </div>

    <!-- Results -->
    <div
      v-if="showDropdown && !selectedAccount"
      class="absolute z-50 mt-1 max-h-64 w-full overflow-auto rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-600 dark:bg-dark-700"
    >
      <p v-if="searchLoading" class="px-3 py-2 text-xs text-gray-500">
        {{ t("common.loading") }}
      </p>
      <p
        v-else-if="availableResults.length === 0"
        class="px-3 py-2 text-xs text-gray-500"
      >
        {{ emptyText }}
      </p>
      <template v-else>
        <button
          v-for="account in availableResults"
          :key="account.id"
          type="button"
          class="flex w-full items-center justify-between gap-2 px-3 py-2 text-left text-sm hover:bg-gray-50 dark:hover:bg-dark-600"
          @click="select(account)"
        >
          <span class="min-w-0 flex-1 truncate">{{ account.name }}</span>
          <span class="shrink-0 text-xs text-gray-400">#{{ account.id }}</span>
        </button>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { adminAPI } from "@/api/admin";
import { useKeyedDebouncedSearch } from "@/composables/useKeyedDebouncedSearch";
import Icon from "@/components/icons/Icon.vue";

interface SimpleAccount {
  id: number;
  name: string;
}

const props = withDefaults(
  defineProps<{
    modelValue: number | null;
    /** Restrict results to one platform; empty means all platforms. */
    platform?: string;
    placeholder?: string;
  }>(),
  {
    platform: "",
    placeholder: "",
  },
);

const emit = defineEmits<{
  "update:modelValue": [value: number | null];
}>();

const { t } = useI18n();
const containerRef = ref<HTMLElement | null>(null);
const searchQuery = ref("");
const searchResults = ref<SimpleAccount[]>([]);
const showDropdown = ref(false);
// Seeds the label for IDs that arrived from saved settings, so a configured
// account shows its name before the operator ever opens the picker.
const labelCache = ref<Record<number, string>>({});

const SEARCH_KEY = "account";

const placeholderText = computed(
  () => props.placeholder || t("admin.settings.imageUpstreamCost.accountSearchPlaceholder"),
);
const emptyText = computed(() => t("admin.settings.imageUpstreamCost.accountSearchEmpty"));

const selectedAccountId = computed(() => {
  const value = props.modelValue;
  return typeof value === "number" && Number.isInteger(value) && value > 0 ? value : null;
});

const selectedAccount = computed<SimpleAccount | null>(() => {
  const id = selectedAccountId.value;
  if (id === null) return null;
  return { id, name: labelCache.value[id] ?? "" };
});

const selectedLabel = computed(() => {
  const account = selectedAccount.value;
  if (!account) return "";
  return account.name
    ? t("admin.settings.imageUpstreamCost.accountLabel", { name: account.name, id: account.id })
    : t("admin.settings.imageUpstreamCost.accountIdFallback", { id: account.id });
});

const availableResults = computed(() =>
  searchResults.value.filter((account) => account.id !== selectedAccountId.value),
);

const searchRunner = useKeyedDebouncedSearch<SimpleAccount[]>({
  delay: 300,
  search: async (keyword, { signal }) => {
    const response = await adminAPI.accounts.list(
      1,
      20,
      { ...(props.platform ? { platform: props.platform } : {}), search: keyword },
      { signal },
    );
    return response.items.map((item) => ({ id: item.id, name: item.name }));
  },
  onSuccess: (_key, result) => {
    searchResults.value = result;
    // Remember names as they are seen so chips stay labelled after selection.
    const next = { ...labelCache.value };
    for (const account of result) next[account.id] = account.name;
    labelCache.value = next;
  },
  onError: () => {
    searchResults.value = [];
  },
});

const searchLoading = ref(false);

function onInput(): void {
  showDropdown.value = true;
  searchLoading.value = true;
  searchRunner.trigger(SEARCH_KEY, searchQuery.value.trim());
  // The runner has no completion callback for this flag, so clear it on a
  // trailing timer; the dropdown already renders "loading" unconditionally
  // while a request is pending.
  window.setTimeout(() => {
    searchLoading.value = false;
  }, 400);
}

function onFocus(): void {
  showDropdown.value = true;
  if (searchResults.value.length === 0) {
    onInput();
  }
}

function select(account: SimpleAccount): void {
  labelCache.value = { ...labelCache.value, [account.id]: account.name };
  emit("update:modelValue", account.id);
  searchQuery.value = "";
  showDropdown.value = false;
}

function clear(): void {
  emit("update:modelValue", null);
  searchQuery.value = "";
}

// Fill in labels for IDs restored from saved settings.
async function hydrateLabel(): Promise<void> {
  const id = selectedAccountId.value;
  if (id === null || labelCache.value[id]) return;
  try {
    const account = await adminAPI.accounts.getById(id);
    labelCache.value = { ...labelCache.value, [id]: account.name };
  } catch {
    // Leave the fallback (#id) in place; the operator can still replace it.
  }
}

watch(selectedAccountId, () => {
  void hydrateLabel();
});
onMounted(() => {
  void hydrateLabel();
});

function handleClickOutside(event: MouseEvent): void {
  const target = event.target as HTMLElement | null;
  if (!target || !containerRef.value?.contains(target)) {
    showDropdown.value = false;
  }
}
onMounted(() => document.addEventListener("mousedown", handleClickOutside));
onUnmounted(() => document.removeEventListener("mousedown", handleClickOutside));
</script>
