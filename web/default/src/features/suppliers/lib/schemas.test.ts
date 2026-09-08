import { describe, expect, test } from 'bun:test'

import { auditLabelKey, formatNumber, proxyEnabled } from './display'
import {
  passwordSchema,
  safeOAuthUrl,
  supplierDefaults,
  timestampMs,
  uploadSchema,
} from './schemas'

const upload = {
  binding_id: 1,
  name: 'fixture',
  outbound_proxy_mode: 'direct',
  outbound_proxy_id: '',
  group_ids: [],
  policy_template_id: '',
  cc_template_id: '',
  max_rpm: 0,
  max_tpm: 0,
  max_concurrent: 0,
  max_sessions: 0,
}

describe('supplier permissions and input boundaries', () => {
  test('new suppliers are read only until explicitly granted write permissions', () => {
    expect(supplierDefaults.view_accounts).toBe(true)
    expect(supplierDefaults.view_usage).toBe(true)
    expect(supplierDefaults.manage_proxies).toBe(false)
    expect(supplierDefaults.upload_accounts).toBe(false)
  })
  test('password validation rejects short and mismatched values', () => {
    expect(
      passwordSchema.safeParse({
        current_password: 'fixture',
        password: 'short',
        confirm: 'short',
      }).success
    ).toBe(false)
    expect(
      passwordSchema.safeParse({
        current_password: 'fixture',
        password: 'fixture-new',
        confirm: 'different',
      }).success
    ).toBe(false)
    expect(
      passwordSchema.safeParse({
        current_password: 'fixture',
        password: 'fixture-new',
        confirm: 'fixture-new',
      }).success
    ).toBe(true)
  })
  test('OAuth requires a manual proxy and respects gateway limits', () => {
    expect(uploadSchema.safeParse(upload).success).toBe(true)
    expect(
      uploadSchema.safeParse({ ...upload, name: 'x'.repeat(65) }).success
    ).toBe(false)
    expect(
      uploadSchema.safeParse({ ...upload, outbound_proxy_mode: 'manual' })
        .success
    ).toBe(false)
    expect(
      uploadSchema.safeParse({
        ...upload,
        outbound_proxy_mode: 'manual',
        outbound_proxy_id: 'proxy_fixture',
        group_ids: ['group_fixture'],
      }).success
    ).toBe(true)
    expect(
      uploadSchema.safeParse({ ...upload, outbound_proxy_mode: 'auto' }).success
    ).toBe(true)
    expect(uploadSchema.safeParse({ ...upload, max_rpm: -1 }).success).toBe(
      false
    )
    expect(
      uploadSchema.safeParse({ ...upload, max_concurrent: 100001 }).success
    ).toBe(false)
  })
  test('authorization navigation only permits credential-free HTTPS URLs', () => {
    expect(
      safeOAuthUrl('https://oauth.example.test/authorize?state=fixture')
    ).toContain('https://oauth.example.test/')
    for (const url of [
      'javascript:alert(1)',
      '//evil.test',
      'http://oauth.example.test',
      'https://user:secret@oauth.example.test',
    ]) {
      expect(safeOAuthUrl(url)).toBeNull()
    }
  })
  test('active and enabled proxy responses both generate a disable action', () => {
    expect(proxyEnabled('active')).toBe(true)
    expect(proxyEnabled('enabled')).toBe(true)
    expect(proxyEnabled('disabled')).toBe(false)
  })
  test('missing metrics stay missing rather than masquerading as zero', () => {
    expect(formatNumber(null)).toBe('--')
    expect(formatNumber(0, 4)).toBe('0.0000')
    expect(timestampMs(1700000000)).toBe(1700000000000)
    expect(timestampMs('2026-09-08T00:00:00+08:00')).toBe(
      Date.parse('2026-09-07T16:00:00Z')
    )
  })
  test('method and route audit actions have human localization keys', () => {
    expect(
      auditLabelKey('POST /api/suppliers/:id/bindings/:binding_id/test')
    ).toBe('supplier.test')
    expect(auditLabelKey('PATCH /supplier-api/v1/proxies/:id')).toBe(
      'supplier.proxyStatus'
    )
    expect(auditLabelKey('POST /supplier-api/v1/auth/password')).toBe(
      'supplier.changePassword'
    )
    expect(auditLabelKey('unknown private event')).toBe('supplier.auditAction')
  })
})
