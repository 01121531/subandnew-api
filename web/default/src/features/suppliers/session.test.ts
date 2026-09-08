import { afterEach, describe, expect, mock, test } from 'bun:test'

import { clearSupplierSession, sessionOptions, supplierClient } from './session'
import type { AuthSession, Session } from './types'

const originalFetch = globalThis.fetch
afterEach(() => {
  clearSupplierSession()
  supplierClient.clear()
  globalThis.fetch = originalFetch
})
const fixture: AuthSession = {
  authenticated: true,
  csrf_token: 'fixture-csrf',
  supplier: {
    id: 1,
    name: 'Fixture',
    username: 'fixture',
    enabled: true,
    view_accounts: true,
    view_usage: true,
    manage_proxies: false,
    upload_accounts: false,
    created_at: 0,
    updated_at: 0,
  },
}

describe('isolated supplier session cache', () => {
  test('logout removes all supplier data and leaves no authenticated session', () => {
    supplierClient.setQueryData(sessionOptions.queryKey, fixture)
    supplierClient.setQueryData(['supplier', 'accounts', 7], {
      private: 'fixture-data',
    })
    clearSupplierSession()
    expect(
      supplierClient.getQueryData<Session>(sessionOptions.queryKey)
    ).toEqual({
      authenticated: false,
    })
    expect(
      supplierClient.getQueryData(['supplier', 'accounts', 7])
    ).toBeUndefined()
  })
  test('a different cookie-session principal cannot inherit the prior supplier cache', async () => {
    globalThis.fetch = mock(async () =>
      Response.json({ success: true, data: fixture })
    ) as unknown as typeof fetch
    await supplierClient.fetchQuery(sessionOptions)
    supplierClient.setQueryData(['supplier', 'accounts', 7], {
      private: 'fixture-data',
    })
    globalThis.fetch = mock(async () =>
      Response.json({
        success: true,
        data: { ...fixture, supplier: { ...fixture.supplier, id: 2 } },
      })
    ) as unknown as typeof fetch
    await supplierClient.fetchQuery(sessionOptions)
    expect(
      supplierClient.getQueryData(['supplier', 'accounts', 7])
    ).toBeUndefined()
  })
  test('successful unauthenticated probes clear cached data after revocation', async () => {
    supplierClient.setQueryData(['supplier', 'accounts', 7], {
      private: 'fixture-data',
    })
    globalThis.fetch = mock(async () =>
      Response.json({ success: true, data: { authenticated: false } })
    ) as unknown as typeof fetch
    await supplierClient.fetchQuery(sessionOptions)
    expect(
      supplierClient.getQueryData(['supplier', 'accounts', 7])
    ).toBeUndefined()
  })
  test('upstream 502 retains the valid local session', async () => {
    supplierClient.setQueryData(sessionOptions.queryKey, fixture)
    globalThis.fetch = mock(async () =>
      Response.json(
        { success: false, message: 'supplier_upstream_authentication_failed' },
        { status: 502 }
      )
    ) as unknown as typeof fetch
    await expect(supplierClient.fetchQuery(sessionOptions)).rejects.toThrow(
      'supplier_upstream_authentication_failed'
    )
    expect(
      supplierClient.getQueryData<Session>(sessionOptions.queryKey)
    ).toEqual(fixture)
  })
})
