import { describe, expect, test } from 'bun:test'

import {
  isInstanceOrderConsumer,
  moveOrderItem,
  sameInstanceOrder,
} from './order'
import type { ManagedInstanceOrder } from './types'

const items: ManagedInstanceOrder['items'] = [
  { id: 3, name: 'C', kind: 'generic' },
  { id: 2, name: 'B', kind: 'generic' },
  { id: 1, name: 'A', kind: 'generic' },
]

describe('instance order draft', () => {
  test('moves items in either direction without mutating the server order', () => {
    const moved = moveOrderItem(items, 0, 2)
    expect(moved.map((item) => item.id)).toEqual([2, 1, 3])
    expect(items.map((item) => item.id)).toEqual([3, 2, 1])
    expect(sameInstanceOrder(items, moved)).toBe(false)
    expect(sameInstanceOrder(items, moveOrderItem(moved, 2, 0))).toBe(true)
  })

  test('ignores moves past the bounds and empty lists', () => {
    expect(moveOrderItem(items, 0, -1)).toBe(items)
    expect(moveOrderItem(items, 2, 3)).toBe(items)
    expect(moveOrderItem(items, -1, 1)).toBe(items)
    expect(moveOrderItem(items, 1, 1)).toBe(items)
    expect(moveOrderItem([], 0, 1)).toEqual([])
  })

  test('dirty comparison uses identity and membership, not display names', () => {
    expect(
      sameInstanceOrder(
        items,
        items.map((item) => ({ ...item, name: 'renamed' }))
      )
    ).toBe(true)
    expect(sameInstanceOrder(items, items.slice(1))).toBe(false)
  })

  test('invalidates console and supplier instance consumers without changing preferences', () => {
    expect(isInstanceOrderConsumer(['fleet-dashboard-instances'])).toBe(true)
    expect(
      isInstanceOrderConsumer(['managed-instances', { search: 'C' }])
    ).toBe(true)
    expect(isInstanceOrderConsumer(['supplier-admin', 'bindings', 2])).toBe(
      true
    )
    expect(isInstanceOrderConsumer(['assistant', 'instance-options'])).toBe(
      true
    )
    expect(isInstanceOrderConsumer(['assistant', 'preferences'])).toBe(false)
    expect(isInstanceOrderConsumer(['assistant', 'me', 'identities'])).toBe(
      true
    )
    expect(isInstanceOrderConsumer(['users', 'admin-policy-instances'])).toBe(
      true
    )
    expect(isInstanceOrderConsumer(['users', 'list'])).toBe(false)
    expect(isInstanceOrderConsumer(['managed-instance-order'])).toBe(false)
  })
})
