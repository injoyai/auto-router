import apiClient from './client'

export interface LoginResponse {
  token: string
  expires_in: number
}

export async function login(adminToken: string): Promise<LoginResponse> {
  const { data } = await apiClient.post('/admin/login', { token: adminToken })
  return data
}
