import { isAxiosError } from 'axios'

import { api } from '@/lib/api'

import { SupplierRequestError, unwrap } from './portal-api'
import type {
  Audit,
  Binding,
  BindingInput,
  Envelope,
  Items,
  Supplier,
  SupplierInput,
  TestResult,
  DefaultPolicy,
  PortalSettings,
} from './types'

async function request<T>(
  path: string,
  method = 'GET',
  data?: unknown
): Promise<T> {
  try {
    const response = await api.request<Envelope<T>>({
      url: `/api/suppliers${path}`,
      method,
      data,
      skipBusinessError: true,
      skipErrorHandler: true,
    })
    return unwrap(response.data)
  } catch (error) {
    if (isAxiosError<Envelope<T>>(error)) {
      if (error.response?.data?.success === false) {
        return unwrap(error.response.data, error.response.status)
      }
      throw new SupplierRequestError(
        'REQUEST_FAILED',
        error.response?.status ?? 0
      )
    }
    throw error
  }
}

export const adminApi = {
  portalSettings: () => request<PortalSettings>('/portal-settings'),
  savePortalSettings: (data: PortalSettings) =>
    request<PortalSettings>('/portal-settings', 'PUT', data),
  get: (id: number) => request<Supplier>(`/${id}`),
  defaults: () => request<DefaultPolicy>('/default-policy'),
  saveDefaults: (data: DefaultPolicy) =>
    request<DefaultPolicy>('/default-policy', 'PUT', data),
  list: (page: number) =>
    request<Items<Supplier>>(`?page=${page}&page_size=50`),
  instances: () =>
    request<Array<{ id: number; name: string; kind: string }>>('/instances'),
  save: (data: SupplierInput, id?: number) =>
    request<Supplier>(id ? `/${id}` : '', id ? 'PUT' : 'POST', data),
  delete: (id: number) => request<unknown>(`/${id}`, 'DELETE'),
  bindings: (id: number) => request<Binding[]>(`/${id}/bindings`),
  saveBinding: (id: number, data: BindingInput, bindingId?: number) =>
    request<Binding>(
      `/${id}/bindings${bindingId ? `/${bindingId}` : ''}`,
      bindingId ? 'PUT' : 'POST',
      data
    ),
  deleteBinding: (id: number, bindingId: number) =>
    request<unknown>(`/${id}/bindings/${bindingId}`, 'DELETE'),
  testBinding: (id: number, bindingId: number) =>
    request<TestResult>(`/${id}/bindings/${bindingId}/test`, 'POST'),
  password: (id: number, password: string) =>
    request<unknown>(`/${id}/password`, 'POST', { password }),
  revoke: (id: number) => request<unknown>(`/${id}/revoke-sessions`, 'POST'),
  audits: (id: number, page: number) =>
    request<Items<Audit>>(`/${id}/audits?page=${page}&page_size=20`),
}
