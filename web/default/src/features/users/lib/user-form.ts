import { z } from 'zod'

import {
  type AdminPermissionMatrix,
  type PermissionCatalog,
  normalizeAdminPermissions,
} from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'

import type { User, UserFormData } from '../types'

export const userFormSchema = z.object({
  username: z.string().min(1, 'Username is required'),
  display_name: z.string().optional(),
  password: z.string().optional(),
  role: z.number().optional(),
  remark: z.string().optional(),
  admin_permissions: z
    .record(z.string(), z.record(z.string(), z.boolean()))
    .optional(),
})

export type UserFormValues = z.infer<typeof userFormSchema>

export const USER_FORM_DEFAULT_VALUES: UserFormValues = {
  username: '',
  display_name: '',
  password: '',
  role: 1,
  remark: '',
  admin_permissions: {},
}

export function transformFormDataToPayload(
  data: UserFormValues,
  userId?: number,
  catalog?: PermissionCatalog
): UserFormData & { id?: number } {
  const payload: UserFormData & { id?: number } = {
    username: data.username,
    display_name: data.display_name || data.username,
    password: data.password || undefined,
  }
  const role = userId === undefined ? data.role || 1 : (data.role ?? 0)
  if (role >= ROLE.ADMIN && catalog) {
    payload.admin_permissions = normalizeAdminPermissions(
      data.admin_permissions as AdminPermissionMatrix | undefined,
      catalog
    )
  }
  if (userId === undefined) payload.role = role
  else {
    payload.id = userId
    payload.remark = data.remark || undefined
  }
  return payload
}

export function transformUserToFormDefaults(user: User): UserFormValues {
  return {
    username: user.username,
    display_name: user.display_name,
    password: '',
    role: user.role,
    remark: user.remark || '',
    admin_permissions: user.admin_permissions ?? {},
  }
}
