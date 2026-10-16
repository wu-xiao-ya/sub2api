import { apiClient } from '../client'

export type FinanceLedgerSource =
  | 'payment'
  | 'refund'
  | 'redeem'
  | 'admin_adjustment'
  | 'affiliate_transfer'

export type FinanceLedgerDirection = 'income' | 'deduction'

export interface FinanceLedgerEntry {
  id: string
  source: FinanceLedgerSource
  occurred_at: string
  user_id: number
  user_email: string
  user_name: string
  amount: number
  direction: FinanceLedgerDirection
  reference: string
  payment_type?: string
  notes?: string
  status: string
}

export interface FinanceLedgerSummary {
  income: number
  deduction: number
  net: number
  count: number
}

export interface FinanceLedgerResponse {
  items: FinanceLedgerEntry[]
  total: number
  page: number
  page_size: number
  start_date: string
  end_date: string
  summary: FinanceLedgerSummary
}

export interface FinanceLedgerQuery {
  page?: number
  page_size?: number
  start_date?: string
  end_date?: string
  timezone?: string
  user?: string
  exclude_users?: string
  source?: FinanceLedgerSource | ''
  direction?: FinanceLedgerDirection | ''
  payment_type?: string
  keyword?: string
}

export async function getLedger(params: FinanceLedgerQuery): Promise<FinanceLedgerResponse> {
  const { data } = await apiClient.get<FinanceLedgerResponse>('/admin/finance/ledger', { params })
  return data
}

export async function exportLedger(params: FinanceLedgerQuery): Promise<Blob> {
  const { data } = await apiClient.get('/admin/finance/ledger/export', {
    params,
    responseType: 'blob',
  })
  return data as Blob
}

const financeAPI = {
  getLedger,
  exportLedger,
}

export default financeAPI
