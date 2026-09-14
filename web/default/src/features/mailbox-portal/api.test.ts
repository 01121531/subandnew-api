import { afterEach, describe, expect, mock, test } from 'bun:test'

import {
  cancelMailboxRequests,
  isAuthFailure,
  mailboxApi,
  MailboxRequestError,
  onAuthFailure,
  onAccountFailure,
  safeError,
} from './api'
import { readCredential } from './lib/credentials'
import { errorKey } from './lib/errors'
import type { Account } from './types'

const originalFetch = globalThis.fetch
afterEach(() => {
  globalThis.fetch = originalFetch
  cancelMailboxRequests()
})
function record(data: unknown = {}) {
  const calls: { url: string; init?: RequestInit }[] = []
  globalThis.fetch = mock(
    async (url: RequestInfo | URL, init?: RequestInit) => {
      calls.push({ url: String(url), init })
      return Response.json({ success: true, data })
    }
  ) as unknown as typeof fetch
  return calls
}

describe('mailbox independent wire contract', () => {
  test('login uses only same-origin cookie transport without console identity or CSRF headers', async () => {
    const calls = record({ authenticated: false })
    await mailboxApi.login({
      username: 'fixture',
      password: 'synthetic-password',
    })
    expect(calls[0].url).toBe('/mailbox-api/v1/auth/login')
    expect(calls[0].init?.credentials).toBe('same-origin')
    expect(calls[0].init?.cache).toBe('no-store')
    expect(calls[0].init?.redirect).toBe('error')
    const headers = new Headers(calls[0].init?.headers)
    expect([...headers.keys()].sort()).toEqual([
      'accept',
      'content-type',
      'x-mailbox-request',
    ])
    expect(headers.get('X-Mailbox-Request')).toBe('1')
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({
      username: 'fixture',
      password: 'synthetic-password',
    })
  })
  test('all authenticated writes require the mailbox CSRF token before sending', async () => {
    const calls = record()
    const file = new File(['fixture'], 'fixture.png', { type: 'image/png' })
    const writes = [
      () => mailboxApi.logout(''),
      () =>
        mailboxApi.password('', { current_password: 'old', password: 'new' }),
      () => mailboxApi.credentials('', 3, 'password'),
      () => mailboxApi.upload('', 4, file),
      () => mailboxApi.submit('', 4, 8, ['image']),
    ]
    for (const write of writes) {
      await expect(write()).rejects.toMatchObject({
        code: 'mailbox_csrf_required',
      })
    }
    expect(calls).toHaveLength(0)
  })
  test('credentials, upload and atomic submit use exact IDs, CSRF and assignment version', async () => {
    const calls = record()
    await mailboxApi.credentials('csrf-fixture', 3, 'otp')
    await mailboxApi.upload(
      'csrf-fixture',
      4,
      new File(['fixture'], 'fixture.png', { type: 'image/png' })
    )
    await mailboxApi.submit('csrf-fixture', 4, 8, ['private-image'])
    expect(calls.map((call) => call.url)).toEqual([
      '/mailbox-api/v1/accounts/3/credentials',
      '/mailbox-api/v1/assignments/4/attachments',
      '/mailbox-api/v1/assignments/4/submit',
    ])
    for (const call of calls) {
      const headers = new Headers(call.init?.headers)
      expect(headers.get('X-Mailbox-CSRF')).toBe('csrf-fixture')
      expect(headers.get('X-Mailbox-Request')).toBe('1')
      expect(headers.has('Authorization')).toBe(false)
      expect(headers.has('New-Api-User')).toBe(false)
      expect(headers.has('X-CSRF-Token')).toBe(false)
      expect(call.init?.method).toBe('POST')
    }
    expect(new Headers(calls[1].init?.headers).has('Content-Type')).toBe(false)
    expect(calls[1].init?.body).toBeInstanceOf(FormData)
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ kind: 'otp' })
    expect(JSON.parse(String(calls[2].init?.body))).toEqual({
      version: 8,
      attachment_ids: ['private-image'],
    })
  })
  test('list queries encode filters and numeric pagination without invented aggregate requests', async () => {
    const calls = record()
    await mailboxApi.accounts({
      search: 'name+tag@example.test',
      status: 'rejected',
      page: 2,
      page_size: 20,
    })
    expect(
      Object.fromEntries(
        new URL(calls[0].url, 'https://fixture.test').searchParams
      )
    ).toEqual({
      search: 'name+tag@example.test',
      status: 'rejected',
      page: '2',
      page_size: '20',
    })
    expect(calls).toHaveLength(1)
    expect(new Headers(calls[0].init?.headers).get('X-Mailbox-Request')).toBe(
      '1'
    )
  })
  test('session and account reads also carry the request marker without authenticated-write CSRF', async () => {
    const calls = record({ authenticated: false })
    await mailboxApi.session()
    await mailboxApi.account(3)
    await mailboxApi.submissions({ page: 1, page_size: 20 })
    for (const call of calls) {
      const headers = new Headers(call.init?.headers)
      expect(headers.get('X-Mailbox-Request')).toBe('1')
      expect(headers.has('X-Mailbox-CSRF')).toBe(false)
      expect(headers.has('Authorization')).toBe(false)
      expect(headers.has('New-Api-User')).toBe(false)
    }
  })
  test('private images are fetched as authenticated blobs with no public URL response', async () => {
    globalThis.fetch = mock(
      async (_url: RequestInfo | URL, init?: RequestInit) => {
        expect(new Headers(init?.headers).get('X-Mailbox-Request')).toBe('1')
        return new Response(new Blob(['image'], { type: 'image/png' }), {
          headers: { 'Content-Type': 'image/png' },
        })
      }
    ) as unknown as typeof fetch
    expect(await mailboxApi.attachment('private-id')).toBeInstanceOf(Blob)
    globalThis.fetch = mock(
      async () =>
        new Response('<html>login</html>', {
          headers: { 'Content-Type': 'text/html' },
        })
    ) as unknown as typeof fetch
    await expect(mailboxApi.attachment('private-id')).rejects.toMatchObject({
      code: 'mailbox_request_failed',
    })
  })
  test('expired-session errors notify consumers; wrong passwords do not expire their session', async () => {
    let expired = 0
    const unsubscribe = onAuthFailure(() => {
      expired += 1
    })
    try {
      globalThis.fetch = mock(async () =>
        Response.json(
          { success: false, message: 'mailbox_session_expired' },
          { status: 401 }
        )
      ) as unknown as typeof fetch
      await expect(mailboxApi.account(3)).rejects.toMatchObject({
        code: 'mailbox_session_expired',
      })
      expect(expired).toBe(1)
      expect(
        isAuthFailure(
          new MailboxRequestError('mailbox_current_password_incorrect', 401)
        )
      ).toBe(false)
      expect(
        isAuthFailure(new MailboxRequestError('mailbox_invalid_csrf', 403))
      ).toBe(true)
    } finally {
      unsubscribe()
    }
  })
  test('late responses cannot repopulate memory after cancellation even when transport ignores abort', async () => {
    let resolve!: (response: Response) => void
    globalThis.fetch = mock(
      () =>
        new Promise<Response>((done) => {
          resolve = done
        })
    ) as unknown as typeof fetch
    const request = mailboxApi.credentials('csrf', 3, 'password')
    cancelMailboxRequests()
    resolve(
      Response.json({ success: true, data: { password: 'never-visible' } })
    )
    await expect(request).rejects.toMatchObject({ name: 'AbortError' })
  })
  test('arbitrary server errors never echo credentials, HTML or messages to the UI', () => {
    expect(safeError({ message: 'password=private-password' }, 500).code).toBe(
      'mailbox_request_failed'
    )
    expect(
      safeError({ message: '<script>fixture</script>' }, 500).message
    ).toBe('mailbox_request_failed')
  })
})

