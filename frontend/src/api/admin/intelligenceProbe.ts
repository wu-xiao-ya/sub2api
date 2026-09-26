/**
 * Admin API for the GPT intelligence degradation probe (pelican SVG test).
 * The probe periodically draws a pelican through a configured group and keeps
 * the returned SVGs so an operator can compare quality over time.
 */

import { apiClient } from "../client";

export interface IntelligenceProbeSettings {
  enabled: boolean;
  interval_minutes: number;
  retention_days: number;
  prompt_override: string;
}

export interface IntelligenceProbeTarget {
  id: number;
  group_id: number;
  group_name: string;
  group_platform: string;
  model: string;
  enabled: boolean;
  last_run_at: string | null;
  next_run_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface IntelligenceProbeTargetInput {
  id?: number;
  group_id: number;
  model: string;
  enabled: boolean;
}

export interface IntelligenceProbeConfig {
  settings: IntelligenceProbeSettings;
  targets: IntelligenceProbeTarget[];
}

export interface IntelligenceProbeConfigUpdateRequest {
  settings: IntelligenceProbeSettings;
  targets: IntelligenceProbeTargetInput[];
}

export type IntelligenceProbeResultStatus = "success" | "no_svg" | "failed";

export interface IntelligenceProbeResult {
  id: number;
  target_id: number;
  status: IntelligenceProbeResultStatus;
  response_svg: string;
  response_excerpt: string;
  error_message: string;
  latency_ms: number | null;
  created_at: string;
}

export interface IntelligenceProbeRunResponse {
  results: IntelligenceProbeResult[];
}

export async function getIntelligenceProbeConfig(): Promise<IntelligenceProbeConfig> {
  const { data } = await apiClient.get<IntelligenceProbeConfig>(
    "/admin/intelligence-probe/config",
  );
  return data;
}

export async function updateIntelligenceProbeConfig(
  request: IntelligenceProbeConfigUpdateRequest,
): Promise<IntelligenceProbeConfig> {
  const { data } = await apiClient.put<IntelligenceProbeConfig>(
    "/admin/intelligence-probe/config",
    request,
  );
  return data;
}

export async function runIntelligenceProbe(
  targetId?: number,
): Promise<IntelligenceProbeRunResponse> {
  // One drawing run can legitimately take up to two minutes server-side, so
  // this call overrides the short global client timeout.
  const { data } = await apiClient.post<IntelligenceProbeRunResponse>(
    "/admin/intelligence-probe/run",
    targetId ? { target_id: targetId } : {},
    { timeout: 300000 },
  );
  return data;
}

export async function listIntelligenceProbeResults(
  targetId: number,
  limit = 120,
): Promise<{ items: IntelligenceProbeResult[] }> {
  const { data } = await apiClient.get<{ items: IntelligenceProbeResult[] }>(
    "/admin/intelligence-probe/results",
    { params: { target_id: targetId, limit } },
  );
  return data;
}

export const intelligenceProbeAPI = {
  getConfig: getIntelligenceProbeConfig,
  updateConfig: updateIntelligenceProbeConfig,
  run: runIntelligenceProbe,
  listResults: listIntelligenceProbeResults,
};
