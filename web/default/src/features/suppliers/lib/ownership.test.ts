import { expect, test } from 'bun:test'

import { createInstance } from 'i18next'

import { ROLE } from '@/lib/roles'

import { canTakeSupplierOwnership, supplierOwnershipText } from './ownership'

test('supplier ownership is root-only and supports ownerless legacy rows', () => {
  const root = { id: 7, role: ROLE.SUPER_ADMIN, status: 1 }
  expect(canTakeSupplierOwnership(root, 2)).toBe(true)
  expect(canTakeSupplierOwnership(root, 0)).toBe(true)
  expect(canTakeSupplierOwnership(root, undefined)).toBe(true)
  expect(canTakeSupplierOwnership(root, 7)).toBe(false)
  expect(canTakeSupplierOwnership({ ...root, role: 10 }, 2)).toBe(false)
  expect(canTakeSupplierOwnership({ ...root, role: 1 }, 2)).toBe(false)
  expect(canTakeSupplierOwnership({ ...root, status: 2 }, 2)).toBe(false)
  expect(canTakeSupplierOwnership(null, 2)).toBe(false)
})

test('supplier confirmation explains audit, sessions and unchanged bindings', async () => {
  const instance = createInstance()
  await instance.init({ lng: 'en', fallbackLng: 'en', resources: {} })
  const t = supplierOwnershipText(instance)
  expect(t('title', { name: 'Partner' })).toBe('Take ownership of Partner?')
  expect(t('confirm')).toContain('sessions and pending OAuth')
  expect(t('confirm')).toContain('audited')
  expect(t('confirm')).toContain('Bindings and configuration stay unchanged')
  await instance.changeLanguage('zhCN')
  expect(supplierOwnershipText(instance)('action')).toBe('接管')
  expect(supplierOwnershipText(instance)('confirm')).toContain('审计')
  expect(supplierOwnershipText(instance)('owner', { id: 7 })).toContain('#7')
})
