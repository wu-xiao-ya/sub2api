<template>
  <div class="space-y-4">
    <p class="text-sm text-gray-500 dark:text-gray-400">
      {{ t("admin.settings.imageUpstreamCost.hint") }}
    </p>

    <div v-if="loading" class="py-8 text-center text-sm text-gray-500">
      {{ t("common.loading") }}
    </div>

    <template v-else>
      <!-- Global default -->
      <div
        class="rounded-lg border border-gray-200 p-4 dark:border-dark-600"
      >
        <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-200">
          {{ t("admin.settings.imageUpstreamCost.defaultPrice") }}
        </label>
        <input
          v-model.number="form.cost_per_image"
          type="number"
          step="0.0001"
          min="0"
          class="input w-48"
        />
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t("admin.settings.imageUpstreamCost.defaultPriceHint") }}
        </p>
      </div>

      <!-- Per-model tiers -->
      <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <h4 class="text-sm font-medium text-gray-700 dark:text-gray-200">
            {{ t("admin.settings.imageUpstreamCost.byModel") }}
          </h4>
          <button type="button" class="btn btn-secondary btn-sm" @click="addModel">
            {{ t("admin.settings.imageUpstreamCost.addModel") }}
          </button>
        </div>

        <p v-if="form.model_overrides.length === 0" class="text-xs text-gray-500 dark:text-gray-400">
          {{ t("admin.settings.imageUpstreamCost.noModelOverride") }}
        </p>

        <div
          v-for="(entry, index) in form.model_overrides"
          :key="`model-${index}`"
          class="mb-3 grid grid-cols-1 gap-2 border-b border-gray-100 pb-3 last:mb-0 last:border-b-0 last:pb-0 dark:border-dark-700 sm:grid-cols-[minmax(0,1fr)_repeat(3,7rem)_auto]"
        >
          <input
            v-model="entry.model"
            type="text"
            :placeholder="t('admin.settings.imageUpstreamCost.modelPlaceholder')"
            class="input"
          />
          <input
            v-for="tier in TIERS"
            :key="tier"
            v-model.number="entry.tiers[tier]"
            type="number"
            step="0.0001"
            min="0"
            :placeholder="tier"
            class="input"
          />
          <button
            type="button"
            class="btn btn-ghost btn-sm text-red-600"
            @click="form.model_overrides.splice(index, 1)"
          >
            {{ t("common.delete") }}
          </button>
        </div>
      </div>

      <!-- Per-account flat override -->
      <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
        <div class="mb-3 flex items-center justify-between">
          <h4 class="text-sm font-medium text-gray-700 dark:text-gray-200">
            {{ t("admin.settings.imageUpstreamCost.byAccount") }}
          </h4>
          <button type="button" class="btn btn-secondary btn-sm" @click="addAccount">
            {{ t("admin.settings.imageUpstreamCost.addAccount") }}
          </button>
        </div>

        <p v-if="form.account_overrides.length === 0" class="text-xs text-gray-500 dark:text-gray-400">
          {{ t("admin.settings.imageUpstreamCost.noAccountOverride") }}
        </p>

        <div
          v-for="(entry, index) in form.account_overrides"
          :key="`acct-${index}`"
          class="mb-2 flex flex-wrap items-center gap-2"
        >
          <input
            v-model.number="entry.account_id"
            type="number"
            min="1"
            :placeholder="t('admin.settings.imageUpstreamCost.accountIdPlaceholder')"
            class="input w-32"
          />
          <input
            v-model.number="entry.cost_per_image"
            type="number"
            step="0.0001"
            min="0"
            class="input w-40"
          />
          <button
            type="button"
            class="btn btn-ghost btn-sm text-red-600"
            @click="form.account_overrides.splice(index, 1)"
          >
            {{ t("common.delete") }}
          </button>
        </div>
      </div>

      <!-- Snapshot multiplier switch -->
      <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
        <label class="flex items-start gap-3">
          <input
            v-model="form.ignore_upstream_rate_snapshot"
            type="checkbox"
            class="mt-1"
          />
          <span>
            <span class="block text-sm font-medium text-gray-700 dark:text-gray-200">
              {{ t("admin.settings.imageUpstreamCost.ignoreSnapshot") }}
            </span>
            <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.imageUpstreamCost.ignoreSnapshotHint") }}
            </span>
          </span>
        </label>
      </div>

      <div v-if="saveMessage" class="text-sm" :class="saveError ? 'text-red-600' : 'text-green-600'">
        {{ saveMessage }}
      </div>

      <button
        type="button"
        class="btn btn-primary"
        :disabled="saving"
        @click="save"
      >
        {{ saving ? t("common.saving") : t("common.save") }}
      </button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  settingsAPI,
  type ImageUpstreamCostAccountOverride,
  type ImageUpstreamCostModelOverride,
} from "@/api/admin/settings";

const { t } = useI18n();

// Tier keys must match the backend's canonical billing sizes.
const TIERS = ["1K", "2K", "4K"] as const;

interface ModelOverrideForm {
  model: string;
  tiers: Record<string, number | undefined>;
}

const loading = ref(true);
const saving = ref(false);
const saveMessage = ref("");
const saveError = ref(false);

const form = reactive({
  cost_per_image: 0,
  ignore_upstream_rate_snapshot: false,
  model_overrides: [] as ModelOverrideForm[],
  account_overrides: [] as ImageUpstreamCostAccountOverride[],
});

function addModel() {
  form.model_overrides.push({ model: "", tiers: {} });
}

function addAccount() {
  form.account_overrides.push({ account_id: 0, cost_per_image: 0 });
}

// Drop blank rows and unset tiers so the request only carries real overrides.
function buildModelOverrides(): ImageUpstreamCostModelOverride[] {
  return form.model_overrides
    .filter((entry) => entry.model.trim() !== "")
    .map((entry) => {
      const tiers: Record<string, number> = {};
      for (const tier of TIERS) {
        const value = entry.tiers[tier];
        if (typeof value === "number" && Number.isFinite(value)) {
          tiers[tier] = value;
        }
      }
      return { model: entry.model.trim(), tiers };
    })
    .filter((entry) => Object.keys(entry.tiers).length > 0);
}

function buildAccountOverrides(): ImageUpstreamCostAccountOverride[] {
  return form.account_overrides.filter(
    (entry) => Number.isFinite(entry.account_id) && entry.account_id > 0,
  );
}

async function load() {
  loading.value = true;
  try {
    const settings = await settingsAPI.getImageUpstreamCost();
    form.cost_per_image = settings.cost_per_image;
    form.ignore_upstream_rate_snapshot = settings.ignore_upstream_rate_snapshot;
    form.model_overrides = (settings.model_overrides ?? []).map((entry) => ({
      model: entry.model,
      tiers: { ...entry.tiers },
    }));
    form.account_overrides = (settings.account_overrides ?? []).map((entry) => ({ ...entry }));
  } catch (error) {
    saveError.value = true;
    saveMessage.value = error instanceof Error ? error.message : String(error);
  } finally {
    loading.value = false;
  }
}

async function save() {
  saving.value = true;
  saveMessage.value = "";
  saveError.value = false;
  try {
    await settingsAPI.updateImageUpstreamCost({
      cost_per_image: form.cost_per_image,
      ignore_upstream_rate_snapshot: form.ignore_upstream_rate_snapshot,
      model_overrides: buildModelOverrides(),
      account_overrides: buildAccountOverrides(),
    });
    saveMessage.value = t("admin.settings.imageUpstreamCost.saved");
    await load();
  } catch (error) {
    saveError.value = true;
    saveMessage.value = error instanceof Error ? error.message : String(error);
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>
