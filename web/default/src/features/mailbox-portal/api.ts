import type {
  Account,
  Attachment,
  Credential,
  ListQuery,
  Page,
  Session,
  Submission,
} from './types'

export class MailboxRequestError extends Error {
  constructor(
    readonly code: string,
    readonly status: number
  ) {
    super(code)
  }
}

const failureListeners = new Set<() => void>()
const accountFailureListeners = new Set<(id: number) => void>()
let generation = 0
const requests = new Set<AbortController>()

export function onAccountFailure(listener: (id: number) => void): () => void {
  accountFailureListeners.add(listener)
  return () => {
    accountFailureListeners.delete(listener)
  }
}

export function invalidateAccountCredentials(id: number, error: unknown): void {
  if (
    error instanceof MailboxRequestError &&
    [403, 404, 409].includes(error.status)
  ) {
    for (const listener of accountFailureListeners) listener(id)
  }
}

export function onAuthFailure(listener: () => void): () => void {
  failureListeners.add(listener)
  return () => {
    failureListeners.delete(listener)
  }
}

export function cancelMailboxRequests(): void {
  generation += 1
  for (const controller of requests) controller.abort()
  requests.clear()
}

export function isAuthFailure(error: unknown): boolean {
  return (
    error instanceof MailboxRequestError &&
    ((error.status === 401 &&
      ![
        'mailbox_invalid_credentials',
        'mailbox_current_password_incorrect',
        'mailbox_current_password_invalid',
      ].includes(error.code)) ||
      [
        'mailbox_session_expired',
        'mailbox_csrf_invalid',
        'mailbox_invalid_csrf',
        'mailbox_csrf_required',
      ].includes(error.code))
  )
}

export function safeError(
  payload: unknown,
  status: number
): MailboxRequestError {
  let code = 'mailbox_request_failed'
  if (
    payload &&
    typeof payload === 'object' &&
    'message' in payload &&
    typeof payload.message === 'string' &&
    /^mailbox_[a-z0-9_]{1,80}$/.test(payload.message)
  ) {
    code = payload.message
  }
  return new MailboxRequestError(code, status)
}

// Deliberately independent of the console axios instance, interceptors and auth stores.
async function request<T>(
  path: string,
  options: {
    body?: unknown
    csrf?: string
    signal?: AbortSignal
    blob?: boolean
    post?: boolean
  } = {}
): Promise<T> {
  if (options.post && path !== '/auth/login' && !options.csrf) {
    throw new MailboxRequestError('mailbox_csrf_required', 403)
  }
  const currentGeneration = generation
  const controller = new AbortController()
  const abort = () => controller.abort()
  options.signal?.addEventListener('abort', abort, { once: true })
  if (options.signal?.aborted) controller.abort()
  requests.add(controller)
  const headers = new Headers({
    'X-Mailbox-Request': '1',
    Accept: options.blob
      ? 'image/png, image/jpeg, image/webp'
      : 'application/json',
  })
  const multipart = options.body instanceof FormData
  if (options.body !== undefined && !multipart) {
    headers.set('Content-Type', 'application/json')
  }
  if (options.csrf) headers.set('X-Mailbox-CSRF', options.csrf)
  try {
    const response = await fetch(`/mailbox-api/v1${path}`, {
      method: options.post ? 'POST' : 'GET',
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
      headers,
      signal: controller.signal,
      body: multipart
        ? (options.body as FormData)
        : JSON.stringify(options.body),
    })
    let result: T
    if (response.ok && options.blob) {
      if (
        !/^image\/(png|jpeg|webp)(;|$)/.test(
          response.headers.get('Content-Type') ?? ''
        )
      ) {
        throw safeError(null, 502)
      }
      result = (await response.blob()) as T
    } else {
      const payload: unknown = await response.json().catch(() => null)
      if (
        !response.ok ||
        !payload ||
        typeof payload !== 'object' ||
        !('success' in payload) ||
        payload.success !== true ||
        !('data' in payload)
      ) {
        throw safeError(payload, response.status)
      }
      result = payload.data as T
    }
    if (generation !== currentGeneration || controller.signal.aborted) {
      throw new DOMException('Aborted', 'AbortError')
    }
    return result
  } catch (error) {
    const accountPath = /^\/accounts\/(\d+)(?:\/credentials)?$/.exec(path)
    if (accountPath) invalidateAccountCredentials(Number(accountPath[1]), error)
    if (path !== '/auth/login' && isAuthFailure(error)) {
      cancelMailboxRequests()
      for (const listener of failureListeners) listener()
    }
    throw error
  } finally {
    requests.delete(controller)
    options.signal?.removeEventListener('abort', abort)
  }
}

function params(query: ListQuery): string {
  const result = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== '') result.set(key, String(value))
  }
  return result.toString()
}

export const mailboxApi = {
  session: (signal?: AbortSignal) =>
    request<Session>('/auth/session', { signal }),
  login: (body: { username: string; password: string }) =>
    request<Session>('/auth/login', { post: true, body }),
  logout: (csrf: string) =>
    request<unknown>('/auth/logout', { post: true, csrf }),
  password: (
    csrf: string,
    body: { current_password: string; password: string }
  ) => request<unknown>('/auth/password', { post: true, csrf, body }),
  accounts: (query: ListQuery, signal?: AbortSignal) =>
    request<Page<Account>>(`/accounts?${params(query)}`, { signal }),
  account: (id: number, signal?: AbortSignal) =>
    request<Account>(`/accounts/${id}`, { signal }),
  credentials: (
    csrf: string,
    id: number,
    kind: 'password' | 'otp',
    signal?: AbortSignal
  ) =>
    request<Credential>(`/accounts/${id}/credentials`, {
      post: true,
      csrf,
      body: { kind },
      signal,
    }),
  submissions: (query: ListQuery, signal?: AbortSignal) =>
    request<Page<Submission>>(`/submissions?${params(query)}`, { signal }),
  upload: (
    csrf: string,
    assignment: number,
    file: File,
    signal?: AbortSignal
  ) => {
    const body = new FormData()
    body.append('file', file)
    return request<Attachment>(`/assignments/${assignment}/attachments`, {
      post: true,
      csrf,
      body,
      signal,
    })
  },
  submit: (csrf: string, assignment: number, version: number, ids: string[]) =>
    request<Submission>(`/assignments/${assignment}/submit`, {
      post: true,
      csrf,
      body: { version, attachment_ids: ids },
    }),
  attachment: (id: string, signal?: AbortSignal) =>
    request<Blob>(`/attachments/${encodeURIComponent(id)}`, {
      blob: true,
      signal,
    }),
}
