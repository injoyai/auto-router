import apiClient from './client'

export interface RoutingConfig {
  id: number
  judge_model_id: number | null
  default_group_id: number | null
  judge_max_input_chars: number
  gateway_token: string
}

export async function getRoutingConfig(): Promise<RoutingConfig> {
  const { data } = await apiClient.get('/admin/routing')
  return data
}

export async function updateRoutingConfig(rc: Partial<RoutingConfig>): Promise<RoutingConfig> {
  const { data } = await apiClient.put('/admin/routing', rc)
  return data
}
