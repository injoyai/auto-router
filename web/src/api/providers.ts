import apiClient from './client'

export interface Provider {
  id: number
  name: string
  base_url: string
  /** 仅用于请求体；后端 Provider.APIKey 标记 json:"-"，响应中不会返回。 */
  api_key?: string
  /** 后端返回，表示是否已保存 API Key */
  has_api_key?: boolean
  protocol: string
  enabled: boolean
}

export interface TestResult {
  ok: boolean
  status: number
  error?: string
}

export async function listProviders(): Promise<Provider[]> {
  const { data } = await apiClient.get('/admin/providers')
  return data.data
}

export async function createProvider(p: Partial<Provider>): Promise<Provider> {
  const { data } = await apiClient.post('/admin/providers', p)
  return data
}

export async function updateProvider(id: number, p: Partial<Provider>): Promise<Provider> {
  const { data } = await apiClient.put(`/admin/providers/${id}`, p)
  return data
}

export async function deleteProvider(id: number): Promise<void> {
  await apiClient.delete(`/admin/providers/${id}`)
}

export async function testProvider(id: number): Promise<TestResult> {
  const { data } = await apiClient.post(`/admin/providers/${id}/test`)
  return data
}
