import apiClient from './client'

export interface RoutingConfig {
  id: number
  judge_model_id: number | null
  default_model_id: number | null
  enable_next_model_directive: boolean
  session_ttl_seconds: number
  judge_max_input_chars: number
}

export async function getRoutingConfig(): Promise<RoutingConfig> {
  const { data } = await apiClient.get('/admin/routing')
  return data
}

export async function updateRoutingConfig(rc: Partial<RoutingConfig>): Promise<RoutingConfig> {
  const { data } = await apiClient.put('/admin/routing', rc)
  return data
}
