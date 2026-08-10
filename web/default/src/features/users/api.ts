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
import type { PermissionCatalog } from '@/lib/admin-permissions'
import { api } from '@/lib/api'

import type {
  ApiResponse,
  GetUsersParams,
  GetUsersResponse,
  ManageUserAction,
  SearchUsersParams,
  User,
  UserFormData,
} from './types'

export async function getUsers(
  params: GetUsersParams = {}
): Promise<GetUsersResponse> {
  const { p = 1, page_size = 10 } = params
  return (await api.get(`/api/user/?p=${p}&page_size=${page_size}`)).data
}

export async function searchUsers(
  params: SearchUsersParams
): Promise<GetUsersResponse> {
  const query = new URLSearchParams({
    keyword: params.keyword ?? '',
    p: String(params.p ?? 1),
    page_size: String(params.page_size ?? 10),
  })
  if (params.role) query.set('role', params.role)
  if (params.status) query.set('status', params.status)
  return (await api.get(`/api/user/search?${query}`)).data
}

export async function getUser(id: number): Promise<ApiResponse<User>> {
  return (await api.get(`/api/user/${id}`)).data
}

export async function createUser(
  data: UserFormData
): Promise<ApiResponse<User>> {
  return (await api.post('/api/user/', data)).data
}

export async function updateUser(
  data: UserFormData & { id: number }
): Promise<ApiResponse<Partial<User>>> {
  return (await api.put('/api/user/', data)).data
}

export async function deleteUser(id: number): Promise<ApiResponse> {
  return (await api.delete(`/api/user/${id}`)).data
}

export async function manageUser(
  id: number,
  action: ManageUserAction
): Promise<ApiResponse<Partial<User>>> {
  return (await api.post('/api/user/manage', { id, action })).data
}

export async function resetUserPasskey(id: number): Promise<ApiResponse> {
  return (await api.delete(`/api/user/${id}/reset_passkey`)).data
}

export async function resetUserTwoFA(id: number): Promise<ApiResponse> {
  return (await api.delete(`/api/user/${id}/2fa`)).data
}

export async function getPermissionCatalog(): Promise<PermissionCatalog> {
  const response = await api.get('/api/authz/catalog')
  return {
    resources: response.data?.data?.resources ?? [],
    roles: response.data?.data?.roles ?? [],
  }
}
