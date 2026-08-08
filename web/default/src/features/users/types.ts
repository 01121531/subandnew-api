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
