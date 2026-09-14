import { describe, expect, test } from 'bun:test'

import {
  ADMIN_DATA_FIELD_KEYS,
  adminDataPolicySchema,
  createDefaultAdminDataPolicy,
  withAdminInstanceScope,
} from './admin-data-policy'

describe('administrator data policy', () => {
  test('switching selected instances to all clears IDs and keeps revision and fields', () => {
    const selected = {
      ...createDefaultAdminDataPolicy(),
      instance_ids: [3, 8],
      fields: { amount: true },
      revision: 4,
    }
    const all = withAdminInstanceScope(selected, 'all')
    expect(all).toEqual({
      instance_scope: 'all',
      instance_ids: [],
      fields: { amount: true },
      revision: 4,
    })
    expect(selected.instance_ids).toEqual([3, 8])
    expect(withAdminInstanceScope(all, 'selected').instance_ids).toEqual([])
  })
  test('new policies deny every instance and field', () => {
    const policy = adminDataPolicySchema.parse(createDefaultAdminDataPolicy())
    expect(policy).toEqual({
      instance_scope: 'selected',
      instance_ids: [],
      fields: {},
      revision: 0,
    })
    expect(
      ADMIN_DATA_FIELD_KEYS.every((key) => policy.fields[key] !== true)
    ).toBe(true)
  })

  test('fresh defaults never share selected instances or field grants', () => {
    const first = createDefaultAdminDataPolicy()
    first.instance_ids.push(1)
    first.fields.amount = true
    expect(createDefaultAdminDataPolicy().instance_ids).toEqual([])
    expect(createDefaultAdminDataPolicy().fields).toEqual({})
  })

  test('accepts selected fields and preserves revision without adding grants', () => {
    const policy = adminDataPolicySchema.parse({
      instance_scope: 'selected',
      instance_ids: [2, 5],
      fields: { amount: true, email: false },
      revision: 4,
    })
    expect(policy.fields).toEqual({ amount: true, email: false })
    expect(policy.instance_ids).toEqual([2, 5])
    expect(policy.revision).toBe(4)
  })

  test.each([
    { instance_scope: 'unknown' },
    { instance_ids: [0] },
    { instance_ids: [-1] },
    { instance_ids: [1.5] },
    { instance_ids: ['1'] },
    { fields: { amount: 'true' } },
    { revision: -1 },
    { revision: 0.5 },
  ])('rejects malformed policy %j', (invalid) => {
    expect(
      adminDataPolicySchema.safeParse({
        ...createDefaultAdminDataPolicy(),
        ...invalid,
      }).success
    ).toBe(false)
  })
})
