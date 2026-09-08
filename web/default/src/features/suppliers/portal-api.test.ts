import { afterEach, describe, expect, mock, test } from 'bun:test'

import { errorKey, sessionExpired } from './lib/errors'
import { portalApi, SupplierRequestError, unwrap } from './portal-api'
import type { AccountQuery, UploadInput } from './types'

const originalFetch = globalThis.fetch
afterEach(() => {
  globalThis.fetch = originalFetch
})

function recordFetch(data: unknown = {}) {
  const calls: Array<{ url: string; init?: RequestInit }> = []
  globalThis.fetch = mock(
    async (url: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(url), init })
      return Response.json({ success: true, data })
    }
  ) as unknown as typeof fetch
  return calls
}

describe('supplier portal wire contract', () => {
  test('login only sends independent cookie-session credentials', async () => {
    const calls = recordFetch({ authenticated: false })
    await portalApi.login({
      username: 'fixture-only',
      password: 'fixture-password',
    })
    expect(calls[0].url).toBe('/supplier-api/v1/auth/login')
    expect(calls[0].init?.credentials).toBe('include')
    expect(calls[0].init?.cache).toBe('no-store')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({
      username: 'fixture-only',
      password: 'fixture-password',
    })
    const headers = new Headers(calls[0].init?.headers)
    expect(headers.has('Authorization')).toBe(false)
    expect(headers.has('New-Api-User')).toBe(false)
    expect(headers.has('X-CSRF-Token')).toBe(false)
  })

  test('unauthenticated probes are normal data and preserve cancellation', async () => {
    const calls = recordFetch({ authenticated: false })
    const controller = new AbortController()
    await expect(portalApi.session(controller.signal)).resolves.toEqual({
      authenticated: false,
    })
    expect(calls[0].init?.signal).toBe(controller.signal)
  })

  test('account filters and force refresh are encoded with explicit binding and pagination', async () => {
    const calls = recordFetch()
    const query: AccountQuery = {
      binding_id: 7,
      page: 2,
      page_size: 100,
      search: 'name+tag@example.test',
      status: 'enabled',
      recovery_window: '5h',
      sort: 'today_cost',
      direction: 'desc',
    }
    await portalApi.accounts(query, undefined, true)
    const url = new URL(calls[0].url, 'https://fixture.test')
    expect(Object.fromEntries(url.searchParams)).toEqual({
      ...Object.fromEntries(
        Object.entries(query).map(([key, value]) => [key, String(value)])
      ),
      refresh: '1',
    })
  })

  test('proxy mutation methods, remote string IDs and CSRF match contract', async () => {
    const calls = recordFetch({ ok: true })
    await portalApi.importProxies('fixture-csrf', 7, 'http://fixture.test:8080')
    await portalApi.testProxy('fixture-csrf', 7, 'proxy_fixture')
    await portalApi.setProxy('fixture-csrf', 7, 'proxy_fixture', 'disabled')
    await portalApi.deleteProxy('fixture-csrf', 7, 'proxy_fixture')
    expect(calls.map((call) => call.init?.method)).toEqual([
      'POST',
      'POST',
      'PATCH',
      'DELETE',
    ])
    expect(calls[1].url).toBe('/supplier-api/v1/proxies/proxy_fixture/test')
    expect(JSON.parse(String(calls[2].init?.body))).toEqual({
      binding_id: 7,
      status: 'disabled',
    })
    expect(calls[3].url).toBe(
      '/supplier-api/v1/proxies/proxy_fixture?binding_id=7'
    )
    for (const call of calls) {
      expect(new Headers(call.init?.headers).get('X-CSRF-Token')).toBe(
        'fixture-csrf'
      )
    }
  })

  test('mutations without a CSRF token do not reach the network', async () => {
    const calls = recordFetch()
    await expect(portalApi.logout('')).rejects.toBeInstanceOf(
      SupplierRequestError
    )
    expect(calls).toHaveLength(0)
  })

  test('OAuth freezes string resource IDs then exchanges only flow and callback', async () => {
    const calls = recordFetch()
    const input: UploadInput = {
      binding_id: 7,
      name: 'fixture',
      outbound_proxy_mode: 'manual',
      outbound_proxy_id: 'proxy_fixture',
      group_ids: ['group_fixture'],
      policy_template_id: '',
      cc_template_id: '',
      max_rpm: 1,
      max_tpm: 2,
      max_concurrent: 3,
      max_sessions: 4,
    }
    await portalApi.authUrl('fixture-csrf', input)
    await portalApi.exchange('fixture-csrf', 'flow_fixture', 'callback_fixture')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual(input)
    expect(calls[1].url).toBe('/supplier-api/v1/account-upload/exchange')
    expect(JSON.parse(String(calls[1].init?.body))).toEqual({
      flow_id: 'flow_fixture',
      callback: 'callback_fixture',
    })
  })

  test('password change sends current and new password without confirmation field', async () => {
    const calls = recordFetch({ authenticated: false })
    await portalApi.password('fixture-csrf', {
      current_password: 'fixture-old',
      password: 'fixture-new',
    })
    expect(calls[0].url).toBe('/supplier-api/v1/auth/password')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({
      current_password: 'fixture-old',
      password: 'fixture-new',
    })
  })

  test('business errors, even HTTP 200, never become success data', () => {
    expect(() =>
      unwrap({ success: false, message: 'supplier_binding_not_found' })
    ).toThrow('supplier_binding_not_found')
    expect(() =>
      unwrap({ success: false, message: 'private upstream payload' })
    ).toThrow('REQUEST_FAILED')
  })

  test('non-JSON upstream failure is sanitized without console-auth fallback', async () => {
    globalThis.fetch = mock(
      async () =>
        new Response('<html>private diagnostic</html>', { status: 502 })
    ) as unknown as typeof fetch
    await expect(portalApi.session()).rejects.toThrow('REQUEST_FAILED')
  })

  test('upstream authentication and wrong current password do not invalidate local sessions', () => {
    const upstream = new SupplierRequestError(
      'supplier_upstream_authentication_failed',
      502
    )
    expect(sessionExpired(upstream)).toBe(false)
    expect(errorKey(upstream)).toBe('supplier.upstreamAuthFailed')
    expect(
      sessionExpired(
        new SupplierRequestError('supplier_current_password_incorrect', 401)
      )
    ).toBe(false)
    expect(
      sessionExpired(new SupplierRequestError('supplier_unauthenticated', 401))
    ).toBe(true)
    expect(
      errorKey(
        new SupplierRequestError('supplier_unrecognized_future_code', 400)
      )
    ).toBe('supplier.requestFailed')
  })
})
