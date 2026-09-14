import type { AdminDataDefinition } from '@/lib/admin-data-policy-view'

import type { UsageRecordFilters } from './types'

export const USAGE_DATA_COLUMNS: AdminDataDefinition[] = [
  {
    key: 'name',
    label: 'Name',
    fields: [],
    paths: ['username', 'user_name', 'model', 'model_name'],
  },
  { key: 'model', label: 'Model', fields: [], paths: ['model', 'model_name'] },
  { key: 'request_id', label: 'Request ID', fields: [] },
  {
    key: 'created_at',
    label: 'adminData.fields.time',
    fields: ['time'],
    paths: ['created_at', 'date'],
  },
  {
    key: 'email',
    label: 'adminData.fields.email',
    fields: ['email'],
    paths: ['email', 'user.email'],
  },
  { key: 'vendor_name', label: 'adminData.fields.vendor', fields: ['vendor'] },
  {
    key: 'group',
    label: 'adminData.fields.group',
    fields: ['group'],
    paths: ['group.name', 'group', 'group_id'],
  },
  { key: 'status', label: 'adminData.fields.status', fields: ['status'] },
  { key: 'requests', label: 'adminData.fields.requests', fields: ['requests'] },
  {
    key: 'total_tokens',
    label: 'adminData.fields.tokens',
    fields: ['tokens'],
    paths: ['total_tokens', 'tokens'],
  },
  {
    key: 'input_tokens',
    label: 'adminData.inputTokens',
    fields: ['tokens'],
    paths: ['input_tokens', 'prompt_tokens'],
  },
  {
    key: 'output_tokens',
    label: 'adminData.outputTokens',
    fields: ['tokens'],
    paths: ['output_tokens', 'completion_tokens'],
  },
  {
    key: 'amount',
    label: 'adminData.fields.amount',
    fields: ['amount'],
    paths: ['amount', 'amount_usd', 'actual_cost', 'quota'],
  },
]

export function sanitizeUsageFilters(
  input: UsageRecordFilters,
  allows: (key: string) => boolean
): UsageRecordFilters {
  const result = Object.fromEntries(
    Object.entries(input).filter(([key]) => allows(key))
  )
  if (typeof result.sort_by === 'string' && !allows(result.sort_by)) {
    result.sort_by = 'model'
  }
  return result
}
