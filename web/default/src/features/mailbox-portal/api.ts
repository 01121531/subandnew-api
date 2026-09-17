import { issueMetadata } from '@/features/mailbox-management/lib/pools'
import type { Issue, IssueKind } from '@/features/mailbox-management/types'

import { normalSubmissionSchema } from './lib/schemas'
import type {
  Account,
  AccountType,
  Attachment,
  Credential,
  CredentialKind,
  ListQuery,
  Page,
  Session,
  Submission,
} from './types'

export class MailboxRequestError extends Error {
  constructor(
    readonly code: string,
    readonly status: number,
    readonly retryAt = 0
  ) {
    super(code)
  }
}

const failureListeners = new Set<() => void>()
const accountFailureListeners = new Set<(id: number, error: unknown) => void>()
let generation = 0
const requests = new Set<AbortController>()
let rateLimitedUntil = 0

export function retryDeadline(value: string | null, now = Date.now()): number {
  const text = value?.trim() ?? ''
  const seconds = /^\d+$/.test(text) ? Number(text) : Number.NaN
  const date = text && !Number.isFinite(seconds) ? Date.parse(text) : Number.NaN
  const deadline = Number.isFinite(seconds) ? now + seconds * 1000 : date
  return Number.isFinite(deadline) && deadline > now ? deadline : now + 30000
}

export function onAccountFailure(
  listener: (id: number, error: unknown) => void
): () => void {
  accountFailureListeners.add(listener)
  return () => {
    accountFailureListeners.delete(listener)
  }
}

