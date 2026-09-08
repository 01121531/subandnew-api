import type {
  AccountPage,
  AccountQuery,
  AccountSummary,
  Binding,
  Envelope,
  ImportResult,
  OAuthFlow,
  ProxyPage,
  Session,
  TestResult,
  UploadInput,
  UploadOptions,
  Usage,
} from './types'

export class SupplierRequestError extends Error {
  constructor(
    readonly code: string,
    readonly status: number
  ) {
    super(code)
  }
}

export function unwrap<T>(payload: Envelope<T>, status = 200): T {
  if (!payload || payload.success !== true) {
    const code =
      payload &&
      'message' in payload &&
      /^(?:[A-Z][A-Z0-9_]{0,79}|supplier_[a-z0-9_]{1,70})$/.test(
        payload.message
      )
        ? payload.message
        : 'REQUEST_FAILED'
    throw new SupplierRequestError(code, status)
  }
  return payload.data
}

async function request<T>(
  path: string,
  options: {
    method?: string
    body?: unknown
    csrf?: string
    signal?: AbortSignal
  } = {}
): Promise<T> {
  if (
    options.method &&
    options.method !== 'GET' &&
    path !== '/auth/login' &&
    !options.csrf
  ) {
    throw new SupplierRequestError('CSRF_REQUIRED', 403)
  }
  const headers = new Headers({ Accept: 'application/json' })
  if (options.body !== undefined) {
    headers.set('Content-Type', 'application/json')
  }
  if (options.csrf) headers.set('X-CSRF-Token', options.csrf)
  const response = await fetch(`/supplier-api/v1${path}`, {
    method: options.method ?? 'GET',
    credentials: 'include',
    cache: 'no-store',
    headers,
    signal: options.signal,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  })
  const payload = (await response.json().catch(() => {
    throw new SupplierRequestError('REQUEST_FAILED', response.status)
  })) as Envelope<T>
  if (!response.ok && payload?.success === true) {
    throw new SupplierRequestError('REQUEST_FAILED', response.status)
  }
  return unwrap(payload, response.status)
}

function params(values: object, refresh = false): string {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(values)) {
    if (value !== '' && value !== null && value !== undefined) {
      query.set(key, String(value))
    }
  }
  if (refresh) query.set('refresh', '1')
  return query.toString()
}

export const portalApi = {
  session: (signal?: AbortSignal) =>
    request<Session>('/auth/session', { signal }),
  login: (body: { username: string; password: string }) =>
    request<Session>('/auth/login', { method: 'POST', body }),
  logout: (csrf: string) =>
    request<unknown>('/auth/logout', { method: 'POST', csrf }),
  password: (
    csrf: string,
    body: { current_password: string; password: string }
  ) => request<unknown>('/auth/password', { method: 'POST', csrf, body }),
  bindings: (signal?: AbortSignal, refresh = false) =>
    request<Binding[]>(`/bindings?${params({}, refresh)}`, { signal }),
  accounts: (query: AccountQuery, signal?: AbortSignal, refresh = false) =>
    request<AccountPage>(`/accounts?${params(query, refresh)}`, { signal }),
  summary: (binding_id: number, signal?: AbortSignal, refresh = false) =>
    request<AccountSummary>(
      `/account-summary?${params({ binding_id }, refresh)}`,
      { signal }
    ),
  usage: (
    binding_id: number,
    days: number,
    signal?: AbortSignal,
    refresh = false
  ) =>
    request<Usage>(`/usage?${params({ binding_id, days }, refresh)}`, {
      signal,
    }),
  proxies: (
    binding_id: number,
    signal?: AbortSignal,
    refresh = false,
    page = 1
  ) =>
    request<ProxyPage>(
      `/proxies?${params({ binding_id, page, page_size: 20 }, refresh)}`,
      {
        signal,
      }
    ),
  importProxies: (csrf: string, binding_id: number, text: string) =>
    request<ImportResult>('/proxies', {
      method: 'POST',
      csrf,
      body: { binding_id, text },
    }),
  testProxy: (csrf: string, binding_id: number, id: string) =>
    request<TestResult>(`/proxies/${encodeURIComponent(id)}/test`, {
      method: 'POST',
      csrf,
      body: { binding_id },
    }),
  setProxy: (
    csrf: string,
    binding_id: number,
    id: string,
    status: 'enabled' | 'disabled'
  ) =>
    request<unknown>(`/proxies/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      csrf,
      body: { binding_id, status },
    }),
  deleteProxy: (csrf: string, binding_id: number, id: string) =>
    request<unknown>(
      `/proxies/${encodeURIComponent(id)}?${params({ binding_id })}`,
      { method: 'DELETE', csrf }
    ),
  options: (binding_id: number, signal?: AbortSignal, refresh = false) =>
    request<UploadOptions>(
      `/account-upload/options?${params({ binding_id }, refresh)}`,
      { signal }
    ),
  authUrl: (csrf: string, body: UploadInput) =>
    request<OAuthFlow>('/account-upload/auth-url', {
      method: 'POST',
      csrf,
      body,
    }),
  exchange: (csrf: string, flow_id: string, callback: string) =>
    request<{ completed: true }>('/account-upload/exchange', {
      method: 'POST',
      csrf,
      body: { flow_id, callback },
    }),
}
