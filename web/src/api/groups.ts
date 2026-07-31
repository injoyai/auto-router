import apiClient from './client'
import type { Model } from './models'

export interface ModelGroup {
  id: number
  name: string
  display_name: string
  description: string
  enabled: boolean
  item_count?: number
}

export interface GroupItem {
  id: number
  group_id: number
  model_id: number
  position: number
  model?: Model
}

export async function listGroups(): Promise<ModelGroup[]> {
  const { data } = await apiClient.get('/admin/groups')
  return data.data
}

export async function createGroup(g: Partial<ModelGroup>): Promise<ModelGroup> {
  const { data } = await apiClient.post('/admin/groups', g)
  return data
}

export async function updateGroup(id: number, g: Partial<ModelGroup>): Promise<ModelGroup> {
  const { data } = await apiClient.put(`/admin/groups/${id}`, g)
  return data
}

export async function deleteGroup(id: number): Promise<void> {
  await apiClient.delete(`/admin/groups/${id}`)
}

export async function listGroupItems(id: number): Promise<GroupItem[]> {
  const { data } = await apiClient.get(`/admin/groups/${id}/items`)
  return data.data
}

export async function replaceGroupItems(id: number, modelIds: number[]): Promise<void> {
  await apiClient.put(`/admin/groups/${id}/items`, { items: modelIds })
}
