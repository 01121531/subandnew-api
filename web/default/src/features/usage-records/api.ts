/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { api } from '@/lib/api'

import type {
  UsageRecordFilters,
  UsageRecordFilterOptions,
  UsageRecordPage,
  UsageRecordSummary,
} from './types'

type ApiResponse<T> = {
  success: boolean
  message: string
  data: T
}

export type UsageRecordExportTask = {
  id: number
  task_id: string
  instance_id: number
  instance_name: string
  instance_kind: string
  actor_id: number
  actor_name: string
  filters: Record<string, string[]>
  status:
    | 'pending'
    | 'running'
    | 'succeeded'
    | 'failed'
    | 'cancelled'
    | 'expired'
  queue_position: number
  progress: number
  processed: number
  total: number
  file_name: string
  file_size: number
  record_count: number
  error_code: string
  started_at: number
  finished_at: number
  expires_at: number
  created_at: number
  updated_at: number
}

export type UsageRecordExportList = {
  items: UsageRecordExportTask[]
  total: number
  page: number
  page_size: number
  has_active: boolean
}

function usageRecordParams(filters: UsageRecordFilters) {
  const params = new URLSearchParams()
  Object.entries(filters).forEach(([key, value]) => {
    const values = Array.isArray(value) ? value : [value]
    values.forEach((item) => {
      const normalized = item.trim()
      if (normalized) params.append(key, normalized)
    })
  })
  return params
}

export async function getUsageRecordFilterOptions(
  instanceId: number,
  filters: UsageRecordFilters
): Promise<ApiResponse<UsageRecordFilterOptions>> {
  const params = usageRecordParams(filters)
  const response = await api.get<ApiResponse<UsageRecordFilterOptions>>(
    `/api/managed-instances/${instanceId}/usage-records/filter-options?${params.toString()}`,
    { disableDuplicate: true }
  )
  return response.data
}

export async function getUsageRecords(
  instanceId: number,
  filters: UsageRecordFilters
): Promise<ApiResponse<UsageRecordPage>> {
  const params = usageRecordParams(filters)
  const response = await api.get<ApiResponse<UsageRecordPage>>(
    `/api/managed-instances/${instanceId}/usage-records?${params.toString()}`,
    { disableDuplicate: true }
  )
  return response.data
}

export async function getUsageRecordSummary(
  instanceId: number,
  filters: UsageRecordFilters
): Promise<ApiResponse<UsageRecordSummary>> {
  const params = usageRecordParams(filters)
  const response = await api.get<ApiResponse<UsageRecordSummary>>(
    `/api/managed-instances/${instanceId}/usage-records/summary?${params.toString()}`,
    { disableDuplicate: true }
  )
  return response.data
}

export async function createUsageRecordsExport(
  instanceId: number,
  filters: UsageRecordFilters
): Promise<ApiResponse<UsageRecordExportTask>> {
  const params = usageRecordParams(filters)
  params.delete('p')
  params.delete('page')
  params.delete('page_size')
  const response = await api.post<ApiResponse<UsageRecordExportTask>>(
    `/api/managed-instances/${instanceId}/usage-records/exports?${params.toString()}`,
    null,
    { disableDuplicate: true }
  )
  return response.data
}

export async function getUsageRecordsExport(
  taskId: string
): Promise<ApiResponse<UsageRecordExportTask>> {
  const response = await api.get<ApiResponse<UsageRecordExportTask>>(
    `/api/managed-usage-exports/${encodeURIComponent(taskId)}`,
    { disableDuplicate: true }
  )
  return response.data
}

export async function downloadUsageRecordsExport(taskId: string) {
  const response = await api.get(
    `/api/managed-usage-exports/${encodeURIComponent(taskId)}/download`,
    { responseType: 'blob', disableDuplicate: true }
  )
  const disposition = String(response.headers['content-disposition'] ?? '')
  const filename =
    disposition.match(/filename="?([^";]+)"?/i)?.[1] ??
    `usage-records-${taskId}.csv`
  const objectUrl = URL.createObjectURL(response.data)
  const anchor = document.createElement('a')
  anchor.href = objectUrl
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(objectUrl)
}

export async function listUsageRecordsExports(filters: {
  page: number
  page_size: number
  status?: string
  instance_id?: number
  actor_id?: number
}): Promise<ApiResponse<UsageRecordExportList>> {
  const params = new URLSearchParams()
  Object.entries(filters).forEach(([key, value]) => {
    if (value !== undefined && value !== '' && value !== 0) {
      params.set(key, String(value))
    }
  })
  const response = await api.get<ApiResponse<UsageRecordExportList>>(
    `/api/managed-usage-exports?${params.toString()}`,
    { disableDuplicate: true }
  )
  return response.data
}

export async function cancelUsageRecordsExport(taskId: string) {
  const response = await api.post<ApiResponse<unknown>>(
    `/api/managed-usage-exports/${encodeURIComponent(taskId)}/cancel`,
    null,
    { disableDuplicate: true }
  )
  return response.data
}

export async function retryUsageRecordsExport(taskId: string) {
  const response = await api.post<ApiResponse<UsageRecordExportTask>>(
    `/api/managed-usage-exports/${encodeURIComponent(taskId)}/retry`,
    null,
    { disableDuplicate: true }
  )
  return response.data
}

export async function deleteUsageRecordsExport(taskId: string) {
  const response = await api.delete<ApiResponse<unknown>>(
    `/api/managed-usage-exports/${encodeURIComponent(taskId)}`,
    { disableDuplicate: true }
  )
  return response.data
}
