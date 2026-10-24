/**
 * Leaderboard API endpoints
 */

import { apiClient } from './client'

export type LeaderboardPeriod = 'today' | 'week' | 'month' | 'all'

export interface LeaderboardEntry {
  rank: number
  user_id: number
  display_name: string
  avatar?: string
  total_tokens: number
  requests: number
  cost: number
  is_current_user: boolean
}

export interface LeaderboardParams {
  period?: LeaderboardPeriod
  limit?: number
  timezone?: string
}

export interface LeaderboardResponse {
  leaderboard: LeaderboardEntry[]
  period: string
  start_date: string
  end_date: string
}

/**
 * Get leaderboard
 * @param params Query parameters
 * @returns Leaderboard data
 */
export async function getLeaderboard(params?: LeaderboardParams): Promise<LeaderboardResponse> {
  const { data } = await apiClient.get<LeaderboardResponse>('/leaderboard', { params })
  return data
}
