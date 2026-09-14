import type { AdminDataFieldKey } from '@/lib/admin-data-policy'

export function accountLoadFields(
  isConductor: boolean,
  isClaudeGateway: boolean,
  canView: (field: AdminDataFieldKey) => boolean
): AdminDataFieldKey[] {
  if (isConductor) {
    return (['rpm', 'concurrency', 'rates'] as const).filter(canView)
  }
  const fields: AdminDataFieldKey[] = ['amount', 'requests', 'tokens']
  if (isClaudeGateway && canView('requests')) fields.push('rates')
  return fields.filter(canView)
}
