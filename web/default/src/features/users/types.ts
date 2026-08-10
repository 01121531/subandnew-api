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
import type { AdminPermissionMatrix } from '@/lib/admin-permissions'

export interface User {
  id: number
  username: string
  display_name: string
  password?: string
  email?: string
  github_id?: string
  discord_id?: string
  oidc_id?: string
  wechat_id?: string
  telegram_id?: string
  linux_do_id?: string
  language?: string
  status: number
  role: number
  created_at?: number
  last_login_at?: number
  DeletedAt?: unknown | null
  remark?: string
  admin_permissions?: AdminPermissionMatrix
}

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface GetUsersParams {
  p?: number
  page_size?: number
}

export interface GetUsersResponse extends ApiResponse<{
  items: User[]
  total: number
  page: number
  page_size: number
}> {}

export interface SearchUsersParams extends GetUsersParams {
  keyword?: string
  role?: string
  status?: string
}

export interface UserFormData {
  username: string
  display_name: string
  password?: string
  role?: number
  remark?: string
  admin_permissions?: AdminPermissionMatrix
}

export type ManageUserAction =
  | 'promote'
  | 'demote'
  | 'enable'
  | 'disable'
  | 'delete'

export type UsersDialogType = 'create' | 'update' | 'delete'
