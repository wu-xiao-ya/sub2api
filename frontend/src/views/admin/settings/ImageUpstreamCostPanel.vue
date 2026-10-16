<template>
  <div class="space-y-4">
    <p class="text-sm text-gray-500 dark:text-gray-400">
      {{ t("admin.settings.imageUpstreamCost.hint") }}
    </p>

    <div v-if="loading" class="py-8 text-center text-sm text-gray-500">
      {{ t("common.loading") }}
    </div>

    <template v-else>
      <!-- All override levels in one table, highest priority first. The global
           default is the last row so an operator reads it as the fallback. -->
      <div class="overflow-x-auto">
        <table class="w-full min-w-[46rem] text-sm">
          <thead>
            <tr class="border-b border-gray-200 text-left text-xs text-gray-500 dark:border-dark-600">
              <th class="py-2 pr-3 font-medium">
                {{ t("admin.settings.imageUpstreamCost.colAccount") }}
              </th>
              <th class="py-2 pr-3 font-medium">
                {{ t("admin.settings.imageUpstreamCost.colModel") }}
              </th>
              <th v-for="tier in TIERS" :key="tier" class="w-28 py-2 pr-3 font-medium">
                {{ tier }}
              </th>
              <th class="w-20 py-2 font-medium"></th>
            </tr>
          </thead>
          <tbody>
            <!-- Account + model rows -->
            <tr
              v-for="(row, index) in accountModelRows"
              :key="`am-${index}`"
              class="border-b border-gray-100 dark:border-dark-700"
            >
              <td class="py-2 pr-3">
                <AccountSelector v-model="row.accountId" :platform="PLATFORM" />
              </td>
              <td class="py-2 pr-3">
                <ModelPicker v-model="row.model" :candidates="candidateModels" />
              </td>
              <td v-for="tier in TIERS" :key="tier" class="py-2 pr-3">
                <input
                  v-model.number="row.tiers[tier]"
                  type="number"
                  step="0.0001"
                  min="0"
                  class="input w-24"
                />
              </td>
              <td class="py-2">
                <button
                  type="button"
                  class="btn btn-ghost btn-sm text-red-600"
                  @click="removeAccountModelRow(index)"
                >
                  {{ t("common.delete") }}
                </button>
              </td>
            </tr>

            <!-- Model rows (any account) -->
            <tr
              v-for="(row, index) in modelRows"
              :key="`m-${index}`"
              class="border-b border-gray-100 dark:border-dark-700"
            >
              <td class="py-2 pr-3 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.imageUpstreamCost.anyAccount") }}
              </td>
              <td class="py-2 pr-3">
                <ModelPicker v-model="row.model" :candidates="candidateModels" />
              </td>
              <td v-for="tier in TIERS" :key="tier" class="py-2 pr-3">
                <input
                  v-model.number="row.tiers[tier]"
                  type="number"
                  step="0.0001"
                  min="0"
                  class="input w-24"
                />
              </td>
              <td class="py-2">
                <button
                  type="button"
                  class="btn btn-ghost btn-sm text-red-600"
                  @click="removeModelRow(index)"
                >
                  {{ t("common.delete") }}
                </button>
              </td>
            </tr>

            <!-- Account rows (all models) -->
            <tr
              v-for="(row, index) in accountRows"
              :key="`a-${index}`"
              class="border-b border-gray-100 dark:border-dark-700"
            >
              <td class="py-2 pr-3">
                <AccountSelector v-model="row.accountId" :platform="PLATFORM" />
              </td>
              <td class="py-2 pr-3 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.imageUpstreamCost.allModels") }}
              </td>
              <td colspan="3" class="py-2 pr-3">
                <input
                  v-model.number="row.costPerImage"
                  type="number"
                  step="0.0001"
                  min="0"
                  class="input w-32"
                />
              </td>
              <td class="py-2">
                <button
                  type="button"
                  class="btn btn-ghost btn-sm text-red-600"
                  @click="removeAccountRow(index)"
                >
                  {{ t("common.delete") }}
                </button>
              </td>
            </tr>

            <!-- Global default -->
            <tr>
              <td class="py-2 pr-3 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.imageUpstreamCost.globalDefault") }}
              </td>
              <td class="py-2 pr-3 text-xs text-gray-500 dark:text-gray-400">
                {{ t("admin.settings.imageUpstreamCost.allModels") }}
              </td>
              <td colspan="3" class="py-2 pr-3">
                <input
                  v-model.number="form.cost_per_image"
                  type="number"
                  step="0.0001"
                  min="0"
                  class="input w-32"
                />
              </td>
              <td class="py-2 text-xs text-gray-400">
                {{ t("admin.settings.imageUpstreamCost.fallback") }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn btn-secondary btn-sm" @click="addAccountModelRow">
          {{ t("admin.settings.imageUpstreamCost.addAccountModel") }}
        </button>
        <button type="button" class="btn btn-secondary btn-sm" @click="addModelRow">
          {{ t("admin.settings.imageUpstreamCost.addModel") }}
        </button>
        <button type="button" class="btn btn-secondary btn-sm" @click="addAccountRow">
          {{ t("admin.settings.imageUpstreamCost.addAccount") }}
        </button>
      </div>

      <p class="text-xs text-gray-500 dark:text-gray-400">
        {{ t("admin.settings.imageUpstreamCost.emptyTierHint") }}
      </p>

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
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  settingsAPI,
  type ImageUpstreamCostAccountModelOverride,
  type ImageUpstreamCostAccountOverride,
  type ImageUpstreamCostModelOverride,
} from "@/api/admin/settings";
import AccountSelector from "@/components/common/AccountSelector.vue";
import ModelPicker from "@/views/admin/settings/ModelPicker.vue";

