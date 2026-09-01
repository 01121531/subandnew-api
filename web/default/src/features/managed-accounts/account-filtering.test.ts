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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  accountFilterDocument,
  accountFilterDateTimeInputValue,
  accountFilterSnapshot,
  createAccountFilterRule,
  matchesAdvancedAccountFilter,
  matchesQuickAccountFilter,
  parseAccountFilterTerms,
  parseAccountFilterDisplayValues,
} from './account-filtering'

const account = accountFilterDocument({
  name: 'Alice Standard',
  email: 'Alice@Gmail.com',
  vendor_name: 'Acme Supply',
  vendor_email: 'owner@acme.test',
  instance: 'Shanghai Gateway',
  group: 'default',
  available: 'available',
  requests: 125,
  amount: 9.875,
  cost_excluding_today: 93.125,
  utilization_5h: 82,
  created_at: 1787967000,
})

describe('account filtering', () => {
  test('parses and deduplicates quick multi-value terms', () => {
    assert.deepEqual(parseAccountFilterTerms(' Gmail，outlook\ngmail,  '), [
      'gmail',
      'outlook',
    ])
  })

  test('preserves the first display form when parsing pasted values', () => {
    assert.deepEqual(
      parseAccountFilterDisplayValues('allen\nhh，Jack,ALLEN\ncc'),
      ['allen', 'hh', 'Jack', 'cc']
    )
    assert.deepEqual(parseAccountFilterDisplayValues('hero'), ['hero'])
  })

  test('quick include matches any value and exclusion rejects any value', () => {
    assert.equal(
      matchesQuickAccountFilter(account, ['yahoo', 'alice'], []),
      true
    )
    assert.equal(matchesQuickAccountFilter(account, ['yahoo'], []), false)
    assert.equal(
      matchesQuickAccountFilter(account, [], ['alice@gmail.com']),
      false
    )
    assert.equal(matchesQuickAccountFilter(account, ['ma'], []), false)
    assert.equal(matchesQuickAccountFilter(account, ['gmail'], []), false)
    assert.equal(matchesQuickAccountFilter(account, ['gmail.com'], []), true)
    assert.equal(matchesQuickAccountFilter(account, ['shanghai'], []), false)
    assert.equal(matchesQuickAccountFilter(account, [], ['default']), true)
  })

  test('supports all and any rule groups with per-rule value modes', () => {
    const email = {
      ...createAccountFilterRule('email'),
      values: ['gmail', 'outlook'],
      value_mode: 'any' as const,
    }
    const name = {
      ...createAccountFilterRule('name'),
      values: ['alice', 'standard'],
      value_mode: 'all' as const,
    }
    assert.equal(
      matchesAdvancedAccountFilter(account, {
        match_mode: 'all',
        rules: [email, name],
      }),
      true
    )
    assert.equal(
      matchesAdvancedAccountFilter(account, {
        match_mode: 'all',
        rules: [{ ...email, operator: 'not_contains' }],
      }),
      false
    )
  })

  test('supports categorical and empty operators', () => {
    const available = {
      ...createAccountFilterRule('available'),
      values: ['available'],
    }
    assert.equal(
      matchesAdvancedAccountFilter(account, {
        match_mode: 'all',
        rules: [available],
      }),
      true
    )
    assert.equal(
      matchesAdvancedAccountFilter(account, {
        match_mode: 'all',
        rules: [{ ...createAccountFilterRule('note'), operator: 'is_empty' }],
      }),
      true
    )
  })

  test('supports vendor equality, prefix, suffix, and contains rules', () => {
    const vendor = createAccountFilterRule('vendor_name')
    vendor.operator = 'is'
    vendor.values = ['acme supply']
    assert.equal(
      matchesAdvancedAccountFilter(account, {
        match_mode: 'all',
        rules: [vendor],
      }),
      true
    )
    vendor.operator = 'starts_with'
    vendor.values = ['acme']
    const email = createAccountFilterRule('vendor_email')
    email.operator = 'ends_with'
    email.values = ['@acme.test']
    assert.equal(
      matchesAdvancedAccountFilter(account, {
        match_mode: 'all',
        rules: [vendor, email],
      }),
      true
    )
    assert.equal(
      matchesQuickAccountFilter(account, ['acme supply'], []),
      false,
      'vendor fields must not broaden quick search'
    )
  })

  test('supports numeric ranges and China local time comparisons', () => {
    const requests = createAccountFilterRule('requests')
    requests.operator = 'gte'
    requests.values = ['100']
    const utilization = createAccountFilterRule('utilization_5h')
    utilization.operator = 'between'
    utilization.values = ['80', '90']
    const created = createAccountFilterRule('created_at')
    created.operator = 'gte'
    created.values = ['2026-08-29 09:00']
    const historicalCost = createAccountFilterRule('cost_excluding_today')
    historicalCost.operator = 'gt'
    historicalCost.values = ['90']
    assert.equal(
      matchesAdvancedAccountFilter(account, {
        match_mode: 'all',
        rules: [requests, utilization, created, historicalCost],
      }),
      true
    )
    assert.equal(
      matchesAdvancedAccountFilter(account, {
        match_mode: 'all',
        rules: [{ ...requests, operator: 'lt' }],
      }),
      false
    )
  })

  test('formats saved China times and timestamps for datetime pickers', () => {
    assert.equal(
      accountFilterDateTimeInputValue('2026-08-29 09:30'),
      '2026-08-29T09:30:00'
    )
    assert.equal(
      accountFilterDateTimeInputValue('1787967000'),
      '2026-08-29T09:30:00'
    )
    assert.equal(accountFilterDateTimeInputValue('not-a-date'), '')
  })

  test('negates the configured any or all value matching result', () => {
    const document = accountFilterDocument({ name: 'alpha beta' })
    const rule = createAccountFilterRule('name')
    rule.operator = 'not_contains'
    rule.values = ['alpha', 'gamma']
    rule.value_mode = 'any'
    assert.equal(
      matchesAdvancedAccountFilter(document, {
        match_mode: 'all',
        rules: [rule],
      }),
      false
    )
    rule.value_mode = 'all'
    assert.equal(
      matchesAdvancedAccountFilter(document, {
        match_mode: 'all',
        rules: [rule],
      }),
      true
    )
  })

  test('keeps incomplete rules out of the effective export snapshot', () => {
    const complete = createAccountFilterRule('email')
    complete.values = ['gmail.com']
    const incomplete = createAccountFilterRule('name')
    assert.deepEqual(
      accountFilterSnapshot({
        match_mode: 'any',
        rules: [complete, incomplete],
      }),
      {
        match_mode: 'any',
        rules: [
          {
            field: 'email',
            operator: 'contains',
            values: ['gmail.com'],
            value_mode: 'any',
          },
        ],
      }
    )
  })
})
