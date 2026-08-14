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
  ApiResponse,
  ManagedInstance,
  ManagedInstanceAuditList,
  ManagedInstanceBatchExecuteInput,
  ManagedInstanceBatchPlanInput,
  ManagedInstanceBatchView,
  ManagedInstanceCredential,
  ManagedInstanceCredentialInput,
  ManagedInstanceFilters,
  ManagedInstanceAlertList,
  ManagedInstanceAccountOutput,
  ManagedAccountRangeInput,
  ManagedAccountRefreshView,
  ManagedAccountSnapshotView,
  ManagedInstanceInventoryPage,
  ManagedInstanceInput,
  ManagedInstanceList,
  ManagedInstanceObservation,
  ManagedInstanceOperation,
  ManagedInstanceOperationExecuteInput,
  ManagedInstanceOperationExecution,
  ManagedInstanceOperationPlanInput,
  ManagedInstancePreflight,
  ManagedInstanceRealtimeMetrics,
  ManagedInstanceRPMHistory,
  ManagedInstanceRPMHistoryBucket,
  ManagedInstanceTask,
  ManagedInstanceSummary,
  ManagedConfigBinding,
  ManagedConfigMode,
  ManagedConfigPreview,
  ManagedConfigSchema,
  ManagedConfigTemplate,
  ManagedConfigTemplateInput,
  ManagedConfigTemplateList,
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
  if (!first.success || first.data.items.length >= first.data.total) {
    return first
  }

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
  const response = await api.post('/api/managed-instances', input, {
    skipErrorHandler: true,
  })
  return response.data
}

