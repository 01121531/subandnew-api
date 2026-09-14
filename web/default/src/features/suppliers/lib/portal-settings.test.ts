import { describe, expect, test } from 'bun:test'

import { SupplierRequestError } from '../portal-api'
import { uploadSettingsChanged } from './errors'
import {
  portalSettingsSchema,
  validPortalText,
  uploadMethodOrder,
} from './portal-settings'

describe('portal settings', () => {
  test('validates Unicode names without interpreting markup', () => {
    expect(validPortalText('中'.repeat(64))).toBe(true)
    expect(validPortalText('中'.repeat(65))).toBe(false)
    expect(validPortalText('<b>Workspace</b>')).toBe(true)
    expect(validPortalText('  ')).toBe(false)
    expect(validPortalText('', true)).toBe(true)
    expect(validPortalText('Line\n2', true)).toBe(false)
  })
  test('requires all switches, permits none enabled and trims title', () => {
    const input = {
      title: '  My workspace  ',
      revision: 2,
      upload_methods: {
        login: false,
        setup_token: false,
        rt: false,
        sk: false,
      },
    }
    expect(portalSettingsSchema.parse(input).title).toBe('My workspace')
    expect(
      portalSettingsSchema.safeParse({
        ...input,
        upload_methods: { login: true },
      }).success
    ).toBe(false)
    expect(uploadMethodOrder).toEqual(['login', 'setup_token', 'rt', 'sk'])
  })
  test('configuration denial clears stale upload state but network errors do not', () => {
    for (const code of [
      'supplier_upload_method_disabled',
      'supplier_portal_settings_changed',
      'supplier_oauth_flow_expired_or_used',
    ]) {
      expect(uploadSettingsChanged(new SupplierRequestError(code, 403))).toBe(
        true
      )
    }
    expect(
      uploadSettingsChanged(new SupplierRequestError('REQUEST_FAILED', 502))
    ).toBe(false)
  })
})
