import apiClient from './client'

export interface Attempt {
  type?: string
  model: string
  provider: string
  success: boolean
  status: number
  error?: string
  latency_ms: number
}

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
  retry_count: number
  served_model: string
  served_provider: string
  failover_count: number
  trace: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cache_tokens: number
  // Judge call diagnostics (populated only when the judge was invoked).
  judge_model: string
  judge_latency_ms: number
  judge_prompt_tokens: number
  judge_completion_tokens: number
  judge_total_tokens: number
  judge_cache_tokens: number
  created_at: string
  provider_name: string
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

export interface TokenStatRow {
  model: string
  provider: string
  count: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cache_tokens: number
}

export interface Stats {
  total: number
  by_reason: { Reason: string; Count: number }[]
  tokens: { total: number; prompt: number; completion: number; cache: number }
  by_model: TokenStatRow[]
  by_provider: TokenStatRow[]
}

export async function listLogs(params: ListLogsParams): Promise<ListLogsResponse> {
  const { data } = await apiClient.get('/admin/logs', { params })
  return data
}

export async function clearLogs(): Promise<void> {
  await apiClient.delete('/admin/logs')
}

export async function getStats(): Promise<Stats> {
  const { data } = await apiClient.get('/admin/stats')
  return data
}

export interface DailyUsageRow {
  date: string
  request_count: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cache_tokens: number
}

export interface DailyStatsParams {
  provider?: string
  model?: string
  days?: number
}

export async function getDailyStats(params: DailyStatsParams): Promise<DailyUsageRow[]> {
  const { data } = await apiClient.get('/admin/stats/daily', { params })
  return data.data
}

export interface DailyUsageByModelRow {
  date: string
  model: string
  provider: string
  request_count: number
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cache_tokens: number
}

export async function getDailyStatsByModel(params: DailyStatsParams): Promise<DailyUsageByModelRow[]> {
  const { data } = await apiClient.get('/admin/stats/daily/models', { params })
  return data.data
}