const account: Account = {
  id: 3,
  email: 'fixture@example.test',
  version: 1,
  assignment_id: 4,
  assignment_version: 8,
  credentials_available: true,
  status: 'pending',
}
describe('credential read reauthorization', () => {
  test('revoked and missing-account responses clear every credential consumer without logging out', async () => {
    const invalidated: number[] = []
    let authFailures = 0
    const unsubscribeAccount = onAccountFailure((id) => invalidated.push(id))
    const unsubscribeAuth = onAuthFailure(() => {
      authFailures += 1
    })
    try {
      for (const [code, status] of [
        ['mailbox_credentials_revoked', 403],
        ['mailbox_not_found', 404],
      ] as const) {
        globalThis.fetch = mock(async () =>
          Response.json({ success: false, message: code }, { status })
        ) as unknown as typeof fetch
        await expect(
          mailboxApi.credentials('csrf', 3, 'otp')
        ).rejects.toMatchObject({ code })
      }
      expect(invalidated).toEqual([3, 3])
      expect(authFailures).toBe(0)
    } finally {
      unsubscribeAccount()
      unsubscribeAuth()
    }
  })
  test('backend current-password-invalid maps to the field error without clearing authentication', () => {
    const error = new MailboxRequestError(
      'mailbox_current_password_invalid',
      401
    )
    expect(errorKey(error)).toBe('mailboxPortal.currentPasswordIncorrect')
    expect(isAuthFailure(error)).toBe(false)
  })
  test('approval while a credential is in flight rejects the returned secret', async () => {
    const calls: string[] = []
    globalThis.fetch = mock(async (url: RequestInfo | URL) => {
      calls.push(String(url))
      let data: unknown = account
      if (calls.length === 2) {
        data = { password: 'synthetic-secret', server_time: 100 }
      }
      if (calls.length === 3) {
        data = { ...account, status: 'approved', credentials_available: false }
      }
      return Response.json({ success: true, data })
    }) as unknown as typeof fetch
    await expect(
      readCredential('csrf', account, 'password', new AbortController().signal)
    ).rejects.toMatchObject({ code: 'mailbox_credentials_revoked' })
    expect(calls).toHaveLength(3)
  })
  test('reassignment prevents the credential endpoint being called at all', async () => {
    const calls = record({ ...account, assignment_id: 99 })
    await expect(
      readCredential('csrf', account, 'otp', new AbortController().signal)
    ).rejects.toMatchObject({ code: 'mailbox_assignment_changed' })
    expect(calls.map((call) => call.url)).toEqual([
      '/mailbox-api/v1/accounts/3',
    ])
  })
})
