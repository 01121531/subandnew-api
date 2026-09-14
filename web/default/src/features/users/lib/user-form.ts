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
import { z } from 'zod'

import {
  adminDataPolicySchema,
  createDefaultAdminDataPolicy,
  withAdminInstanceScope,
} from '@/lib/admin-data-policy'
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
  admin_data_policy: adminDataPolicySchema.optional(),
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

export function explicitAdminPermissions(
  value: AdminPermissionMatrix | undefined,
  catalog: PermissionCatalog
): AdminPermissionMatrix {
  return Object.fromEntries(
    catalog.resources.map((resource) => [
      resource.resource,
      Object.fromEntries(
        resource.actions.map((action) => [
          action.action,
          value?.[resource.resource]?.[action.action] === true,
        ])
      ),
    ])
  )
}

export function createAdminFormDefaults(): UserFormValues {
  return {
    ...USER_FORM_DEFAULT_VALUES,
    role: ROLE.ADMIN,
    admin_permissions: {},
    admin_data_policy: createDefaultAdminDataPolicy(),
  }
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
  if (
    role === ROLE.ADMIN &&
    userId === undefined &&
    !catalog?.resources.length
  ) {
    throw new Error('Permission catalog required to create an administrator')
  }
  if (role >= ROLE.ADMIN && catalog) {
    if (userId === undefined) {
      payload.admin_permissions = explicitAdminPermissions(
        data.admin_permissions,
        catalog
      )
    } else if (data.admin_permissions !== undefined) {
      payload.admin_permissions = normalizeAdminPermissions(
        data.admin_permissions,
        catalog
      )
    }
  }
  if (role === ROLE.ADMIN) {
    if (data.admin_data_policy !== undefined) {
      payload.admin_data_policy = adminDataPolicySchema.parse(
        withAdminInstanceScope(
          data.admin_data_policy,
          data.admin_data_policy.instance_scope
        )
      )
    } else if (userId === undefined) {
      payload.admin_data_policy = createDefaultAdminDataPolicy()
    }
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
    admin_data_policy: user.admin_data_policy,
  }
}
