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
  UsageRecordPage,
  UsageRecordSummary,
} from './types'

type ApiResponse<T> = {
  success: boolean
  message: string
  data: T
}

export type UsageRecordExportTask = {
  task_id: string
  status: 'pending' | 'running' | 'succeeded' | 'failed'
  state?: {
    progress?: number
    processed?: number
    total?: number
    stage?: string
  }
  result?: {
    file_name?: string
    record_count?: number
    size?: number
  }
  error?: string
}

function usageRecordParams(filters: UsageRecordFilters) {
  const params = new URLSearchParams()
  Object.entries(filters).forEach(([key, value]) => {
    const normalized = value.trim()
    if (normalized) params.set(key, normalized)
  })
  return params
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
  instanceId: number,
  taskId: string
): Promise<ApiResponse<UsageRecordExportTask>> {
  const response = await api.get<ApiResponse<UsageRecordExportTask>>(
    `/api/managed-instances/${instanceId}/usage-records/exports/${encodeURIComponent(taskId)}`,
    { disableDuplicate: true }
  )
  return response.data
}

export async function downloadUsageRecordsExport(
  instanceId: number,
  taskId: string
) {
  const response = await api.get(
    `/api/managed-instances/${instanceId}/usage-records/exports/${encodeURIComponent(taskId)}/download`,
    { responseType: 'blob', disableDuplicate: true }
  )
  const disposition = String(response.headers['content-disposition'] ?? '')
  const filename =
    disposition.match(/filename="?([^";]+)"?/i)?.[1] ??
    `usage-records-${instanceId}.csv`
  const objectUrl = URL.createObjectURL(response.data)
  const anchor = document.createElement('a')
  anchor.href = objectUrl
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(objectUrl)
}
