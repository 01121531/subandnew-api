import { describe, expect, test } from 'bun:test'

import { useSupplierUploadPreferences } from '@/stores/supplier-upload-preferences'

import type { Supplier, UploadOptions } from '../types'
import { portalViews } from './portal-views'
import { uploadSchema } from './schemas'
import { uploadDefaults } from './upload-defaults'

const options: UploadOptions = {
  observed_at: 1,
  stale: false,
  groups: [],
  proxies: [{ id: 'proxy1', name: 'Proxy', host: '192.0.2.1', port: 443 }],
  policies: [
    {
      id: 'p1',
      name: 'First',
      policy: {
        max_rpm: 100,
        max_tpm: 200,
        max_concurrent: 3,
        max_sessions: 0,
      },
    },
    {
      id: 'p2',
      name: 'Second',
      policy: {
        max_rpm: 200,
        max_tpm: 400,
        max_concurrent: 6,
        max_sessions: 8,
      },
    },
  ],
  templates: [
    { id: 'cc1', name: 'First' },
    { id: 'cc2', name: 'Second' },
  ],
}

describe('supplier upload defaults', () => {
  test('selects first bindable proxy and templates, preserving real zero', () => {
    const defaults = uploadDefaults(11, options)
    expect(defaults).toMatchObject({
      binding_id: 11,
      outbound_proxy_mode: 'manual',
      outbound_proxy_id: 'proxy1',
      policy_template_id: 'p1',
      cc_template_id: 'cc1',
      max_rpm: 100,
      max_sessions: 0,
    })
    expect(uploadSchema.safeParse({ ...defaults, name: 'Demo' }).success).toBe(
      true
    )
  })

  test('uses only currently valid remembered IDs and current policy limits', () => {
    const selected = uploadDefaults(11, options, {
      policyId: 'p2',
      templateId: 'cc2',
    })
    expect(selected).toMatchObject({
      policy_template_id: 'p2',
      cc_template_id: 'cc2',
      max_rpm: 200,
    })
    const deleted = uploadDefaults(11, options, {
      policyId: 'deleted',
      templateId: 'deleted',
    })
    expect(deleted).toMatchObject({
      policy_template_id: 'p1',
      cc_template_id: 'cc1',
    })
  })

  test('does not fall back to direct connection or unlimited limits', () => {
    const empty = uploadDefaults(11, {
      ...options,
      proxies: [],
      policies: [],
      templates: [],
    })
    expect(empty).toMatchObject({
      outbound_proxy_mode: 'manual',
      outbound_proxy_id: '',
      policy_template_id: '',
      cc_template_id: '',
      max_rpm: '',
      max_tpm: '',
      max_concurrent: '',
      max_sessions: '',
    })
    expect(uploadSchema.safeParse({ ...empty, name: 'Demo' }).success).toBe(
      false
    )
    const noProxy = uploadDefaults(11, { ...options, proxies: [] })
    expect(uploadSchema.safeParse({ ...noProxy, name: 'Demo' }).success).toBe(
      false
    )
  })

  test('stores only IDs and time format with supplier and binding isolation', () => {
    const store = useSupplierUploadPreferences.getState()
    store.remember(1, 11, {
      policyId: 'p2',
      templateId: 'cc2',
      password: 'never-store',
      max_rpm: 99,
      nameTimeMode: 'date_time',
      timestamp: 'never-store',
    } as { policyId: string; templateId: string })
    store.remember(2, 11, { policyId: 'p1', templateId: 'cc1' })
    const { choices } = useSupplierUploadPreferences.getState()
    expect(choices['1:11']).toEqual({
      policyId: 'p2',
      templateId: 'cc2',
      nameTimeMode: 'date_time',
    })
    expect(choices['2:11']).toEqual({
      policyId: 'p1',
      templateId: 'cc1',
      nameTimeMode: 'none',
    })
    expect(choices['1:12']).toBeUndefined()
    expect(uploadDefaults(11, options, choices['1:11']).name_time_mode).toBe(
      'date_time'
    )
    expect(uploadDefaults(11, options).name_time_mode).toBe('none')
    expect(
      uploadDefaults(11, options, { policyId: 'p1', templateId: 'cc1' })
        .name_time_mode
    ).toBe('none')
  })

  test('upload-only suppliers retain the account entry without other modules', () => {
    const supplier = {
      view_accounts: false,
      upload_accounts: true,
      view_usage: false,
      manage_proxies: false,
    } as Supplier
    expect(portalViews(supplier).map((view) => view.id)).toEqual([
      'accounts',
      'security',
    ])
    expect(
      portalViews({ ...supplier, upload_accounts: false }).map(
        (view) => view.id
      )
    ).toEqual(['security'])
  })
})
