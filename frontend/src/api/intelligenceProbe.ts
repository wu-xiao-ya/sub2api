/**
 * User-facing intelligence degradation probe results (read-only gallery).
 */

import { apiClient } from "./client";
import type { IntelligenceProbeResultStatus } from "./admin/intelligenceProbe";

export interface IntelligenceProbeUserResult {
  id: number;
  target_id: number;
  group_id: number;
  group_name: string;
  model: string;
  status: IntelligenceProbeResultStatus;
  response_svg: string;
  response_excerpt: string;
  error_message: string;
  latency_ms: number | null;
  created_at: string;
}

export async function listIntelligenceProbeUserResults(
  limit = 120,
): Promise<{ items: IntelligenceProbeUserResult[] }> {
  const { data } = await apiClient.get<{ items: IntelligenceProbeUserResult[] }>(
    "/intelligence-probe/results",
    { params: { limit } },
  );
  return data;
}

export const intelligenceProbeAPI = {
  listResults: listIntelligenceProbeUserResults,
};
