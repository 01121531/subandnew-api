import { describe, expect, test } from 'bun:test'

import { supplierEn, supplierZh } from '@/i18n/supplier'

import type { Supplier } from '../types'
import {
  applyOverrides,
  bindingCapabilities,
  globalEffectivePolicy,
  policyGroups,
} from './permissions'

describe('supplier policy', () => {
  test('child overrides and null inheritance preserve sources and parent data', () => {
    const defaults = globalEffectivePolicy({
      policy: {
        view_accounts: true,
        upload_accounts: false,
        'account.email': true,
      },
      revision: 1,
    })
    const supplier = applyOverrides(
      defaults,
      { view_accounts: false, 'account.email': null },
      'supplier'
    )
    const binding = applyOverrides(
      supplier,
      { view_accounts: true, upload_accounts: true },
      'binding'
    )
    expect(binding.values.view_accounts).toBe(true)
    expect(binding.sources.view_accounts).toBe('binding')
    expect(binding.sources['account.email']).toBe('global')
    expect(supplier.values.view_accounts).toBe(false)
    expect(defaults.values.view_accounts).toBe(true)
  })
  test('missing binding policy fails closed, including old session flags', () => {
    const old = {
      view_accounts: true,
      view_usage: true,
      upload_accounts: true,
      manage_proxies: true,
    } as Supplier
    expect(bindingCapabilities(old).upload_accounts).toBe(false)
    const effective = globalEffectivePolicy({
      policy: { upload_accounts: true },
      revision: 2,
    })
    const result = bindingCapabilities(old, effective)
    expect(result.upload_accounts).toBe(true)
    expect(result.view_accounts).toBe(false)
    expect(result.view_usage).toBe(false)
  })
  test('all 21 keys have unique bilingual labels', () => {
    const keys = policyGroups.flatMap((group) => [...group.keys])
    expect(new Set(keys).size).toBe(21)
    for (const field of keys) {
      const label =
        `policyField_${field.replaceAll('.', '_')}` as keyof typeof supplierZh
      expect(supplierZh[label]).toBeTruthy()
      expect(supplierEn[label]).toBeTruthy()
    }
  })
})
