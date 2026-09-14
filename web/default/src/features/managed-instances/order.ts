import type { ManagedInstanceOrder } from './types'

export function moveOrderItem(
  items: ManagedInstanceOrder['items'],
  from: number,
  to: number
): ManagedInstanceOrder['items'] {
  if (
    from < 0 ||
    to < 0 ||
    from >= items.length ||
    to >= items.length ||
    from === to
  ) {
    return items
  }
  const next = [...items]
  const [item] = next.splice(from, 1)
  next.splice(to, 0, item)
  return next
}

export function sameInstanceOrder(
  left: ManagedInstanceOrder['items'],
  right: ManagedInstanceOrder['items']
): boolean {
  return (
    left.length === right.length &&
    left.every((item, index) => item.id === right[index].id)
  )
}

export function isInstanceOrderConsumer(key: readonly unknown[]): boolean {
  return (
    [
      'managed-instances',
      'fleet-dashboard-instances',
      'managed-account-instances',
      'usage-record-instances',
      'usage-export-instances',
      'billing-instances',
      'account-data-api-instances',
      'supplier-admin',
      'supplier',
    ].includes(String(key[0])) ||
    (key[0] === 'assistant' &&
      (key[1] === 'instance-options' ||
        (key[1] === 'me' && key[2] === 'identities'))) ||
    (key[0] === 'users' && key[1] === 'admin-policy-instances')
  )
}
