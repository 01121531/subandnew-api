import { api } from '@/lib/api'

import type { ApiResponse, UpdateUserRequest, UserProfile } from './types'

export async function getUserProfile(): Promise<ApiResponse<UserProfile>> {
  return (await api.get('/api/user/self')).data
}

export async function updateUserProfile(
  data: UpdateUserRequest
): Promise<ApiResponse> {
  return (await api.put('/api/user/self', data)).data
}

export async function updateUserLanguage(
  language: string
): Promise<ApiResponse> {
  return (await api.put('/api/user/self', { language })).data
}
