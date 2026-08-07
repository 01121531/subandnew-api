import { api } from '@/lib/api'

import type {
  SystemOptionsResponse,
  SystemTaskListResponse,
  UpdateOptionRequest,
  UpdateOptionResponse,
} from './types'

export async function getSystemOptions() {
  const res = await api.get<SystemOptionsResponse>('/api/option/')
  return res.data
}

export async function listSystemTasks(limit = 50) {
  const res = await api.get<SystemTaskListResponse>('/api/system-task/list', {
    params: { limit },
  })
  return res.data
}

export async function updateSystemOption(request: UpdateOptionRequest) {
  const res = await api.put<UpdateOptionResponse>('/api/option/', request)
  return res.data
}