const { t } = useI18n();

// Tier keys must match the backend's canonical billing sizes.
const TIERS = ["1K", "2K", "4K"] as const;
// Image generation lives on the OpenAI-compatible platform today.
const PLATFORM = "openai";

interface AccountModelRow {
  accountId: number | null;
  model: string;
  tiers: Record<string, number | undefined>;
}
interface ModelRow {
  model: string;
  tiers: Record<string, number | undefined>;
}
interface AccountRow {
  accountId: number | null;
  costPerImage: number;
}

const loading = ref(true);
const saving = ref(false);
const saveMessage = ref("");
const saveError = ref(false);
const candidateModels = ref<string[]>([]);

const form = reactive({
  cost_per_image: 0,
  ignore_upstream_rate_snapshot: false,
  accountModelRows: [] as AccountModelRow[],
  modelRows: [] as ModelRow[],
  accountRows: [] as AccountRow[],
});

const accountModelRows = computed(() => form.accountModelRows);
const modelRows = computed(() => form.modelRows);
const accountRows = computed(() => form.accountRows);

function emptyTiers(): Record<string, number | undefined> {
  return {};
}

function addAccountModelRow() {
  form.accountModelRows.push({ accountId: null, model: "", tiers: emptyTiers() });
}
function addModelRow() {
  form.modelRows.push({ model: "", tiers: emptyTiers() });
}
function addAccountRow() {
  form.accountRows.push({ accountId: null, costPerImage: 0 });
}
function removeAccountModelRow(index: number) {
  form.accountModelRows.splice(index, 1);
}
function removeModelRow(index: number) {
  form.modelRows.splice(index, 1);
}
function removeAccountRow(index: number) {
  form.accountRows.splice(index, 1);
}

// Trim rows the backend would reject: a row without its key column, or without
// any tier filled, carries no information and is dropped rather than failing
// the whole save.
function buildTiers(tiers: Record<string, number | undefined>): Record<string, number> {
  const out: Record<string, number> = {};
  for (const tier of TIERS) {
    const value = tiers[tier];
    if (typeof value === "number" && Number.isFinite(value)) {
      out[tier] = value;
    }
  }
  return out;
}

function buildAccountModelOverrides(): ImageUpstreamCostAccountModelOverride[] {
  return form.accountModelRows
    .filter((row) => row.accountId !== null && row.model.trim() !== "")
    .map((row) => ({
      account_id: row.accountId as number,
      model: row.model.trim(),
      tiers: buildTiers(row.tiers),
    }))
    .filter((row) => Object.keys(row.tiers).length > 0);
}

function buildModelOverrides(): ImageUpstreamCostModelOverride[] {
  return form.modelRows
    .filter((row) => row.model.trim() !== "")
    .map((row) => ({ model: row.model.trim(), tiers: buildTiers(row.tiers) }))
    .filter((row) => Object.keys(row.tiers).length > 0);
}

function buildAccountOverrides(): ImageUpstreamCostAccountOverride[] {
  return form.accountRows
    .filter((row) => row.accountId !== null)
    .map((row) => ({ account_id: row.accountId as number, cost_per_image: row.costPerImage }));
}

async function load() {
  loading.value = true;
  try {
    const [settings, candidates] = await Promise.all([
      settingsAPI.getImageUpstreamCost(),
      // Candidates are a convenience only; a failure must not block the form.
      settingsAPI.getImageUpstreamCostCandidates().catch(() => ({ models: [], accounts: [] })),
    ]);
    form.cost_per_image = settings.cost_per_image;
    form.ignore_upstream_rate_snapshot = settings.ignore_upstream_rate_snapshot;
    form.accountModelRows = (settings.account_model_overrides ?? []).map((entry) => ({
      accountId: entry.account_id,
      model: entry.model,
      tiers: { ...entry.tiers },
    }));
    form.modelRows = (settings.model_overrides ?? []).map((entry) => ({
      model: entry.model,
      tiers: { ...entry.tiers },
    }));
    form.accountRows = (settings.account_overrides ?? []).map((entry) => ({
      accountId: entry.account_id,
      costPerImage: entry.cost_per_image,
    }));
    candidateModels.value = candidates.models ?? [];
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
      account_model_overrides: buildAccountModelOverrides(),
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
