import apiClient from './client'

export interface Model {
  id: number
  name: string
  display_name: string
  provider_id: number
  description: string
  enabled: boolean
  is_judge: boolean
}

export async function listModels(): Promise<Model[]> {
  const { data } = await apiClient.get('/admin/models')
  return data.data
}

export async function createModel(m: Partial<Model>): Promise<Model> {
  const { data } = await apiClient.post('/admin/models', m)
  return data
}

export async function updateModel(id: number, m: Partial<Model>): Promise<Model> {
  const { data } = await apiClient.put(`/admin/models/${id}`, m)
  return data
}

export async function deleteModel(id: number): Promise<void> {
  await apiClient.delete(`/admin/models/${id}`)
}

export async function setJudgeModel(id: number): Promise<void> {
  await apiClient.post(`/admin/models/${id}/judge`)
}

export interface ModelTestResult {
  ok: boolean
  status: number
  latency_ms: number
  error?: string
}

export async function testModel(id: number): Promise<ModelTestResult> {
  const { data } = await apiClient.post(`/admin/models/${id}/test`)
  return data
}
