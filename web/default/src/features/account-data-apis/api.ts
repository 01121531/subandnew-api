/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { api } from '@/lib/api'

import type {
  AccountDataAPI,
  AccountDataAPIAccessLogList,
  AccountDataAPICreateResult,
  AccountDataAPIInput,
  AccountDataAPIFilterOptions,
  AccountDataAPIInstance,
  AccountDataAPIKey,
  AccountDataAPIList,
  AccountDataAPIPreview,
} from './types'

type Response<T> = { success: boolean; message: string; data: T }

export async function listAccountDataAPIs(input: {
  search: string
  status: string
  page: number
  pageSize: number
  signal?: AbortSignal
}) {
  const params = new URLSearchParams({
    page: String(input.page),
    page_size: String(input.pageSize),
  })
  if (input.search) params.set('search', input.search)
  if (input.status) params.set('status', input.status)
  const response = await api.get<Response<AccountDataAPIList>>(
    `/api/account-data-apis?${params}`,
    { signal: input.signal, disableDuplicate: true }
  )
  return response.data
}

export async function listAccountDataAPIInstances() {
  const response = await api.get<Response<AccountDataAPIInstance[]>>(
    '/api/account-data-apis/instances'
  )
  return response.data
}

export async function getAccountDataAPIFilterOptions(input: {
  instance_ids: number[]
  dataset: AccountDataAPIInput['dataset']
  preset_days: number
}) {
  const response = await api.post<Response<AccountDataAPIFilterOptions>>(
    '/api/account-data-apis/filter-options',
    input,
    { disableDuplicate: true, skipErrorHandler: true }
  )
  return response.data
}

export async function createAccountDataAPI(input: AccountDataAPIInput) {
  const response = await api.post<Response<AccountDataAPICreateResult>>(
    '/api/account-data-apis',
    input,
    { skipErrorHandler: true }
  )
  return response.data
}

export async function updateAccountDataAPI(
  id: number,
  input: AccountDataAPIInput
) {
  const response = await api.put<Response<AccountDataAPI>>(
    `/api/account-data-apis/${id}`,
    input,
    { skipErrorHandler: true }
  )
  return response.data
}

export async function deleteAccountDataAPI(id: number) {
  const response = await api.delete<Response<{ id: number }>>(
    `/api/account-data-apis/${id}`,
    { skipErrorHandler: true }
  )
  return response.data
}

export async function previewAccountDataAPI(input: AccountDataAPIInput) {
  const response = await api.post<Response<AccountDataAPIPreview>>(
    '/api/account-data-apis/preview',
    input,
    { skipErrorHandler: true }
  )
  return response.data
}

export async function createAccountDataAPIKey(
  id: number,
  input: { name: string; expires_at?: number }
) {
  const response = await api.post<
    Response<{ key: AccountDataAPIKey; secret: string }>
  >(`/api/account-data-apis/${id}/keys`, input, { skipErrorHandler: true })
  return response.data
}

export async function revokeAccountDataAPIKey(id: number, keyId: number) {
  const response = await api.delete<Response<{ id: number }>>(
    `/api/account-data-apis/${id}/keys/${keyId}`,
    { skipErrorHandler: true }
  )
  return response.data
}

export async function listAccountDataAPIAccessLogs(
  id: number,
  page = 1,
  signal?: AbortSignal
) {
  const response = await api.get<Response<AccountDataAPIAccessLogList>>(
    `/api/account-data-apis/${id}/access-logs?page=${page}&page_size=20`,
    { signal, disableDuplicate: true, skipErrorHandler: true }
  )
  return response.data
}
