import { describe, expect, test } from 'bun:test'

import { ADMIN_DATA_FIELD_KEYS } from '@/lib/admin-data-policy'
import type { PermissionCatalog } from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'

import type { User } from '../types'
import { canManageUser, isAdminPolicyConflict } from './admin-editor-access'
import {
  createAdminFormDefaults,
  transformFormDataToPayload,
  transformUserToFormDefaults,
  userFormSchema,
} from './user-form'

const catalog: PermissionCatalog = {
  resources: [
    {
      resource: 'managed_instance',
      label_key: 'Instances',
      actions: [
        { action: 'view', label_key: 'View', description_key: '' },
        { action: 'operate', label_key: 'Operate', description_key: '' },
      ],
    },
  ],
  roles: [
    {
      key: 'admin',
      name: 'Admin',
      built_in: true,
      superuser: false,
      grants: { managed_instance: { view: true, operate: true } },
    },
  ],
}
const admin: User = {
  id: 2,
  role: ROLE.ADMIN,
  status: 1,
  username: 'admin',
  display_name: 'Admin',
}

describe('bounded administrator form payload', () => {
  test('cannot create an admin without the complete function catalog', () => {
    expect(() => transformFormDataToPayload(createAdminFormDefaults())).toThrow(
      'Permission catalog required'
    )
    expect(() =>
      transformFormDataToPayload(createAdminFormDefaults(), undefined, {
        resources: [],
        roles: [],
      })
    ).toThrow('Permission catalog required')
  })
  test('new admin explicitly denies baseline function grants and all data', () => {
    const values = userFormSchema.parse({
      ...createAdminFormDefaults(),
      username: 'new-admin',
      password: 'password123',
    })
    const payload = transformFormDataToPayload(values, undefined, catalog)
    expect(payload.role).toBe(ROLE.ADMIN)
    expect(payload.admin_permissions).toEqual({
      managed_instance: { view: false, operate: false },
    })
    expect(payload.admin_data_policy).toEqual({
      instance_scope: 'selected',
      instance_ids: [],
      fields: {},
      revision: 0,
    })
  })

  test('only checked functions and fields are allowed', () => {
    const values = createAdminFormDefaults()
    values.admin_permissions = { managed_instance: { view: true } }
    values.admin_data_policy = {
      instance_scope: 'selected',
      instance_ids: [3],
      fields: { amount: true, email: false },
      revision: 0,
    }
    const payload = transformFormDataToPayload(values, undefined, catalog)
    expect(payload.admin_permissions).toEqual({
      managed_instance: { view: true, operate: false },
    })
    expect(payload.admin_data_policy?.fields).toEqual({
      amount: true,
      email: false,
    })
    expect(payload.admin_data_policy?.instance_ids).toEqual([3])
  })

  test('omitted update policy remains absent for backward compatibility', () => {
    const values = transformUserToFormDefaults(admin)
    expect(values.admin_data_policy).toBeUndefined()
    expect(
      transformFormDataToPayload(values, admin.id, catalog)
    ).not.toHaveProperty('admin_data_policy')
  })

  test('all-instance payload clears stale selected IDs while preserving grants and revision', () => {
    const values = transformUserToFormDefaults({
      ...admin,
      admin_data_policy: {
        instance_scope: 'selected',
        instance_ids: [3, 8],
        fields: { requests: true },
        revision: 7,
      },
    })
    if (!values.admin_data_policy) throw new Error('Expected selected policy')
    values.admin_data_policy.instance_scope = 'all'
    expect(
      transformFormDataToPayload(values, admin.id, catalog).admin_data_policy
    ).toEqual({
      instance_scope: 'all',
      instance_ids: [],
      fields: { requests: true },
      revision: 7,
    })
    expect(values.admin_data_policy.instance_ids).toEqual([3, 8])
  })

  test('existing all-instance and all-field policy is preserved with its revision', () => {
    const policy = {
      instance_scope: 'all' as const,
      instance_ids: [],
      fields: Object.fromEntries(
        ADMIN_DATA_FIELD_KEYS.map((key) => [key, true])
      ),
      revision: 7,
    }
    const values = transformUserToFormDefaults({
      ...admin,
      admin_data_policy: policy,
    })
    expect(
      transformFormDataToPayload(values, admin.id, catalog).admin_data_policy
    ).toEqual(policy)
  })

  test('ordinary user payload never contains administrator policy', () => {
    const values = { ...createAdminFormDefaults(), role: ROLE.USER }
    const payload = transformFormDataToPayload(values, undefined, catalog)
    expect(payload).not.toHaveProperty('admin_permissions')
    expect(payload).not.toHaveProperty('admin_data_policy')
  })

  test('schema keeps policy optional but rejects invalid supplied policy', () => {
    expect(userFormSchema.safeParse({ username: 'old-user' }).success).toBe(
      true
    )
    expect(
      userFormSchema.safeParse({ username: 'admin', admin_data_policy: {} })
        .success
    ).toBe(false)
  })
})

describe('administrator management boundaries', () => {
  test('only root can manage another administrator, never itself or another root', () => {
    const root = { id: 1, role: ROLE.SUPER_ADMIN }
    expect(canManageUser(root, admin)).toBe(true)
    expect(canManageUser(admin, admin)).toBe(false)
    expect(canManageUser(admin, { ...admin, id: 3 })).toBe(false)
    expect(canManageUser(root, root)).toBe(false)
    expect(canManageUser(root, { ...root, id: 4 })).toBe(false)
    expect(canManageUser(null, admin)).toBe(false)
    expect(canManageUser(admin, { id: 3, role: ROLE.USER })).toBe(false)
    expect(canManageUser(root, { id: 3, role: ROLE.USER })).toBe(true)
  })

  test('recognizes version conflicts without classifying other errors as conflicts', () => {
    expect(
      isAdminPolicyConflict({ code: 'admin_data_policy_revision_conflict' })
    ).toBe(true)
    expect(isAdminPolicyConflict({ message: 'Policy revision conflict' })).toBe(
      true
    )
    expect(isAdminPolicyConflict({ message: 'Network error' })).toBe(false)
  })
})