export function invalidateAccountCredentials(id: number, error: unknown): void {
  if (
    error instanceof MailboxRequestError &&
    [403, 404, 409].includes(error.status) &&
    !['mailbox_cvv_unavailable', 'mailbox_cvv_task_restricted'].includes(
      error.code
    )
  ) {
    for (const listener of accountFailureListeners) listener(id, error)
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
  rateLimitedUntil = 0
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
    patch?: boolean
  } = {}
): Promise<T> {
  if (Date.now() < rateLimitedUntil) {
    throw new MailboxRequestError(
      'mailbox_request_failed',
      429,
      rateLimitedUntil
    )
  }
  if (
    (options.post || options.patch) &&
    path !== '/auth/login' &&
    !options.csrf
  ) {
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
  let method = options.post ? 'POST' : 'GET'
  if (options.patch) method = 'PATCH'
  try {
    const response = await fetch(`/mailbox-api/v1${path}`, {
      method,
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
      headers,
      signal: controller.signal,
      body: multipart
        ? (options.body as FormData)
        : JSON.stringify(options.body),
    })
    if (response.status === 429) {
      rateLimitedUntil = Math.max(
        rateLimitedUntil,
        retryDeadline(response.headers.get('Retry-After'))
      )
      throw new MailboxRequestError(
        'mailbox_request_failed',
        429,
        rateLimitedUntil
      )
    }
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
    if (generation !== currentGeneration || controller.signal.aborted) {
      throw new DOMException('Aborted', 'AbortError')
    }
    const accountPath = /^\/accounts\/(\d+)(?:\/credentials)?(?:\?|$)/.exec(
      path
    )
    if (accountPath) {
      const id = Number(accountPath[1])
      if (!path.includes('/credentials')) {
        for (const listener of accountFailureListeners) listener(id, error)
      } else invalidateAccountCredentials(id, error)
    }
    if (
      path !== '/auth/login' &&
      path !== '/auth/session' &&
      isAuthFailure(error)
    ) {
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
  for (const [key, value] of Object.entries({
    ...query,
    account_type: query.account_type ?? 'refund',
  })) {
    if (value !== undefined && value !== '') result.set(key, String(value))
  }
  return result.toString()
}

function scoped(path: string, accountType: AccountType): string {
  return `${path}?account_type=${accountType}`
}

function assertPool(
  value: { account_type?: AccountType },
  accountType: AccountType
): void {
  if ((value.account_type ?? 'refund') !== accountType) {
    throw new MailboxRequestError('mailbox_assignment_changed', 409)
  }
}

export function cardLast4(value: string | undefined): string {
  return typeof value === 'string' && /^\d{4}$/.test(value) ? value : ''
}

// Allowlist ordinary DTOs before they can enter the query/mutation cache.
function publicAccount(value: Account, accountType: AccountType): Account {
  assertPool(value, accountType)
  return {
    id: value.id,
    account_type: value.account_type ?? 'refund',
    card_last4: accountType === 'opening' ? cardLast4(value.card_last4) : '',
    email: value.email,
    version: value.version,
    assignment_id: value.assignment_id,
    assignment_version: value.assignment_version,
    operator_id: value.operator_id,
    operator_name: value.operator_name,
    status: value.status,
    assigned_at: value.assigned_at,
    credentials_available: value.credentials_available,
  }
}

function publicAttachment(value: Attachment): Attachment {
  return {
    id: value.id,
    content_type: value.content_type,
    size: value.size,
    width: value.width,
    height: value.height,
    created_at: value.created_at,
    expires_at: value.expires_at,
    deleted_at: value.deleted_at,
  }
}

function publicSubmission(
  value: Submission,
  accountType: AccountType
): Submission {
  assertPool(value, accountType)
  return {
    id: value.id,
    account_type: value.account_type ?? 'refund',
    card_last4: accountType === 'opening' ? cardLast4(value.card_last4) : '',
    assignment_id: value.assignment_id,
    account_id: value.account_id,
    email: value.email,
    operator_id: value.operator_id,
    operator_name: value.operator_name,
    status: value.status,
    version: value.version,
    review_reason: value.review_reason,
    remark: typeof value.remark === 'string' ? value.remark : '',
    can_edit_remark: value.can_edit_remark === true,
    reviewed_by: value.reviewed_by,
    reviewed_at: value.reviewed_at,
    created_at: value.created_at,
    attachments: (value.attachments ?? []).map(publicAttachment),
  }
}

function publicPage<T>(value: Page<T>, project: (item: T) => T): Page<T> {
  return {
    status_counts: value.status_counts
      ? Object.fromEntries(
          ['pending', 'rejected', 'submitted', 'approved', 'issue_pending'].map(
            (status) => {
              const count = value.status_counts?.[status] ?? 0
              return [
                status,
                Number.isSafeInteger(count) && count >= 0 ? count : 0,
              ]
            }
          )
        )
      : undefined,
    items: value.items.map(project),
    total: value.total,
    page: value.page,
    page_size: value.page_size,
    has_more: value.has_more,
  }
}

export const mailboxApi = {
  editRemark: (
    csrf: string,
    id: number,
    accountType: AccountType,
    version: number,
    remark: string
  ) =>
    request<Submission>(`/submissions/${id}/remark`, {
      patch: true,
      csrf,
      body: { account_type: accountType, version, remark },
    }).then((value) => publicSubmission(value, accountType)),
  remarkHistory: (
    id: number,
    accountType: AccountType,
    page: number,
    signal: AbortSignal
  ) =>
    request<import('@/components/mailbox-remark-editor').RemarkHistory>(
      `/submissions/${id}/remark-history?${params({ account_type: accountType, page, page_size: 20 })}`,
      { signal }
    ),
  issues: (
    query: ListQuery & { assignment_id?: number; kind?: string },
    signal?: AbortSignal
  ) =>
    request<Page<Issue>>(`/issues?${params(query)}`, { signal }).then((value) =>
      publicPage(value, (item) =>
        issueMetadata(item, query.account_type ?? 'refund')
      )
    ),
  issue: (id: number, accountType: AccountType, signal?: AbortSignal) =>
    request<Issue>(scoped(`/issues/${id}`, accountType), { signal }).then(
      (value) => issueMetadata(value, accountType)
    ),
  report: (
    csrf: string,
    assignment: number,
    body: {
      version: number
      kind: IssueKind
      description: string
      attachment_ids: string[]
    },
    accountType: AccountType,
    signal?: AbortSignal
  ) =>
    request<Issue>(scoped(`/assignments/${assignment}/issues`, accountType), {
      post: true,
      csrf,
      body,
      signal,
    }).then((value) => issueMetadata(value, accountType)),
  session: async (signal?: AbortSignal): Promise<Session> => {
    try {
      return await request<Session>('/auth/session', { signal })
    } catch (error) {
      // Let acceptSession clear private data without canceling this route/session query.
      if (isAuthFailure(error)) return { authenticated: false }
      throw error
    }
  },
  login: (body: { username: string; password: string }) =>
    request<Session>('/auth/login', { post: true, body }),
  logout: (csrf: string) =>
    request<unknown>('/auth/logout', { post: true, csrf }),
  password: (
    csrf: string,
    body: { current_password: string; password: string }
  ) => request<unknown>('/auth/password', { post: true, csrf, body }),
  accounts: (query: ListQuery, signal?: AbortSignal) =>
    request<Page<Account>>(`/accounts?${params(query)}`, { signal }).then(
      (value) =>
        publicPage(value, (item) =>
          publicAccount(item, query.account_type ?? 'refund')
        )
    ),
  account: (
    id: number,
    signal?: AbortSignal,
    accountType: AccountType = 'refund'
  ) =>
    request<Account>(scoped(`/accounts/${id}`, accountType), { signal }).then(
      (value) => publicAccount(value, accountType)
    ),
  credentials: (
    csrf: string,
    id: number,
    kind: CredentialKind,
    signal?: AbortSignal,
    accountType: AccountType = 'refund'
  ) =>
    request<Credential>(scoped(`/accounts/${id}/credentials`, accountType), {
      post: true,
      csrf,
      body: { kind },
      signal,
    }).then((value): Credential => {
      if (kind === 'card') {
        return {
          card_number: value.card_number,
          card_expiry: value.card_expiry,
          server_time: value.server_time,
        }
      }
      if (kind === 'password') {
        return { password: value.password, server_time: value.server_time }
      }
      if (kind === 'cvv') {
        return {
          cvv: value.cvv,
          ...(value.persistent === true ? { persistent: true } : {}),
          expires_at: value.expires_at,
          server_time: value.server_time,
        }
      }
      return {
        available: value.available,
        code: value.code,
        expires_at: value.expires_at,
        server_time: value.server_time,
      }
    }),
  submissions: (query: ListQuery, signal?: AbortSignal) =>
    request<Page<Submission>>(`/submissions?${params(query)}`, { signal }).then(
      (value) =>
        publicPage(value, (item) =>
          publicSubmission(item, query.account_type ?? 'refund')
        )
    ),
  upload: (
    csrf: string,
    assignment: number,
    file: File,
    signal?: AbortSignal,
    accountType: AccountType = 'refund'
  ) => {
    const body = new FormData()
    body.append('file', file)
    return request<Attachment>(
      scoped(`/assignments/${assignment}/attachments`, accountType),
      {
        post: true,
        csrf,
        body,
        signal,
      }
    ).then(publicAttachment)
  },
  submit: async (
    csrf: string,
    assignment: number,
    version: number,
    ids: string[],
    signal?: AbortSignal,
    accountType: AccountType = 'refund',
    remark = ''
  ) => {
    const parsed = normalSubmissionSchema.safeParse({
      attachment_ids: ids,
      remark,
    })
    if (!parsed.success) {
      throw new MailboxRequestError(parsed.error.issues[0].message, 400)
    }
    return request<Submission>(
      scoped(`/assignments/${assignment}/submit`, accountType),
      {
        post: true,
        csrf,
        body: { version, ...parsed.data },
        signal,
      }
    ).then((value) => publicSubmission(value, accountType))
  },
  attachment: (
    id: string,
    signal?: AbortSignal,
    accountType: AccountType = 'refund'
  ) =>
    request<Blob>(
      scoped(`/attachments/${encodeURIComponent(id)}`, accountType),
      {
        blob: true,
        signal,
      }
    ),
}
