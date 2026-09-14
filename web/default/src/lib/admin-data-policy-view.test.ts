import { describe, expect, test } from 'bun:test'

import {
  USAGE_DATA_COLUMNS,
  sanitizeUsageFilters,
} from '@/features/usage-records/restricted-usage-policy'
import { adminDataEn, adminDataZh } from '@/i18n/admin-data'

import {
  ADMIN_DATA_FIELD_KEYS,
  canViewAdminDataField,
  canViewAdminInstance,
  createDefaultAdminDataPolicy,
  visibleAdminDataKey,
} from './admin-data-policy'
import {
  adminDataPagination,
  adminDataValue,
  formatAdminDataValue,
  sortAdminDataRows,
  visibleAdminDefinitions,
} from './admin-data-policy-view'

const principal = {
  id: 2,
  role: 10,
  admin_data_policy: createDefaultAdminDataPolicy(),
}

describe('restricted data projection in the UI', () => {
  test('derived metrics require both contributing field grants', () => {
    const amountOnly = {
      ...principal,
      admin_data_policy: {
        ...principal.admin_data_policy,
        fields: { amount: true },
      },
    }
    const amountsAndAccounts = {
      ...amountOnly,
      admin_data_policy: {
        ...amountOnly.admin_data_policy,
        fields: { amount: true, accounts: true },
      },
    }
    expect(visibleAdminDataKey(amountOnly, 'average')).toBe(false)
    expect(visibleAdminDataKey(amountsAndAccounts, 'average')).toBe(true)
    expect(
      visibleAdminDataKey(amountsAndAccounts, 'requests_per_account')
    ).toBe(false)
    expect(visibleAdminDataKey(amountsAndAccounts, 'amount_per_request')).toBe(
      false
    )
    expect(visibleAdminDataKey(amountsAndAccounts, 'cost_per_request')).toBe(
      false
    )
    const emailOnly = {
      ...principal,
      admin_data_policy: {
        ...principal.admin_data_policy,
        fields: { email: true },
      },
    }
    expect(visibleAdminDataKey(emailOnly, 'vendor_email')).toBe(false)
    expect(
      visibleAdminDataKey(
        {
          ...emailOnly,
          admin_data_policy: {
            ...emailOnly.admin_data_policy,
            fields: { email: true, vendor: true },
          },
        },
        'vendor_email'
      )
    ).toBe(true)
  })
  test('missing policies, signed-out users, and ordinary users fail closed', () => {
    for (const user of [
      null,
      { id: 2, role: 10 },
      principal,
      {
        ...principal,
        role: 1,
        admin_data_policy: {
          ...principal.admin_data_policy,
          instance_scope: 'all' as const,
          fields: { amount: true },
        },
      },
    ]) {
      expect(canViewAdminDataField(user, 'amount')).toBe(false)
      expect(canViewAdminInstance(user, 3)).toBe(false)
    }
    expect(canViewAdminDataField({ id: 1, role: 100 }, 'amount')).toBe(true)
    expect(canViewAdminInstance({ id: 1, role: 100 }, 3)).toBe(true)
    expect(
      canViewAdminInstance(
        {
          ...principal,
          admin_data_policy: {
            ...principal.admin_data_policy,
            instance_ids: [3],
          },
        },
        3
      )
    ).toBe(true)
  })

  test('hidden fields are absent from table, filter, and sort definitions', () => {
    const visible = visibleAdminDefinitions(
      USAGE_DATA_COLUMNS,
      (key) => key === 'tokens'
    )
    expect(visible.map((item) => item.key)).toEqual([
      'name',
      'model',
      'request_id',
      'total_tokens',
      'input_tokens',
      'output_tokens',
    ])
  })

  test('missing data is never converted to zero or a stringified private object', () => {
    expect(formatAdminDataValue(adminDataValue({}, 'cost'))).toBe('--')
    expect(formatAdminDataValue({ email: 'private@example.com' })).toBe('--')
    expect(formatAdminDataValue(0)).toBe('0')
    expect(adminDataValue({ amount: 0, quota: 55 }, 'amount', 'quota')).toBe(0)
  })

  test('name sorting does not use a hidden metric for tiebreaking', () => {
    const rows = [
      { name: 'B', cost: 999 },
      { name: 'A', cost: 2 },
      { name: 'A', cost: 1 },
    ]
    expect(
      sortAdminDataRows(rows, 'name', 'asc', (row, key) =>
        adminDataValue(row, key)
      )
    ).toEqual([rows[1], rows[2], rows[0]])
  })

  test('inexact or forbidden totals remain absent and has_more controls navigation', () => {
    expect(
      adminDataPagination(
        { total: 1000, total_is_exact: false, has_more: true, page: 1 },
        1,
        20,
        true
      )
    ).toEqual({ exactTotal: undefined, hasMore: true })
    expect(
      adminDataPagination(
        { total: 1000, total_is_exact: true, has_more: false },
        1,
        20,
        false
      )
    ).toEqual({ exactTotal: undefined, hasMore: false })
    expect(
      adminDataPagination({ has_more: true, page: 1 }, 2, 20, false).hasMore
    ).toBe(false)
    expect(adminDataPagination(undefined, 1, 20, true).hasMore).toBe(false)
  })

  test('denied saved filter values and time sort never reach the usage API', () => {
    const input = {
      page: '2',
      sort_by: 'created_at',
      model: 'model-a',
      group_id: '9',
      start_date: '2026-01-01',
      end_date: '2026-01-02',
      note: 'private',
      vendor_email: 'private@example.com',
    }
    expect(
      sanitizeUsageFilters(input, (key) => visibleAdminDataKey(principal, key))
    ).toEqual({
      page: '2',
      sort_by: 'model',
      model: 'model-a',
      start_date: '2026-01-01',
      end_date: '2026-01-02',
    })
    expect(sanitizeUsageFilters(input, () => true)).toEqual(input)
  })

  test('every catalog field has English and Chinese labels', () => {
    for (const key of ADMIN_DATA_FIELD_KEYS) {
      expect(adminDataEn.fields[key].length).toBeGreaterThan(0)
      expect(adminDataZh.fields[key].length).toBeGreaterThan(0)
    }
  })
})
