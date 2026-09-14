import { z } from 'zod'

export const ADMIN_DATA_FIELD_KEYS = [
  'amount',
  'requests',
  'tokens',
  'rpm',
  'concurrency',
  'accounts',
  'rates',
  'email',
  'vendor',
  'group',
  'status',
  'time',
] as const

export type AdminDataFieldKey = (typeof ADMIN_DATA_FIELD_KEYS)[number]

export const adminDataPolicySchema = z.object({
  instance_scope: z.enum(['all', 'selected']),
  instance_ids: z.array(z.number().int().positive()),
  fields: z.record(z.string(), z.boolean()),
  revision: z.number().int().nonnegative(),
})

export type AdminDataPolicy = z.infer<typeof adminDataPolicySchema>

export function withAdminInstanceScope(
  policy: AdminDataPolicy,
  scope: AdminDataPolicy['instance_scope']
): AdminDataPolicy {
  return {
    ...policy,
    instance_scope: scope,
    instance_ids: scope === 'all' ? [] : [...policy.instance_ids],
  }
}

export function createDefaultAdminDataPolicy(): AdminDataPolicy {
  return {
    instance_scope: 'selected',
    instance_ids: [],
    fields: {},
    revision: 0,
  }
}

type DataPrincipal = {
  id: number
  role: number
  admin_data_policy?: AdminDataPolicy
  authorization_version?: number
  permissions?: unknown
}

export function canViewAdminDataField(
  user: DataPrincipal | null | undefined,
  field: AdminDataFieldKey
): boolean {
  if (!user) return false
  return (
    user.role === 100 ||
    (user.role === 10 && user.admin_data_policy?.fields[field] === true)
  )
}

export function canViewAdminInstance(
  user: DataPrincipal | null | undefined,
  id: number
): boolean {
  if (!user || id <= 0) return false
  return (
    user.role === 100 ||
    (user.role === 10 &&
      (user.admin_data_policy?.instance_scope === 'all' ||
        (user.admin_data_policy?.instance_scope === 'selected' &&
          user.admin_data_policy.instance_ids.includes(id))))
  )
}

// UI filter/sort aliases, kept explicit so a new sensitive metric cannot become a fallback sort.
const ADMIN_DATA_UI_FIELDS: Record<string, AdminDataFieldKey[]> = {
  note: [...ADMIN_DATA_FIELD_KEYS],
  notes: [...ADMIN_DATA_FIELD_KEYS],
  search: ['email', 'vendor'],
  keyword: ['email', 'vendor'],
  amount: ['amount'],
  cost: ['amount'],
  quota: ['amount'],
  actual_cost: ['amount'],
  average: ['amount', 'accounts'],
  requests_per_account: ['requests', 'accounts'],
  average_requests: ['requests', 'accounts'],
  tokens_per_account: ['tokens', 'accounts'],
  average_tokens: ['tokens', 'accounts'],
  amount_per_account: ['amount', 'accounts'],
  cost_per_account: ['amount', 'accounts'],
  amount_per_request: ['amount', 'requests'],
  cost_per_request: ['amount', 'requests'],
  requests: ['requests'],
  total_requests: ['requests'],
  tokens: ['tokens'],
  total_tokens: ['tokens'],
  rpm: ['rpm'],
  concurrency: ['concurrency'],
  active_sessions: ['concurrency'],
  accounts: ['accounts'],
  added: ['accounts'],
  total: ['accounts'],
  rates: ['rates'],
  success_rate: ['rates'],
  utilization_5h: ['rates'],
  utilization_7d: ['rates'],
  coverage: ['rates'],
  email: ['email'],
  vendor: ['vendor'],
  vendor_name: ['vendor'],
  vendor_email: ['vendor', 'email'],
  ownership: ['vendor'],
  group: ['group'],
  group_id: ['group'],
  status: ['status'],
  available: ['status'],
  time: ['time'],
  created_at: ['time'],
  last_activity: ['time'],
  last_activity_at: ['time'],
  survival: ['time', 'status'],
  'average-unavailable-survival': ['time', 'status', 'accounts'],
  unavailable: ['status', 'accounts'],
}

export function visibleAdminDataKey(
  user: DataPrincipal | null | undefined,
  key: string
): boolean {
  return (ADMIN_DATA_UI_FIELDS[key] ?? []).every((field) =>
    canViewAdminDataField(user, field)
  )
}

export function adminDataAuthorizationKey(
  user: DataPrincipal | null | undefined
): string {
  return JSON.stringify(
    user
      ? [
          user.id,
          user.role,
          user.authorization_version,
          user.admin_data_policy,
          user.permissions,
        ]
      : null
  )
}
