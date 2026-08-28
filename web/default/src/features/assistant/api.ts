/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { api } from '@/lib/api'

import type {
  ApiResponse,
  AssistantBindingCode,
  AssistantChannel,
  AssistantIdentity,
  AssistantLoginView,
  AssistantModelProfile,
  AssistantModelProfileInput,
  AssistantModelTestResult,
  AssistantRunDetail,
  AssistantRunList,
  AssistantRunStatus,
} from './types'

export async function listAssistantModelProfiles() {
  const response = await api.get<ApiResponse<AssistantModelProfile[]>>(
    '/api/assistant/model-profiles'
  )
  return response.data.data
}

export async function createAssistantModelProfile(
  input: AssistantModelProfileInput
) {
  const response = await api.post<ApiResponse<AssistantModelProfile>>(
    '/api/assistant/model-profiles',
    input
  )
  return response.data.data
}

export async function updateAssistantModelProfile(
  id: number,
  input: AssistantModelProfileInput
) {
  const { api_key: apiKey, ...profile } = input
  const payload = apiKey.trim() ? { ...profile, api_key: apiKey } : profile
  const response = await api.put<ApiResponse<AssistantModelProfile>>(
    `/api/assistant/model-profiles/${id}`,
    payload
  )
  return response.data.data
}

export async function deleteAssistantModelProfile(id: number) {
  await api.delete(`/api/assistant/model-profiles/${id}`)
}

export async function testAssistantModelProfile(id: number) {
  const response = await api.post<ApiResponse<AssistantModelTestResult>>(
    `/api/assistant/model-profiles/${id}/test`,
    undefined,
    { skipErrorHandler: true }
  )
  return response.data.data
}

export async function listAssistantChannels() {
  const response = await api.get<ApiResponse<AssistantChannel[]>>(
    '/api/assistant/channels'
  )
  return response.data.data
}

export async function startAssistantChannelLogin() {
  const response = await api.post<ApiResponse<AssistantLoginView>>(
    '/api/assistant/channels/login'
  )
  return response.data.data
}

export async function checkAssistantChannelLogin(
  channelId: number,
  verifyCode = ''
) {
  const response = await api.post<ApiResponse<AssistantLoginView>>(
    `/api/assistant/channels/${channelId}/login/status`,
    { verify_code: verifyCode },
    { skipErrorHandler: true }
  )
  return response.data.data
}

export async function createAssistantBindingCode() {
  const response = await api.post<ApiResponse<AssistantBindingCode>>(
    '/api/assistant/binding-code',
    { scope: 'all', instance_ids: [] }
  )
  return response.data.data
}

export async function listAssistantRuns(input: {
  page: number
  pageSize: number
  status: AssistantRunStatus | ''
}) {
  const params = new URLSearchParams({
    page: String(input.page),
    page_size: String(input.pageSize),
  })
  if (input.status) params.set('status', input.status)
  const response = await api.get<ApiResponse<AssistantRunList>>(
    `/api/assistant/runs?${params.toString()}`
  )
  return response.data.data
}

export async function getAssistantRun(runId: string) {
  const response = await api.get<ApiResponse<AssistantRunDetail>>(
    `/api/assistant/runs/${encodeURIComponent(runId)}`
  )
  return response.data.data
}

export async function listAssistantIdentities() {
  const response = await api.get<ApiResponse<AssistantIdentity[]>>(
    '/api/assistant/identities'
  )
  return response.data.data
}

export async function revokeAssistantIdentity(identityId: number) {
  await api.delete(`/api/assistant/identities/${identityId}`)
}

export async function removeAssistantChannelCredential(channelId: number) {
  await api.delete(`/api/assistant/channels/${channelId}/credential`)
}
