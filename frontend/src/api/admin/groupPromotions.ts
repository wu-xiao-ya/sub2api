import { apiClient } from '../client'
import type {
  BasePaginationResponse,
  CreateGroupPromotionRequest,
  GroupPromotion,
  UpdateGroupPromotionRequest,
} from '@/types'

export async function list(
  page: number = 1,
  pageSize: number = 20,
  filters?: {
    group_id?: number
    enabled?: boolean
    search?: string
    sort_by?: string
    sort_order?: 'asc' | 'desc'
  },
  options?: { signal?: AbortSignal },
): Promise<BasePaginationResponse<GroupPromotion>> {
  const { data } = await apiClient.get<BasePaginationResponse<GroupPromotion>>('/admin/group-promotions', {
    params: { page, page_size: pageSize, ...filters },
    signal: options?.signal,
  })
  return data
}

export async function getById(id: number): Promise<GroupPromotion> {
  const { data } = await apiClient.get<GroupPromotion>(`/admin/group-promotions/${id}`)
  return data
}

export async function create(request: CreateGroupPromotionRequest): Promise<GroupPromotion> {
  const { data } = await apiClient.post<GroupPromotion>('/admin/group-promotions', request)
  return data
}

export async function update(id: number, request: UpdateGroupPromotionRequest): Promise<GroupPromotion> {
  const { data } = await apiClient.put<GroupPromotion>(`/admin/group-promotions/${id}`, request)
  return data
}

export async function deleteGroupPromotion(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/admin/group-promotions/${id}`)
  return data
}

const groupPromotionsAPI = {
  list,
  getById,
  create,
  update,
  delete: deleteGroupPromotion,
}

export default groupPromotionsAPI
