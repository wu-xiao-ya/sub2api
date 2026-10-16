import { apiClient } from '../client'

export interface HostSample {
  at: number
  generated_at: string
  cpu: number
  mem: number
  disk: number
  mem_used: number
  mem_total: number
  disk_used: number
  disk_total: number
  rx: number
  tx: number
}

export interface HostSummary {
  generated_at: string
  host: { hostname: string; kernel: string; uptime_seconds: number; load: number[] }
  cpu: { percent: number; cores: number }
  memory: { used: number; total: number; available: number; percent: number }
  disk: { used: number; total: number; free: number; percent: number }
  samples: HostSample[]
  services: { name: string; ok: boolean; active: string; health?: string }[]
  probes: { name: string; ok: boolean; status: number; ms: number; body?: string }[]
  containers: { name: string; image: string; state: string; status: string; ports: string }[]
  ports: string[]
}

export type LogService = 'sub2api' | 'postgres' | 'redis' | 'caddy' | 'server-monitor'
export interface HostLogs { service: string; lines: string; ok: boolean; error?: string }

export async function getServerSummary(signal?: AbortSignal): Promise<HostSummary> {
  const { data } = await apiClient.get<HostSummary>('/admin/server-monitor/summary', { signal })
  return data
}

export async function getServerLogs(service: LogService, lines: number, signal?: AbortSignal): Promise<HostLogs> {
  const { data } = await apiClient.get<HostLogs>('/admin/server-monitor/logs', { params: { service, lines }, signal })
  return data
}
