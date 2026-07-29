import apiClient from './client'

export interface RequestLog {
  id: number
  session_id: string
  client_protocol: string
  requested_model: string
  routed_model: string
  route_reason: string
  judge_raw: string
  status: number
  latency_ms: number
  error: string
  created_at: string
}

export interface ListLogsParams {
  page?: number
  page_size?: number
  reason?: string
  model?: string
}

export interface ListLogsResponse {
  data: RequestLog[]
  total: number
  page: number
  page_size: number
}

export interface Stats {
  total: number
  by_reason: { Reason: string; Count: number }[]
}

export async function listLogs(params: ListLogsParams): Promise<ListLogsResponse> {
  const { data } = await apiClient.get('/admin/logs', { params })
  return data
}

export async function getStats(): Promise<Stats> {
  const { data } = await apiClient.get('/admin/stats')
  return data
}
