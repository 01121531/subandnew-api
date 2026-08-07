import { api } from '@/lib/api'

import type {
  ApiResponse,
  ManagedInstance,
  ManagedInstanceAuditList,
  ManagedInstanceCredential,
  ManagedInstanceCredentialInput,
  ManagedInstanceFilters,
  ManagedInstanceInput,
  ManagedInstanceList,
  ManagedInstanceTask,
} from './types'

export async function getManagedInstances(
  filters: ManagedInstanceFilters
): Promise<ApiResponse<ManagedInstanceList>> {
  const pageSize = 100
  const params = new URLSearchParams({ page: '1', page_size: String(pageSize) })
  if (filters.search) params.set('search', filters.search)
  if (filters.kind) params.set('kind', filters.kind)
  if (filters.status) params.set('status', filters.status)
  const firstResponse = await api.get<ApiResponse<ManagedInstanceList>>(
    `/api/managed-instances?${params.toString()}`
  )
  const first = firstResponse.data
  if (!first.success || first.data.items.length >= first.data.total) return first

  const pageCount = Math.ceil(first.data.total / pageSize)
  const remaining = await Promise.all(
    Array.from({ length: pageCount - 1 }, async (_, index) => {
      const pageParams = new URLSearchParams(params)
      pageParams.set('page', String(index + 2))
      const response = await api.get<ApiResponse<ManagedInstanceList>>(
        `/api/managed-instances?${pageParams.toString()}`
      )
      return response.data.data.items
    })
  )
  const items = [first.data.items, ...remaining].flat()
  return {
    ...first,
    data: { ...first.data, items, page: 1, page_size: items.length },
  }
}

export async function getManagedInstance(
  id: number
): Promise<ApiResponse<ManagedInstance>> {
  const response = await api.get(`/api/managed-instances/${id}`)
  return response.data
}

export async function getManagedInstanceAudits(
  id: number
): Promise<ApiResponse<ManagedInstanceAuditList>> {
  const response = await api.get(
    `/api/managed-instances/${id}/audits?page=1&page_size=20`
  )
  return response.data
}

export async function getManagedInstanceTask(
  id: number,
  taskId: string
): Promise<ApiResponse<ManagedInstanceTask>> {
  const response = await api.get(
    `/api/managed-instances/${id}/tasks/${encodeURIComponent(taskId)}`
  )
  return response.data
}

export async function createManagedInstance(
  input: ManagedInstanceInput
): Promise<ApiResponse<ManagedInstance>> {
  const response = await api.post('/api/managed-instances', input)
  return response.data
}

export async function updateManagedInstance(
  id: number,
  input: ManagedInstanceInput
): Promise<ApiResponse<ManagedInstance>> {
  const response = await api.put(`/api/managed-instances/${id}`, input)
  return response.data
}

export async function rotateManagedInstanceCredential(
  id: number,
  input: ManagedInstanceCredentialInput
): Promise<ApiResponse<ManagedInstanceCredential>> {
  const response = await api.put(
    `/api/managed-instances/${id}/credential`,
    input
  )
  return response.data
}

export async function checkManagedInstance(
  id: number
): Promise<ApiResponse<ManagedInstanceTask>> {
  const response = await api.post(`/api/managed-instances/${id}/check`)
  return response.data
}

export async function deleteManagedInstance(
  id: number
): Promise<ApiResponse<{ id: number }>> {
  const response = await api.delete(`/api/managed-instances/${id}`)
  return response.data
}
