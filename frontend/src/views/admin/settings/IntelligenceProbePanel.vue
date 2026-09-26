<template>
  <div class="space-y-4">
    <p class="text-sm text-gray-500 dark:text-gray-400">
      {{ t("admin.settings.intelligenceProbe.hint") }}
    </p>

    <div v-if="loading" class="py-8 text-center text-sm text-gray-500">
      {{ t("common.loading") }}
    </div>

    <template v-else>
      <!-- Runner switches -->
      <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
        <label class="flex items-start gap-3">
          <input v-model="form.enabled" type="checkbox" class="mt-1" />
          <span>
            <span class="block text-sm font-medium text-gray-700 dark:text-gray-200">
              {{ t("admin.settings.intelligenceProbe.enabled") }}
            </span>
            <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">
              {{ t("admin.settings.intelligenceProbe.enabledHint") }}
            </span>
          </span>
        </label>
      </div>

      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-200">
            {{ t("admin.settings.intelligenceProbe.intervalMinutes") }}
          </label>
          <input v-model.number="form.interval_minutes" type="number" min="1" max="1440" class="input w-32" />
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t("admin.settings.intelligenceProbe.intervalHint") }}
          </p>
        </div>
        <div>
          <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-200">
            {{ t("admin.settings.intelligenceProbe.retentionDays") }}
          </label>
          <input v-model.number="form.retention_days" type="number" min="1" max="30" class="input w-32" />
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t("admin.settings.intelligenceProbe.retentionHint") }}
          </p>
        </div>
      </div>

      <div>
        <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-200">
          {{ t("admin.settings.intelligenceProbe.promptOverride") }}
        </label>
        <textarea
          v-model="form.prompt_override"
          rows="2"
          class="input w-full"
          :placeholder="t('admin.settings.intelligenceProbe.promptPlaceholder')"
        ></textarea>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t("admin.settings.intelligenceProbe.promptHint") }}
        </p>
      </div>

      <!-- Target list: one row per group+model pair -->
      <div class="overflow-x-auto">
        <table class="w-full min-w-[40rem] text-sm">
          <thead>
            <tr class="border-b border-gray-200 text-left text-xs text-gray-500 dark:border-dark-600">
              <th class="py-2 pr-3 font-medium">{{ t("admin.settings.intelligenceProbe.colGroup") }}</th>
              <th class="py-2 pr-3 font-medium">{{ t("admin.settings.intelligenceProbe.colModel") }}</th>
              <th class="w-20 py-2 pr-3 font-medium">{{ t("admin.settings.intelligenceProbe.colEnabled") }}</th>
              <th class="w-20 py-2 font-medium"></th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(row, index) in targetRows"
              :key="index"
              class="border-b border-gray-100 dark:border-dark-700"
            >
              <td class="py-2 pr-3">
                <Select
                  v-model="row.group_id"
                  :options="groupOptions"
                  searchable
                  :placeholder="t('admin.settings.intelligenceProbe.groupPlaceholder')"
                />
              </td>
              <td class="py-2 pr-3">
                <ModelPicker v-model="row.model" :placeholder="t('admin.settings.intelligenceProbe.modelPlaceholder')" />
              </td>
              <td class="py-2 pr-3">
                <input v-model="row.enabled" type="checkbox" class="mt-1" />
              </td>
              <td class="py-2">
                <button
                  type="button"
                  class="btn btn-ghost btn-sm text-red-600"
                  @click="removeRow(index)"
                >
                  {{ t("common.delete") }}
                </button>
              </td>
            </tr>
            <tr v-if="targetRows.length === 0">
              <td colspan="4" class="py-4 text-center text-xs text-gray-400">
                {{ t("admin.settings.intelligenceProbe.emptyTargets") }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <button type="button" class="btn btn-secondary btn-sm" @click="addRow">
        {{ t("admin.settings.intelligenceProbe.addTarget") }}
      </button>

      <div v-if="saveMessage" class="text-sm" :class="saveError ? 'text-red-600' : 'text-green-600'">
        {{ saveMessage }}
      </div>

      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn btn-primary" :disabled="saving" @click="save">
          {{ saving ? t("common.saving") : t("common.save") }}
        </button>
        <button type="button" class="btn btn-secondary" :disabled="running || !loadedTargetIds.length" @click="runNow">
          {{ running ? t("admin.settings.intelligenceProbe.running") : t("admin.settings.intelligenceProbe.runNow") }}
        </button>
        <p v-if="loadedTargetIds.length" class="w-full text-xs text-gray-500 dark:text-gray-400">
          {{ t("admin.settings.intelligenceProbe.runNowHint") }}
        </p>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";
import {
  intelligenceProbeAPI,
  type IntelligenceProbeConfigUpdateRequest,
  type IntelligenceProbeTargetInput,
} from "@/api/admin/intelligenceProbe";
import { groupsAPI } from "@/api/admin/groups";
import { extractApiErrorMessage } from "@/utils/apiError";
import type { AdminGroup } from "@/types";
import Select from "@/components/common/Select.vue";
import ModelPicker from "@/views/admin/settings/ModelPicker.vue";

const { t } = useI18n();

// The probe only draws through OpenAI-platform groups today; the backend
// rejects anything else, so the picker is filtered client-side as well.
const PLATFORM = "openai";

const loading = ref(true);
const saving = ref(false);
const running = ref(false);
const saveMessage = ref("");
const saveError = ref(false);
const groups = ref<AdminGroup[]>([]);
const loadedTargetIds = ref<number[]>([]);

interface TargetRow {
  id?: number;
  group_id: number | null;
  model: string;
  enabled: boolean;
}

const form = reactive({
  enabled: false,
  interval_minutes: 12,
  retention_days: 1,
  prompt_override: "",
});
const targetRows = ref<TargetRow[]>([]);

const groupOptions = computed(() =>
  groups.value
    .filter((group) => group.platform === PLATFORM)
    .map((group) => ({ value: group.id, label: group.name })),
);

function addRow() {
  targetRows.value.push({ group_id: null, model: "", enabled: true });
}
function removeRow(index: number) {
  targetRows.value.splice(index, 1);
}

function buildTargets(): IntelligenceProbeTargetInput[] {
  return targetRows.value
    .filter((row) => row.group_id !== null && row.model.trim() !== "")
    .map((row) => ({
      id: row.id,
      group_id: row.group_id as number,
      model: row.model.trim(),
      enabled: row.enabled,
    }));
}

async function load() {
  loading.value = true;
  try {
    const [config, groupList] = await Promise.all([
      intelligenceProbeAPI.getConfig(),
      // A groups failure must not block the form; the rows stay editable.
      groupsAPI.getAll().catch(() => [] as AdminGroup[]),
    ]);
    groups.value = groupList;
    form.enabled = config.settings.enabled;
    form.interval_minutes = config.settings.interval_minutes;
    form.retention_days = config.settings.retention_days;
    form.prompt_override = config.settings.prompt_override;
    targetRows.value = config.targets.map((target) => ({
      id: target.id,
      group_id: target.group_id,
      model: target.model,
      enabled: target.enabled,
    }));
    loadedTargetIds.value = config.targets.filter((target) => target.enabled).map((target) => target.id);
  } catch (error) {
    saveError.value = true;
    saveMessage.value = extractApiErrorMessage(error, t("admin.settings.intelligenceProbe.loadFailed"));
  } finally {
    loading.value = false;
  }
}

async function save() {
  saving.value = true;
  saveMessage.value = "";
  saveError.value = false;
  try {
    const request: IntelligenceProbeConfigUpdateRequest = {
      settings: {
        enabled: form.enabled,
        interval_minutes: form.interval_minutes,
        retention_days: form.retention_days,
        prompt_override: form.prompt_override,
      },
      targets: buildTargets(),
    };
    const config = await intelligenceProbeAPI.updateConfig(request);
    loadedTargetIds.value = config.targets.filter((target) => target.enabled).map((target) => target.id);
    saveMessage.value = t("admin.settings.intelligenceProbe.saved");
    await load();
  } catch (error) {
    saveError.value = true;
    saveMessage.value = extractApiErrorMessage(error, t("admin.settings.intelligenceProbe.saveFailed"));
  } finally {
    saving.value = false;
  }
}

async function runNow() {
  running.value = true;
  saveMessage.value = "";
  saveError.value = false;
  try {
    // One request per saved target: a single run can take up to two minutes
    // (the model is actually drawing), so we never batch them into one HTTP
    // call that would run past the client timeout.
    let done = 0;
    for (const targetId of loadedTargetIds.value) {
      await intelligenceProbeAPI.run(targetId);
      done += 1;
    }
    saveMessage.value = t("admin.settings.intelligenceProbe.runDone", { count: done });
  } catch (error) {
    saveError.value = true;
    saveMessage.value = extractApiErrorMessage(error, t("admin.settings.intelligenceProbe.runFailed"));
  } finally {
    running.value = false;
  }
}

onMounted(load);
</script>