export async function probeManagedInstance(
  input: ManagedInstanceInput
): Promise<ApiResponse<ManagedInstancePreflight>> {
  const response = await api.post('/api/managed-instances/probe', input)
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

export async function getManagedInstanceInventory(
  id: number,
  resource = 'auto',
  cursor = '',
  options?: { silent?: boolean }
): Promise<
  ApiResponse<ManagedInstanceObservation<ManagedInstanceInventoryPage>>
> {
  const params = new URLSearchParams({ resource })
  if (cursor) params.set('cursor', cursor)
  const response = await api.get(
    `/api/managed-instances/${id}/inventory?${params.toString()}`,
    {
      disableDuplicate: true,
      skipBusinessError: options?.silent,
      skipErrorHandler: options?.silent,
    }
  )
  return response.data
}

export async function getManagedInstanceMetrics(
  id: number,
  window?: { start: number; end: number },
  options?: { silent?: boolean }
): Promise<ApiResponse<ManagedInstanceObservation<ManagedInstanceSummary>>> {
  const params = window
    ? new URLSearchParams({
        start: String(window.start),
        end: String(window.end),
      })
    : null
  const response = await api.get(
    `/api/managed-instances/${id}/metrics${params ? `?${params.toString()}` : ''}`,
    {
      disableDuplicate: true,
      skipBusinessError: options?.silent,
      skipErrorHandler: options?.silent,
    }
  )
  return response.data
}

export async function getManagedInstanceRealtimeMetrics(
  id: number,
  options?: { silent?: boolean }
): Promise<
  ApiResponse<ManagedInstanceObservation<ManagedInstanceRealtimeMetrics>>
> {
  const response = await api.get(
    `/api/managed-instances/${id}/realtime-metrics`,
    {
      disableDuplicate: true,
      skipBusinessError: options?.silent,
      skipErrorHandler: options?.silent,
    }
  )
  return response.data
}

export async function getManagedInstanceRPMHistory(
  ids: number[],
  bucket: ManagedInstanceRPMHistoryBucket,
  options?: { silent?: boolean }
): Promise<ApiResponse<ManagedInstanceRPMHistory>> {
  const params = new URLSearchParams({
    ids: ids.join(','),
    bucket,
  })
  const response = await api.get(
    `/api/managed-instances/realtime-history?${params.toString()}`,
    {
      disableDuplicate: true,
      skipBusinessError: options?.silent,
      skipErrorHandler: options?.silent,
    }
  )
  return response.data
}

export async function getManagedInstanceAccountOutput(
  id: number,
  window: { start: number; end: number },
  options?: { silent?: boolean }
): Promise<
  ApiResponse<ManagedInstanceObservation<ManagedInstanceAccountOutput>>
> {
  const params = new URLSearchParams({
    start: String(window.start),
    end: String(window.end),
  })
  const response = await api.get(
    `/api/managed-instances/${id}/account-output?${params.toString()}`,
    {
      disableDuplicate: true,
      skipBusinessError: options?.silent,
      skipErrorHandler: options?.silent,
    }
  )
  return response.data
}

function managedAccountRangeParams(input: ManagedAccountRangeInput) {
  const params = new URLSearchParams()
  if (input.preset_days) params.set('preset_days', String(input.preset_days))
  if (input.start) params.set('start', String(input.start))
  if (input.end) params.set('end', String(input.end))
  if (input.timezone) params.set('timezone', input.timezone)
  return params
}

export async function getManagedAccountSnapshot(
  id: number,
  input: ManagedAccountRangeInput,
  options?: { silent?: boolean }
): Promise<ApiResponse<ManagedAccountSnapshotView>> {
  const params = managedAccountRangeParams(input)
  const response = await api.get(
    `/api/managed-instances/${id}/account-management/snapshot?${params.toString()}`,
    {
      disableDuplicate: true,
      skipBusinessError: options?.silent,
      skipErrorHandler: options?.silent,
    }
  )
  return response.data
}

export async function refreshManagedAccountSnapshot(
  id: number,
  input: ManagedAccountRangeInput & { force?: boolean },
  options?: { silent?: boolean }
): Promise<ApiResponse<ManagedAccountRefreshView>> {
  const response = await api.post(
    `/api/managed-instances/${id}/account-management/refresh`,
    input,
    {
      skipBusinessError: options?.silent,
      skipErrorHandler: options?.silent,
    }
  )
  return response.data
}

export async function getManagedAlerts(): Promise<
  ApiResponse<ManagedInstanceAlertList>
> {
  const response = await api.get(
    '/api/managed-instances/alerts?page=1&page_size=100',
    { disableDuplicate: true }
  )
  return response.data
}

export async function getManagedInstanceAlerts(
  id: number
): Promise<ApiResponse<ManagedInstanceAlertList>> {
  const response = await api.get(
    `/api/managed-instances/${id}/alerts?page=1&page_size=100`,
    { disableDuplicate: true }
  )
  return response.data
}

export async function planManagedInstanceOperation(
  id: number,
  input: ManagedInstanceOperationPlanInput
): Promise<ApiResponse<ManagedInstanceOperation>> {
  const response = await api.post(
    `/api/managed-instances/${id}/actions/plan`,
    input
  )
  return response.data
}

export async function executeManagedInstanceOperation(
  id: number,
  input: ManagedInstanceOperationExecuteInput
): Promise<ApiResponse<ManagedInstanceOperationExecution>> {
  const response = await api.post(`/api/managed-instances/${id}/actions`, input)
  return response.data
}

export async function getManagedInstanceOperation(
  id: number,
  operationId: string
): Promise<ApiResponse<ManagedInstanceOperation>> {
  const response = await api.get(
    `/api/managed-instances/${id}/operations/${encodeURIComponent(operationId)}`,
    { disableDuplicate: true }
  )
  return response.data
}

export async function planManagedInstanceBatch(
  input: ManagedInstanceBatchPlanInput
): Promise<ApiResponse<ManagedInstanceBatchView>> {
  const response = await api.post(
    '/api/managed-instances/actions/batch/plan',
    input
  )
  return response.data
}

export async function executeManagedInstanceBatch(
  input: ManagedInstanceBatchExecuteInput
): Promise<ApiResponse<ManagedInstanceBatchView>> {
  const response = await api.post('/api/managed-instances/actions/batch', input)
  return response.data
}

export async function getManagedInstanceBatch(
  batchId: string
): Promise<ApiResponse<ManagedInstanceBatchView>> {
  const response = await api.get(
    `/api/managed-instances/actions/batch/${encodeURIComponent(batchId)}`,
    { disableDuplicate: true }
  )
  return response.data
}

export async function deleteManagedInstance(
  id: number
): Promise<ApiResponse<{ id: number }>> {
  const response = await api.delete(`/api/managed-instances/${id}`)
  return response.data
}

export async function getManagedConfigSchemas(): Promise<
  ApiResponse<ManagedConfigSchema[]>
> {
  const response = await api.get('/api/managed-config/schemas')
  return response.data
}

export async function getManagedConfigTemplates(
  kind: string
): Promise<ApiResponse<ManagedConfigTemplateList>> {
  const params = new URLSearchParams({ kind })
  const response = await api.get(
    `/api/managed-config/templates?${params.toString()}`
  )
  return response.data
}

export async function createManagedConfigTemplate(
  input: ManagedConfigTemplateInput
): Promise<ApiResponse<ManagedConfigTemplate>> {
  const response = await api.post('/api/managed-config/templates', input)
  return response.data
}

export async function updateManagedConfigTemplate(
  id: number,
  input: ManagedConfigTemplateInput
): Promise<ApiResponse<ManagedConfigTemplate>> {
  const response = await api.put(`/api/managed-config/templates/${id}`, input)
  return response.data
}

export async function deleteManagedConfigTemplate(
  id: number
): Promise<ApiResponse<{ id: number }>> {
  const response = await api.delete(`/api/managed-config/templates/${id}`)
  return response.data
}

export async function getManagedInstanceConfig(
  id: number
): Promise<ApiResponse<ManagedConfigBinding | null>> {
  const response = await api.get(`/api/managed-instances/${id}/config`, {
    disableDuplicate: true,
  })
  return response.data
}

export async function setManagedInstanceConfig(
  id: number,
  input: { template_id: number; mode: ManagedConfigMode }
): Promise<ApiResponse<ManagedConfigBinding>> {
  const response = await api.put(`/api/managed-instances/${id}/config`, input)
  return response.data
}

export async function refreshManagedInstanceConfig(
  id: number
): Promise<ApiResponse<ManagedConfigPreview>> {
  const response = await api.post(`/api/managed-instances/${id}/config/refresh`)
  return response.data
}

export async function planManagedInstanceConfigApply(
  id: number,
  input: { expected_observed_hash: string; idempotency_key: string }
): Promise<ApiResponse<ManagedInstanceOperation>> {
  const response = await api.post(
    `/api/managed-instances/${id}/config/apply/plan`,
    input
  )
  return response.data
}

export async function executeManagedInstanceConfigApply(
  id: number,
  input: ManagedInstanceOperationExecuteInput
): Promise<ApiResponse<ManagedInstanceOperationExecution>> {
  const response = await api.post(
    `/api/managed-instances/${id}/config/apply`,
    input
  )
  return response.data
}

export async function getManagedInstanceConfigOperation(
  id: number,
  operationId: string
): Promise<ApiResponse<ManagedInstanceOperation>> {
  const response = await api.get(
    `/api/managed-instances/${id}/config/operations/${encodeURIComponent(operationId)}`,
    { disableDuplicate: true }
  )
  return response.data
}
